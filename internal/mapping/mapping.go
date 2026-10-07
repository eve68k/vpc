// Package mapping は MAC/IP と所在の対応を問い合わせるマッピングサービスを抽象化する。
// DHCP、ARP代理応答、分散ルーターなど複数の機能から共通で参照される境界。
package mapping

import "net"

// VPCID は VPC の識別子。独自のカプセル化ヘッダにそのまま埋め込むため、
// 可変長の string ではなく固定長の整数にする（design.md の VNI 相当）。
type VPCID uint32

// Lease は VPC 内で mac に割り当てる（または割り当て済みの）リース情報。
type Lease struct {
	IP        net.IP
	Subnet    *net.IPNet // ネットワークアドレス + マスク。ゲートウェイは Subnet から導出する。
	LeaseTime uint32
}

// Service は VPC ごとの割り当て情報を問い合わせる。
// キャッシュするかどうかは実装側の詳細であり、呼び出し側は関知しない。
type Service interface {
	// VPCForPort は port 名からその port が所属する VPC の識別子を返す。
	VPCForPort(portName string) (vpcID VPCID, ok bool)
	// Lookup は vpcID 内で mac に割り当てるリース情報を返す。
	// ok=false はその MAC に割り当てられる IP がないことを示す。
	Lookup(vpcID VPCID, mac net.HardwareAddr) (lease Lease, ok bool)
}
