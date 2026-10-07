package mapping

import (
	"net"
	"sync"
)

// VPCConfig はモックが VPC ごとに保持する静的な設定。
type VPCConfig struct {
	Subnet    *net.IPNet
	Pool      []net.IP
	LeaseTime uint32
}

// mockService はプールから順に割り当てるだけのインメモリ実装。
type mockService struct {
	portVPC map[string]VPCID

	mu   sync.Mutex
	vpcs map[VPCID]*mockVPC
}

type mockVPC struct {
	cfg   VPCConfig
	next  int
	byMAC map[string]net.IP
}

// NewMock は portVPC（Port名 → VPCID）と vpcs（VPCID → 設定）から、
// VPCごとのプールでMACごとに順番にIPを割り当てる Service を返す。
func NewMock(portVPC map[string]VPCID, vpcs map[VPCID]VPCConfig) Service {
	m := &mockService{portVPC: portVPC, vpcs: make(map[VPCID]*mockVPC, len(vpcs))}
	for id, cfg := range vpcs {
		m.vpcs[id] = &mockVPC{cfg: cfg, byMAC: make(map[string]net.IP)}
	}
	return m
}

func (m *mockService) VPCForPort(portName string) (VPCID, bool) {
	id, ok := m.portVPC[portName]
	return id, ok
}

func (m *mockService) Lookup(vpcID VPCID, mac net.HardwareAddr) (Lease, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	v, ok := m.vpcs[vpcID]
	if !ok {
		return Lease{}, false
	}

	ip, ok := v.byMAC[mac.String()]
	if !ok {
		if v.next >= len(v.cfg.Pool) {
			return Lease{}, false
		}
		ip = v.cfg.Pool[v.next]
		v.next++
		v.byMAC[mac.String()] = ip
	}
	return Lease{IP: ip, Subnet: v.cfg.Subnet, LeaseTime: v.cfg.LeaseTime}, true
}
