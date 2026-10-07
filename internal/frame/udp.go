package frame

import "encoding/binary"

// UDPHeaderLen は UDP ヘッダ長。
const UDPHeaderLen = 8

// UDP は UDP ヘッダへの view。
type UDP struct {
	SrcPort, DstPort uint16
	Payload          []byte
}

// ParseUDP は b の先頭を UDP ヘッダとして読む。Length フィールドでペイロードを切り出す。
func ParseUDP(b []byte) (u UDP, ok bool) {
	if len(b) < UDPHeaderLen {
		return UDP{}, false
	}
	length := int(binary.BigEndian.Uint16(b[4:6]))
	if length < UDPHeaderLen || length > len(b) {
		return UDP{}, false
	}
	return UDP{
		SrcPort: binary.BigEndian.Uint16(b[0:2]),
		DstPort: binary.BigEndian.Uint16(b[2:4]),
		Payload: b[UDPHeaderLen:length],
	}, true
}

// BuildUDP は UDP ヘッダを out に書き込み、続くペイロード領域を返す。
// チェックサムは IPv4 の送信元/宛先が確定してから SetUDPChecksum で別途計算する。
func BuildUDP(out []byte, srcPort, dstPort uint16, payloadLen int) []byte {
	length := UDPHeaderLen + payloadLen
	binary.BigEndian.PutUint16(out[0:2], srcPort)
	binary.BigEndian.PutUint16(out[2:4], dstPort)
	binary.BigEndian.PutUint16(out[4:6], uint16(length))
	binary.BigEndian.PutUint16(out[6:8], 0)
	return out[UDPHeaderLen:length]
}
