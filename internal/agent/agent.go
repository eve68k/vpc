// Package agent は 1 つのプロセスで複数の VNI の Port を同時に扱う。
package agent

import (
	"encoding/binary"
	"errors"
	"fmt"
	"log"
	"net"
	"sync"

	"github.com/eve68k/vpc/internal/mapping"
	"github.com/eve68k/vpc/vni"
)

var (
	ErrAlreadyAttached = errors.New("port already attached")
	ErrNotAttached     = errors.New("port not attached")
	ErrUnknownVNI      = errors.New("unknown vni")
)

// FrameHandler は Port から読んだフレームを処理する。handled=false なら誰も扱わなかったフレーム。
type FrameHandler interface {
	HandleFrame(frame []byte, p vni.Port) (handled bool, err error)
}

// Manager は Attach された Port ごとに読み取りループを持つ。
// どの Port を持つべきかは Source が決め、Manager は Sink としてそれに従うだけにする。
type Manager struct {
	Mapping mapping.Service
	Handler FrameHandler
	// Open は port 名から Port を開く。
	Open func(name string) (vni.Port, error)

	mu    sync.Mutex
	ports map[string]*attached
}

type attached struct {
	port vni.Port
	done chan struct{} // 読み取りループが終わると閉じる。
}

// Attach は name の Port を開いて読み取りを始める。
// name が Mapping に VNI として登録されていなければ、Port を開かずに失敗する。
func (m *Manager) Attach(name string) error {
	if _, ok := m.Mapping.VNIForPort(name); !ok {
		return fmt.Errorf("%w: %s", ErrUnknownVNI, name)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.ports[name]; ok {
		return fmt.Errorf("%w: %s", ErrAlreadyAttached, name)
	}
	p, err := m.Open(name)
	if err != nil {
		return fmt.Errorf("open %s: %w", name, err)
	}
	a := &attached{port: p, done: make(chan struct{})}
	if m.ports == nil {
		m.ports = make(map[string]*attached)
	}
	m.ports[name] = a
	go m.serve(a)
	return nil
}

// Detach は name の Port を閉じ、読み取りループの終了を待つ。
func (m *Manager) Detach(name string) error {
	m.mu.Lock()
	a, ok := m.ports[name]
	delete(m.ports, name)
	m.mu.Unlock()
	if !ok {
		return fmt.Errorf("%w: %s", ErrNotAttached, name)
	}
	a.port.Close()
	<-a.done
	return nil
}

// Close はすべての Port を Detach する。
func (m *Manager) Close() {
	m.mu.Lock()
	names := make([]string, 0, len(m.ports))
	for name := range m.ports {
		names = append(names, name)
	}
	m.mu.Unlock()
	for _, name := range names {
		m.Detach(name)
	}
}

// Attached は Attach 済みの port 名を返す。
func (m *Manager) Attached() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	names := make([]string, 0, len(m.ports))
	for name := range m.ports {
		names = append(names, name)
	}
	return names
}

func (m *Manager) serve(a *attached) {
	defer close(a.done)
	p := a.port
	buf := make([]byte, 65536)
	for {
		n, err := p.ReadFrame(buf)
		if err != nil {
			if !errors.Is(err, vni.ErrClosed) {
				// 1 つの Port の失敗で他の VNI を巻き込まない。
				log.Printf("%s: read: %v", p.Name(), err)
				m.forget(p.Name(), a)
				p.Close()
			}
			return
		}

		handled, err := m.Handler.HandleFrame(buf[:n], p)
		if err != nil {
			log.Printf("%s: dhcp: %v", p.Name(), err)
			continue
		}
		if handled || n < 14 {
			continue
		}
		dst := net.HardwareAddr(buf[0:6])
		src := net.HardwareAddr(buf[6:12])
		et := binary.BigEndian.Uint16(buf[12:14])
		log.Printf("%s: frame len=%d dst=%s src=%s ethertype=0x%04x", p.Name(), n, dst, src, et)
	}
}

// forget は読み取りに失敗した Port を、自分が登録したものである場合に限り取り除く。
// 同名で Attach し直された別の Port を消さないため a を比べる。
func (m *Manager) forget(name string, a *attached) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ports[name] == a {
		delete(m.ports, name)
	}
}
