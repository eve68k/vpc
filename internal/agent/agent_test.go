package agent

import (
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/eve68k/vpc/internal/mapping"
	"github.com/eve68k/vpc/vni"
)

// echoHandler は受け取ったフレームを、読んだ Port へそのまま書き返す。
type echoHandler struct{}

func (echoHandler) HandleFrame(b []byte, p vni.Port) (bool, error) {
	return true, p.WriteFrame(b)
}

// newTestManager は names を VNI として登録済みで、Open が MemPair の片側を返す Manager と、
// VM 側に当たる反対側の Port（name → Port）を返す。
func newTestManager(names ...string) (*Manager, map[string]vni.Port) {
	var vnis []mapping.VNI
	for _, n := range names {
		vnis = append(vnis, mapping.VNI{Name: n, VPCID: 1, MAC: net.HardwareAddr{2, 0, 0, 0, 0, 1}})
	}
	peers := make(map[string]vni.Port)
	var mu sync.Mutex
	m := &Manager{
		Mapping: mapping.NewMock(vnis, nil),
		Handler: echoHandler{},
		Open: func(name string) (vni.Port, error) {
			agentSide, vmSide := vni.NewMemPair(name, name+"-vm")
			mu.Lock()
			peers[name] = vmSide
			mu.Unlock()
			return agentSide, nil
		},
	}
	return m, peers
}

func roundTrip(t *testing.T, vm vni.Port, frame []byte) {
	t.Helper()
	if err := vm.WriteFrame(frame); err != nil {
		t.Fatalf("write: %v", err)
	}
	got := make(chan []byte, 1)
	go func() {
		buf := make([]byte, 64)
		n, err := vm.ReadFrame(buf)
		if err == nil {
			got <- buf[:n]
		}
	}()
	select {
	case b := <-got:
		if string(b) != string(frame) {
			t.Fatalf("got %q, want %q", b, frame)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for reply")
	}
}

func TestManager_複数のPortを同時に扱える(t *testing.T) {
	m, peers := newTestManager("tap-a", "tap-b")
	defer m.Close()

	for _, n := range []string{"tap-a", "tap-b"} {
		if err := m.Attach(n); err != nil {
			t.Fatalf("Attach(%s): %v", n, err)
		}
	}
	roundTrip(t, peers["tap-a"], []byte("a"))
	roundTrip(t, peers["tap-b"], []byte("b"))
}

func TestManager_Detachした後は読み取りが止まり同名で再Attachできる(t *testing.T) {
	m, peers := newTestManager("tap-a")
	defer m.Close()

	if err := m.Attach("tap-a"); err != nil {
		t.Fatal(err)
	}
	old := peers["tap-a"]
	if err := m.Detach("tap-a"); err != nil {
		t.Fatal(err)
	}
	if err := old.WriteFrame([]byte("x")); !errors.Is(err, vni.ErrClosed) {
		t.Fatalf("WriteFrame after Detach = %v, want ErrClosed", err)
	}

	if err := m.Attach("tap-a"); err != nil {
		t.Fatalf("re-Attach: %v", err)
	}
	roundTrip(t, peers["tap-a"], []byte("again"))
}

func TestManager_Detachは他のPortに影響しない(t *testing.T) {
	m, peers := newTestManager("tap-a", "tap-b")
	defer m.Close()
	m.Attach("tap-a")
	m.Attach("tap-b")

	if err := m.Detach("tap-a"); err != nil {
		t.Fatal(err)
	}
	roundTrip(t, peers["tap-b"], []byte("b"))
}

func TestManager_Attachの失敗(t *testing.T) {
	m, _ := newTestManager("tap-a")
	defer m.Close()

	if err := m.Attach("tap-x"); !errors.Is(err, ErrUnknownVNI) {
		t.Errorf("unknown VNI: got %v, want ErrUnknownVNI", err)
	}
	if err := m.Attach("tap-a"); err != nil {
		t.Fatal(err)
	}
	if err := m.Attach("tap-a"); !errors.Is(err, ErrAlreadyAttached) {
		t.Errorf("duplicate: got %v, want ErrAlreadyAttached", err)
	}
	if err := m.Detach("tap-x"); !errors.Is(err, ErrNotAttached) {
		t.Errorf("Detach unknown: got %v, want ErrNotAttached", err)
	}
}

func TestManager_Closeで全Portが閉じる(t *testing.T) {
	m, peers := newTestManager("tap-a", "tap-b")
	m.Attach("tap-a")
	m.Attach("tap-b")

	m.Close()

	if got := m.Attached(); len(got) != 0 {
		t.Errorf("Attached() = %v, want empty", got)
	}
	for n, vm := range peers {
		if err := vm.WriteFrame([]byte("x")); !errors.Is(err, vni.ErrClosed) {
			t.Errorf("%s: WriteFrame after Close = %v, want ErrClosed", n, err)
		}
	}
}
