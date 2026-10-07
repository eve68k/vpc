// Package mapping は MAC/IP と所在の対応を問い合わせるマッピングサービスを抽象化する。
// DHCP、ARP代理応答、分散ルーターなど複数の機能から共通で参照される境界。
package mapping

import "net"

// Service は mac に割り当てる IP を問い合わせる。
type Service interface {
	// Lookup は mac に割り当てる（または既に割り当て済みの）IP を返す。
	// ok=false はその MAC に割り当てられる IP がないことを示す。
	Lookup(mac net.HardwareAddr) (ip net.IP, ok bool)
}
