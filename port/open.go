package port

import "fmt"

// Open は kind に応じた Port 実装を開く。
//
//	afpacket: 既存の NIC (tap/veth) を AF_PACKET で読み書きする（Linux のみ）
func Open(kind, ifname string) (Port, error) {
	switch kind {
	case "afpacket":
		return openAFPacket(ifname)
	default:
		return nil, fmt.Errorf("unknown port kind: %q", kind)
	}
}
