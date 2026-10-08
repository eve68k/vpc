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

// Mock はプールから順に割り当てるだけのインメモリ実装。
// 本物のマッピングサービスに差し替えるまでの暫定で、VNI や VPC を実行中に足し引きできる。
type Mock struct {
	mu   sync.Mutex
	vnis map[string]VNI
	vpcs map[VPCID]*mockVPC
}

type mockVPC struct {
	cfg   VPCConfig
	next  int
	byMAC map[string]net.IP
}

// NewMock は登録済みの vnis と vpcs（VPCID → 設定）から、
// VPCごとのプールでMACごとに順番にIPを割り当てる Service を返す。
func NewMock(vnis []VNI, vpcs map[VPCID]VPCConfig) *Mock {
	m := &Mock{vnis: make(map[string]VNI, len(vnis)), vpcs: make(map[VPCID]*mockVPC, len(vpcs))}
	for _, v := range vnis {
		m.vnis[v.Name] = v
	}
	for id, cfg := range vpcs {
		m.vpcs[id] = &mockVPC{cfg: cfg, byMAC: make(map[string]net.IP)}
	}
	return m
}

// PutVNI は VNI を登録する。同じ Name があれば置き換える。
func (m *Mock) PutVNI(v VNI) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.vnis[v.Name] = v
}

// DeleteVNI は portName の VNI を取り除く。割り当て済みのリースは残す。
func (m *Mock) DeleteVNI(portName string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.vnis, portName)
}

// EnsureVPC は vpcID が未登録のときだけ cfg で登録する。
// 登録済みなら何もしない（割り当て済みのリースを失わないため）。
func (m *Mock) EnsureVPC(vpcID VPCID, cfg VPCConfig) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.vpcs[vpcID]; !ok {
		m.vpcs[vpcID] = &mockVPC{cfg: cfg, byMAC: make(map[string]net.IP)}
	}
}

func (m *Mock) VNIForPort(portName string) (VNI, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.vnis[portName]
	return v, ok
}

func (m *Mock) Lookup(vpcID VPCID, mac net.HardwareAddr) (Lease, bool) {
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
