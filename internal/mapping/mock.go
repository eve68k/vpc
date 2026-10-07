package mapping

import (
	"net"
	"sync"
)

// mockService はプールから順に割り当てるだけのインメモリ実装。
type mockService struct {
	mu    sync.Mutex
	pool  []net.IP
	next  int
	byMAC map[string]net.IP
}

// NewMock は pool からMACごとに順番にIPを割り当てる Service を返す。
func NewMock(pool []net.IP) Service {
	return &mockService{pool: pool, byMAC: make(map[string]net.IP)}
}

func (m *mockService) Lookup(mac net.HardwareAddr) (net.IP, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if ip, ok := m.byMAC[mac.String()]; ok {
		return ip, true
	}
	if m.next >= len(m.pool) {
		return nil, false
	}
	ip := m.pool[m.next]
	m.next++
	m.byMAC[mac.String()] = ip
	return ip, true
}
