package dhcp

import (
	"net"
	"testing"
)

func TestMessage_BuildしてParseすると同じ内容が復元される(t *testing.T) {
	cases := []struct {
		name string
		msg  Message
	}{
		{
			name: "Discover",
			msg: Message{
				Op:     OpBootRequest,
				Xid:    0x11223344,
				YIAddr: net.IPv4zero, // 固定長フィールドはワイヤ上「未設定」を表現できず常に0.0.0.0になる
				CHAddr: net.HardwareAddr{0x02, 0x00, 0x00, 0x00, 0x00, 0x01},
				Type:   MessageTypeDiscover,
			},
		},
		{
			name: "Offer（オプション一式あり）",
			msg: Message{
				Op:         OpBootReply,
				Xid:        0x11223344,
				YIAddr:     net.IPv4(10, 10, 0, 1),
				CHAddr:     net.HardwareAddr{0x02, 0x00, 0x00, 0x00, 0x00, 0x01},
				Type:       MessageTypeOffer,
				ServerID:   net.IPv4(10, 10, 0, 254),
				LeaseTime:  3600,
				SubnetMask: net.IPv4(255, 255, 255, 0),
			},
		},
		{
			name: "Request（requested IP指定）",
			msg: Message{
				Op:          OpBootRequest,
				Xid:         0x55,
				YIAddr:      net.IPv4zero,
				CHAddr:      net.HardwareAddr{0x02, 0x00, 0x00, 0x00, 0x00, 0x02},
				Type:        MessageTypeRequest,
				RequestedIP: net.IPv4(10, 10, 0, 1),
			},
		},
		{
			name: "Ack",
			msg: Message{
				Op:        OpBootReply,
				Xid:       0x55,
				YIAddr:    net.IPv4(10, 10, 0, 1),
				CHAddr:    net.HardwareAddr{0x02, 0x00, 0x00, 0x00, 0x00, 0x02},
				Type:      MessageTypeACK,
				ServerID:  net.IPv4(10, 10, 0, 254),
				LeaseTime: 1800,
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b := c.msg.Build()

			got, err := Parse(b)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}

			if got.Op != c.msg.Op {
				t.Errorf("Op: got %v, want %v", got.Op, c.msg.Op)
			}
			if got.Xid != c.msg.Xid {
				t.Errorf("Xid: got %v, want %v", got.Xid, c.msg.Xid)
			}
			if got.Type != c.msg.Type {
				t.Errorf("Type: got %v, want %v", got.Type, c.msg.Type)
			}
			if got.CHAddr.String() != c.msg.CHAddr.String() {
				t.Errorf("CHAddr: got %v, want %v", got.CHAddr, c.msg.CHAddr)
			}
			if !ipEqual(got.YIAddr, c.msg.YIAddr) {
				t.Errorf("YIAddr: got %v, want %v", got.YIAddr, c.msg.YIAddr)
			}
			if !ipEqual(got.RequestedIP, c.msg.RequestedIP) {
				t.Errorf("RequestedIP: got %v, want %v", got.RequestedIP, c.msg.RequestedIP)
			}
			if !ipEqual(got.ServerID, c.msg.ServerID) {
				t.Errorf("ServerID: got %v, want %v", got.ServerID, c.msg.ServerID)
			}
			if got.LeaseTime != c.msg.LeaseTime {
				t.Errorf("LeaseTime: got %v, want %v", got.LeaseTime, c.msg.LeaseTime)
			}
			if !ipEqual(got.SubnetMask, c.msg.SubnetMask) {
				t.Errorf("SubnetMask: got %v, want %v", got.SubnetMask, c.msg.SubnetMask)
			}
		})
	}
}

func ipEqual(a, b net.IP) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Equal(b)
}

func TestParse_短すぎるフレームはErrTooShortになる(t *testing.T) {
	_, err := Parse(make([]byte, 100))
	if err != ErrTooShort {
		t.Fatalf("got %v, want ErrTooShort", err)
	}
}

func TestParse_magic_cookieが異なるとErrBadCookieになる(t *testing.T) {
	b := (&Message{Op: OpBootRequest, Type: MessageTypeDiscover}).Build()
	b[236] = 0x00 // cookie を破壊

	_, err := Parse(b)
	if err != ErrBadCookie {
		t.Fatalf("got %v, want ErrBadCookie", err)
	}
}

func TestParse_オプションが無いメッセージはTypeがゼロ値になる(t *testing.T) {
	b := (&Message{Op: OpBootRequest}).Build()
	// message type オプション自体は常にBuildが付与するので、直接ヘッダ+cookieのみのバッファで確認する。
	raw := append(append([]byte{}, b[:236]...), 0x63, 0x82, 0x53, 0x63, 0xff)

	got, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got.Type != 0 {
		t.Fatalf("Type: got %v, want 0", got.Type)
	}
}
