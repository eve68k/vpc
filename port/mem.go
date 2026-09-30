package port

import "sync"

type memLink struct {
	done chan struct{}
	once sync.Once
}

type memPort struct {
	name string
	rx   <-chan []byte
	tx   chan<- []byte
	link *memLink
}

// NewMemPair は相互に接続されたメモリ上の Port ペアを返す。
// OS 非依存のユニットテスト用（macOS でも動く）。どちらかを Close すると両方閉じる。
func NewMemPair(nameA, nameB string) (Port, Port) {
	ab := make(chan []byte, 64)
	ba := make(chan []byte, 64)
	l := &memLink{done: make(chan struct{})}
	return &memPort{name: nameA, rx: ba, tx: ab, link: l},
		&memPort{name: nameB, rx: ab, tx: ba, link: l}
}

func (p *memPort) Name() string { return p.name }

func (p *memPort) ReadFrame(buf []byte) (int, error) {
	select {
	case f := <-p.rx:
		return copy(buf, f), nil
	case <-p.link.done:
		return 0, ErrClosed
	}
}

func (p *memPort) WriteFrame(frame []byte) error {
	cp := make([]byte, len(frame))
	copy(cp, frame)
	select {
	case p.tx <- cp:
		return nil
	case <-p.link.done:
		return ErrClosed
	}
}

func (p *memPort) Close() error {
	p.link.once.Do(func() { close(p.link.done) })
	return nil
}
