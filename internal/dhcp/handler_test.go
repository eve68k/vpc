package dhcp

import (
	"net"
	"testing"
	"time"

	"github.com/eve68k/vpc/internal/frame"
	"github.com/eve68k/vpc/internal/mapping"
	"github.com/eve68k/vpc/port"
)

var (
	testServerMAC = net.HardwareAddr{0x02, 0x00, 0x00, 0x00, 0x00, 0xfe}
	testServerIP  = net.IPv4(10, 10, 0, 1)
	testClientMAC = net.HardwareAddr{0x02, 0x00, 0x00, 0x00, 0x00, 0x01}
)

// buildClientFrame はクライアント→サーバ方向のDHCPフレーム（Ethernet+IPv4+UDP+DHCP）を組み立てる。
func buildClientFrame(t *testing.T, msg *Message) []byte {
	t.Helper()
	payload := msg.Build()
	udpLen := frame.UDPHeaderLen + len(payload)
	out := make([]byte, frame.EtherHeaderLen+frame.IPv4HeaderLen+udpLen)

	srcIP, dstIP := net.IPv4zero, net.IPv4bcast
	ipPayload := frame.BuildEthernet(out, testClientMAC, testServerMAC, frame.EtherTypeIPv4)
	udpSegment := frame.BuildIPv4(ipPayload, frame.IPProtocolUDP, srcIP, dstIP, udpLen)
	dhcpPayload := frame.BuildUDP(udpSegment, ClientPort, ServerPort, len(payload))
	copy(dhcpPayload, payload)
	frame.SetUDPChecksum(udpSegment, srcIP, dstIP)

	return out
}

const testVPCID mapping.VPCID = 1

// newTestHandler は "server" という名前のPortがVPC 1（10.10.0.0/24）に所属するHandlerを返す。
func newTestHandler() *Handler {
	_, subnet, _ := net.ParseCIDR("10.10.0.0/24")
	pool := []net.IP{net.IPv4(10, 10, 0, 2), net.IPv4(10, 10, 0, 3)}
	return &Handler{
		Mapping: mapping.NewMock(
			map[string]mapping.VPCID{"server": testVPCID},
			map[mapping.VPCID]mapping.VPCConfig{testVPCID: {Subnet: subnet, Pool: pool, LeaseTime: 3600}},
		),
		ServerMAC: testServerMAC,
	}
}

func TestIdentifyRequest_DHCPではないフレームは判定されない(t *testing.T) {
	discover := buildClientFrame(t, &Message{Op: OpBootRequest, CHAddr: testClientMAC, Type: MessageTypeDiscover})

	cases := []struct {
		name  string
		frame []byte
	}{
		{
			name: "EtherTypeがIPv4以外",
			frame: func() []byte {
				f := append([]byte{}, discover...)
				f[12], f[13] = 0x08, 0x06 // ARP
				return f
			}(),
		},
		{
			name: "IPのProtocolがUDP以外",
			frame: func() []byte {
				f := append([]byte{}, discover...)
				f[14+9] = 6 // TCP
				return f
			}(),
		},
		{
			name: "UDPの送信元ポートが68以外",
			frame: func() []byte {
				f := append([]byte{}, discover...)
				ipStart := 14
				udpStart := ipStart + 20
				f[udpStart], f[udpStart+1] = 0x00, 0x35 // port 53
				return f
			}(),
		},
		{
			name:  "短すぎるフレーム",
			frame: discover[:10],
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, _, _, ok := identifyRequest(c.frame); ok {
				t.Fatalf("got ok=true, want false")
			}
		})
	}
}

func TestIdentifyRequest_正しいDHCPフレームは判定される(t *testing.T) {
	discover := buildClientFrame(t, &Message{Op: OpBootRequest, CHAddr: testClientMAC, Type: MessageTypeDiscover})

	if _, _, _, ok := identifyRequest(discover); !ok {
		t.Fatalf("got ok=false, want true")
	}
}

func TestHandler_DiscoverフレームからOfferが届く(t *testing.T) {
	a, b := port.NewMemPair("client", "server")
	h := newTestHandler()

	discover := buildClientFrame(t, &Message{Op: OpBootRequest, CHAddr: testClientMAC, Type: MessageTypeDiscover})
	if err := a.WriteFrame(discover); err != nil {
		t.Fatal(err)
	}

	buf := make([]byte, 2048)
	n, err := b.ReadFrame(buf)
	if err != nil {
		t.Fatal(err)
	}

	handled, err := h.HandleFrame(buf[:n], b)
	if err != nil {
		t.Fatalf("HandleFrame: %v", err)
	}
	if !handled {
		t.Fatalf("got handled=false, want true")
	}

	replyBuf := make([]byte, 2048)
	n, err = a.ReadFrame(replyBuf)
	if err != nil {
		t.Fatalf("ReadFrame (reply): %v", err)
	}

	eth, ip, udp, ok := identifyReply(replyBuf[:n])
	if !ok {
		t.Fatalf("reply frame did not parse as Ethernet/IPv4/UDP")
	}
	if eth.Dst.String() != testClientMAC.String() {
		t.Errorf("dst MAC: got %v, want %v", eth.Dst, testClientMAC)
	}
	if udp.SrcPort != ServerPort || udp.DstPort != ClientPort {
		t.Errorf("ports: got %d->%d, want %d->%d", udp.SrcPort, udp.DstPort, ServerPort, ClientPort)
	}

	msg, err := Parse(udp.Payload)
	if err != nil {
		t.Fatalf("Parse reply: %v", err)
	}
	if msg.Type != MessageTypeOffer {
		t.Errorf("Type: got %v, want Offer", msg.Type)
	}
	if !msg.YIAddr.Equal(net.IPv4(10, 10, 0, 2)) {
		t.Errorf("YIAddr: got %v, want 10.10.0.2", msg.YIAddr)
	}
	if !ip.Src.Equal(testServerIP) {
		t.Errorf("src IP: got %v, want %v", ip.Src, testServerIP)
	}
	if !msg.ServerID.Equal(testServerIP) {
		t.Errorf("ServerID: got %v, want %v", msg.ServerID, testServerIP)
	}
	if want := net.IPv4(255, 255, 255, 0); !msg.SubnetMask.Equal(want) {
		t.Errorf("SubnetMask: got %v, want %v", msg.SubnetMask, want)
	}
	if msg.LeaseTime != 3600 {
		t.Errorf("LeaseTime: got %d, want 3600", msg.LeaseTime)
	}
}

