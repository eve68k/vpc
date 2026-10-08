package dhcp

import (
	"net"

	"github.com/eve68k/vpc/internal/frame"
	"github.com/eve68k/vpc/internal/mapping"
	"github.com/eve68k/vpc/vni"
)

const (
	ClientPort = 68
	ServerPort = 67
)

// Handler は Discover/Offer, Request/Ack を処理する。
// フロー全体の状態は追わず（Discoverを覚えていなくてもRequestだけ単独で処理できる）、
// MAC↔IPの対応という「状態」はMappingServiceを正本として都度問い合わせる。
type Handler struct {
	Mapping   mapping.Service
	ServerMAC net.HardwareAddr // VPC共通の固定MAC
}

// HandleFrame はフレームを受け取り、クライアント→サーバ方向のDHCPメッセージであれば
// 処理して p に応答を書き込む。DHCP以外のフレームは何もせず handled=false を返す。
func (h *Handler) HandleFrame(b []byte, p vni.Port) (handled bool, err error) {
	eth, _, udp, ok := identifyRequest(b)
	if !ok {
		return false, nil
	}

	msg, err := Parse(udp.Payload)
	if err != nil {
		return true, err
	}

	vpcID, ok := h.Mapping.VPCForPort(p.Name())
	if !ok {
		return true, nil
	}

	var reply *Message
	var serverIP net.IP
	switch msg.Type {
	case MessageTypeDiscover:
		reply, serverIP = h.handleDiscover(vpcID, msg)
	case MessageTypeRequest:
		reply, serverIP = h.handleRequest(vpcID, msg)
	default:
		return true, nil
	}
	if reply == nil {
		return true, nil
	}

	return true, p.WriteFrame(h.buildReplyFrame(eth.Src, serverIP, reply))
}

func (h *Handler) handleDiscover(vpcID mapping.VPCID, req *Message) (*Message, net.IP) {
	lease, ok := h.Mapping.Lookup(vpcID, req.CHAddr)
	if !ok {
		return nil, nil
	}
	return h.reply(req, MessageTypeOffer, lease)
}

func (h *Handler) handleRequest(vpcID mapping.VPCID, req *Message) (*Message, net.IP) {
	lease, ok := h.Mapping.Lookup(vpcID, req.CHAddr)
	if !ok || req.RequestedIP == nil || !lease.IP.Equal(req.RequestedIP) {
		return nil, nil // 対応が存在しなければ無視（NAKは今後）
	}
	return h.reply(req, MessageTypeACK, lease)
}

// reply は応答メッセージと、応答の送信元に使うサーバIP（ゲートウェイ）を返す。
func (h *Handler) reply(req *Message, t MessageType, lease mapping.Lease) (*Message, net.IP) {
	serverIP := gatewayOf(lease.Subnet)
	return &Message{
		Op:         OpBootReply,
		Xid:        req.Xid,
		YIAddr:     lease.IP,
		CHAddr:     req.CHAddr,
		Type:       t,
		ServerID:   serverIP,
		LeaseTime:  lease.LeaseTime,
		SubnetMask: net.IP(lease.Subnet.Mask),
		Router:     serverIP,
	}, serverIP
}

// gatewayOf は subnet のネットワークアドレス+1 を返す。
// MappingServiceにゲートウェイIPを返させず、サブネットから導出する。
func gatewayOf(subnet *net.IPNet) net.IP {
	base := subnet.IP.To4()
	ip := make(net.IP, net.IPv4len)
	copy(ip, base)
	ip[3]++
	return ip
}

// identifyRequest はフレームが「クライアント→サーバ方向のDHCPメッセージ」かどうかを
// 段階的な浅い判定（EtherType → IPのProtocol → UDPポート）で確認する。
// 無関係なフレームをフルパース前に安価に弾くのが目的。
func identifyRequest(b []byte) (eth frame.Ethernet, ip frame.IPv4, udp frame.UDP, ok bool) {
	eth, ok = frame.ParseEthernet(b)
	if !ok || eth.EtherType != frame.EtherTypeIPv4 {
		return frame.Ethernet{}, frame.IPv4{}, frame.UDP{}, false
	}

	ip, ok = frame.ParseIPv4(eth.Payload)
	if !ok || ip.Protocol != frame.IPProtocolUDP {
		return frame.Ethernet{}, frame.IPv4{}, frame.UDP{}, false
	}

	udp, ok = frame.ParseUDP(ip.Payload)
	if !ok || udp.SrcPort != ClientPort || udp.DstPort != ServerPort {
		return frame.Ethernet{}, frame.IPv4{}, frame.UDP{}, false
	}

	return eth, ip, udp, true
}

func (h *Handler) buildReplyFrame(clientMAC net.HardwareAddr, serverIP net.IP, reply *Message) []byte {
	payload := reply.Build()
	udpLen := frame.UDPHeaderLen + len(payload)
	out := make([]byte, frame.EtherHeaderLen+frame.IPv4HeaderLen+udpLen)

	dstIP := net.IPv4bcast
	ipPayload := frame.BuildEthernet(out, h.ServerMAC, clientMAC, frame.EtherTypeIPv4)
	udpSegment := frame.BuildIPv4(ipPayload, frame.IPProtocolUDP, serverIP, dstIP, udpLen)
	dhcpPayload := frame.BuildUDP(udpSegment, ServerPort, ClientPort, len(payload))
	copy(dhcpPayload, payload)
	frame.SetUDPChecksum(udpSegment, serverIP, dstIP)

	return out
}
