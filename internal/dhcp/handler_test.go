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
	testServerIP  = net.IPv4(10, 10, 0, 254)
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

func newTestHandler() *Handler {
	pool := []net.IP{net.IPv4(10, 10, 0, 1), net.IPv4(10, 10, 0, 2)}
	return &Handler{
		Mapping:    mapping.NewMock(pool),
		ServerMAC:  testServerMAC,
		ServerIP:   testServerIP,
		SubnetMask: net.IPv4(255, 255, 255, 0),
		LeaseTime:  3600,
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
	if !msg.YIAddr.Equal(net.IPv4(10, 10, 0, 1)) {
		t.Errorf("YIAddr: got %v, want 10.10.0.1", msg.YIAddr)
	}
	_ = ip
}

func TestHandler_RequestしたIPがMappingServiceと一致すればAckが届く(t *testing.T) {
	a, b := port.NewMemPair("client", "server")
	h := newTestHandler()

	// 同じMACで先にDiscoverさせ、割り当てを確定させる。
	allocated, _ := h.Mapping.Lookup(testClientMAC)

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
	h.Mapping.Lookup(testClientMAC) // 先に割り当てておく

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