func TestHandler_RequestしたIPがMappingServiceと一致すればAckが届く(t *testing.T) {
	a, b := port.NewMemPair("client", "server")
	h := newTestHandler()

	// 同じMACで先にDiscoverさせ、割り当てを確定させる。
	lease, _ := h.Mapping.Lookup(testVPCID, testClientMAC)
	allocated := lease.IP

	request := buildClientFrame(t, &Message{
		Op:          OpBootRequest,
		CHAddr:      testClientMAC,
		Type:        MessageTypeRequest,
		RequestedIP: allocated,
	})
	if err := a.WriteFrame(request); err != nil {
		t.Fatal(err)
	}

	buf := make([]byte, 2048)
	n, err := b.ReadFrame(buf)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.HandleFrame(buf[:n], b); err != nil {
		t.Fatalf("HandleFrame: %v", err)
	}

	replyBuf := make([]byte, 2048)
	n, err = a.ReadFrame(replyBuf)
	if err != nil {
		t.Fatalf("ReadFrame (reply): %v", err)
	}
	_, _, udp, ok := identifyReply(replyBuf[:n])
	if !ok {
		t.Fatalf("reply frame did not parse as Ethernet/IPv4/UDP")
	}
	msg, err := Parse(udp.Payload)
	if err != nil {
		t.Fatalf("Parse reply: %v", err)
	}
	if msg.Type != MessageTypeACK {
		t.Errorf("Type: got %v, want Ack", msg.Type)
	}
}

func TestHandler_割り当てと異なるRequestedIPは無視される(t *testing.T) {
	a, b := port.NewMemPair("client", "server")
	h := newTestHandler()
	h.Mapping.Lookup(testVPCID, testClientMAC) // 先に割り当てておく

	request := buildClientFrame(t, &Message{
		Op:          OpBootRequest,
		CHAddr:      testClientMAC,
		Type:        MessageTypeRequest,
		RequestedIP: net.IPv4(192, 168, 0, 1), // 割り当てと異なるIP
	})
	if err := a.WriteFrame(request); err != nil {
		t.Fatal(err)
	}

	buf := make([]byte, 2048)
	n, err := b.ReadFrame(buf)
	if err != nil {
		t.Fatal(err)
	}
	handled, err := h.HandleFrame(buf[:n], b)
	if err != nil {
		t.Fatalf("HandleFrame: %v", err)
	}
	if !handled {
		t.Fatalf("got handled=false, want true")
	}

	result := make(chan []byte, 1)
	go func() {
		rb := make([]byte, 2048)
		if n, err := a.ReadFrame(rb); err == nil {
			result <- rb[:n]
		}
	}()
	select {
	case <-result:
		t.Fatalf("got a reply frame, want none")
	case <-time.After(50 * time.Millisecond):
	}
	a.Close()
	b.Close()
}

func TestHandler_VPCに属さないPortのフレームには応答しない(t *testing.T) {
	a, b := port.NewMemPair("client", "unknown")
	h := newTestHandler()

	discover := buildClientFrame(t, &Message{Op: OpBootRequest, CHAddr: testClientMAC, Type: MessageTypeDiscover})
	handled, err := h.HandleFrame(discover, b)
	if err != nil {
		t.Fatalf("HandleFrame: %v", err)
	}
	if !handled {
		t.Fatalf("got handled=false, want true")
	}

	result := make(chan struct{}, 1)
	go func() {
		rb := make([]byte, 2048)
		if _, err := a.ReadFrame(rb); err == nil {
			result <- struct{}{}
		}
	}()
	select {
	case <-result:
		t.Fatalf("got a reply frame, want none")
	case <-time.After(50 * time.Millisecond):
	}
	a.Close()
	b.Close()
}

func TestGatewayOf_ネットワークアドレスの次のIPを返す(t *testing.T) {
	_, subnet, _ := net.ParseCIDR("192.168.5.0/24")
	if got, want := gatewayOf(subnet), net.IPv4(192, 168, 5, 1); !got.Equal(want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// identifyReply はテスト専用の、サーバ→クライアント方向フレーム用の浅い判定ヘルパー。
func identifyReply(b []byte) (eth frame.Ethernet, ip frame.IPv4, udp frame.UDP, ok bool) {
	eth, ok = frame.ParseEthernet(b)
	if !ok || eth.EtherType != frame.EtherTypeIPv4 {
		return frame.Ethernet{}, frame.IPv4{}, frame.UDP{}, false
	}
	ip, ok = frame.ParseIPv4(eth.Payload)
	if !ok || ip.Protocol != frame.IPProtocolUDP {
		return frame.Ethernet{}, frame.IPv4{}, frame.UDP{}, false
	}
	udp, ok = frame.ParseUDP(ip.Payload)
	if !ok {
		return frame.Ethernet{}, frame.IPv4{}, frame.UDP{}, false
	}
	return eth, ip, udp, true
}
