// Package frame は Ethernet/IPv4/UDP をオフセット直読み・直書きするための薄い層。
// フルパース前に無関係なフレームを安価に弾けるようにするのが目的で、
// ゼロコピーの view を返すだけに留める（コピーが必要なら呼び出し側の責務）。
package frame

import (
	"encoding/binary"
	"net"
)

// EtherHeaderLen は Ethernet II ヘッダ（VLANタグなし）の長さ。
const EtherHeaderLen = 14

type EtherType uint16

const (
	EtherTypeIPv4 EtherType = 0x0800
	EtherTypeARP  EtherType = 0x0806
)

// Ethernet はフレーム先頭の Ethernet II ヘッダへの view。Dst/Src は b を直接指す。
type Ethernet struct {
	Dst, Src  net.HardwareAddr
	EtherType EtherType
	Payload   []byte
}

// ParseEthernet は b の先頭を Ethernet ヘッダとして読む。短すぎる場合は ok=false。
func ParseEthernet(b []byte) (eth Ethernet, ok bool) {
	if len(b) < EtherHeaderLen {
		return Ethernet{}, false
	}
	return Ethernet{
		Dst:       net.HardwareAddr(b[0:6]),
		Src:       net.HardwareAddr(b[6:12]),
		EtherType: EtherType(binary.BigEndian.Uint16(b[12:14])),
		Payload:   b[EtherHeaderLen:],
	}, true
}

// BuildEthernet は out の先頭に Ethernet ヘッダを書き込み、続くペイロード領域を返す。
func BuildEthernet(out []byte, srcMAC, dstMAC net.HardwareAddr, et EtherType) []byte {
	copy(out[0:6], dstMAC)
	copy(out[6:12], srcMAC)
	binary.BigEndian.PutUint16(out[12:14], uint16(et))
	return out[EtherHeaderLen:]
}
