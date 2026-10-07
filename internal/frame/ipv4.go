package frame

import (
	"encoding/binary"
	"net"
)

// IPv4HeaderLen はオプションなしの IPv4 ヘッダ長。
const IPv4HeaderLen = 20

type IPProtocol uint8

const IPProtocolUDP IPProtocol = 17

// IPv4 は IPv4 ヘッダへの view。
type IPv4 struct {
	Protocol IPProtocol
	Src, Dst net.IP
	Payload  []byte
}

// ParseIPv4 は b の先頭を IPv4 ヘッダとして読む。IHL のオプション分は読み飛ばす。
func ParseIPv4(b []byte) (ip IPv4, ok bool) {
	if len(b) < IPv4HeaderLen {
		return IPv4{}, false
	}
	ihl := int(b[0]&0x0f) * 4
	if ihl < IPv4HeaderLen || len(b) < ihl {
		return IPv4{}, false
	}
	return IPv4{
		Protocol: IPProtocol(b[9]),
		Src:      net.IP(b[12:16]),
		Dst:      net.IP(b[16:20]),
		Payload:  b[ihl:],
	}, true
}

// BuildIPv4 はオプションなしの IPv4 ヘッダを out に書き込み、チェックサムを計算してから
// 続くペイロード領域を返す。payloadLen はヘッダに続くペイロードのバイト数。
func BuildIPv4(out []byte, proto IPProtocol, src, dst net.IP, payloadLen int) []byte {
	out[0] = 0x45 // version=4, IHL=5
	out[1] = 0
	binary.BigEndian.PutUint16(out[2:4], uint16(IPv4HeaderLen+payloadLen))
	binary.BigEndian.PutUint16(out[4:6], 0)
	binary.BigEndian.PutUint16(out[6:8], 0)
	out[8] = 64 // TTL
	out[9] = byte(proto)
	binary.BigEndian.PutUint16(out[10:12], 0)
	copy(out[12:16], src.To4())
	copy(out[16:20], dst.To4())
	binary.BigEndian.PutUint16(out[10:12], Checksum(out[:IPv4HeaderLen]))
	return out[IPv4HeaderLen:]
}
