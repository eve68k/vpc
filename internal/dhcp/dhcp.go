// Package dhcp は DHCP (RFC 2131) メッセージの Parse/Build と、
// Discover/Offer, Request/Ack のハンドリングを提供する。
package dhcp

import (
	"encoding/binary"
	"errors"
	"net"
)

// headerLen は固定長ヘッダ（op 〜 file）のバイト数。magic cookie はこの直後。
const headerLen = 236

const magicCookie = 0x63825363

var (
	ErrTooShort  = errors.New("dhcp: frame too short")
	ErrBadCookie = errors.New("dhcp: bad magic cookie")
)

type OpCode uint8

const (
	OpBootRequest OpCode = 1
	OpBootReply   OpCode = 2
)

// MessageType は option 53 の値。RELEASE/NAK 等は後続issueで増える前提。
type MessageType uint8

const (
	MessageTypeDiscover MessageType = 1
	MessageTypeOffer    MessageType = 2
	MessageTypeRequest  MessageType = 3
	MessageTypeACK      MessageType = 5
)

type optionCode uint8

const (
	optionSubnetMask  optionCode = 1
	optionRequestedIP optionCode = 50
	optionLeaseTime   optionCode = 51
	optionMessageType optionCode = 53
	optionServerID    optionCode = 54
	optionPad         optionCode = 0
	optionEnd         optionCode = 255
)

// Message は DHCP メッセージ。オプションは最小限（type/requested IP/server ID/lease/subnet mask）のみ保持する。
type Message struct {
	Op     OpCode
	Xid    uint32
	Flags  uint16
	CIAddr net.IP
	YIAddr net.IP
	SIAddr net.IP
	GIAddr net.IP
	CHAddr net.HardwareAddr

	Type        MessageType
	RequestedIP net.IP
	ServerID    net.IP
	LeaseTime   uint32
	SubnetMask  net.IP
}

// Parse は DHCP メッセージをパースする。b は UDP ペイロード全体。
func Parse(b []byte) (*Message, error) {
	if len(b) < headerLen+4 {
		return nil, ErrTooShort
	}
	if binary.BigEndian.Uint32(b[236:240]) != magicCookie {
		return nil, ErrBadCookie
	}

	m := &Message{
		Op:     OpCode(b[0]),
		Xid:    binary.BigEndian.Uint32(b[4:8]),
		Flags:  binary.BigEndian.Uint16(b[10:12]),
		CIAddr: cloneIP(b[12:16]),
		YIAddr: cloneIP(b[16:20]),
		SIAddr: cloneIP(b[20:24]),
		GIAddr: cloneIP(b[24:28]),
		CHAddr: net.HardwareAddr(append([]byte(nil), b[28:34]...)),
	}
	if err := m.parseOptions(b[240:]); err != nil {
		return nil, err
	}
	return m, nil
}

func (m *Message) parseOptions(opts []byte) error {
	for i := 0; i < len(opts); {
		code := optionCode(opts[i])
		if code == optionPad {
			i++
			continue
		}
		if code == optionEnd {
			return nil
		}
		if i+1 >= len(opts) {
			return ErrTooShort
		}
		l := int(opts[i+1])
		i += 2
		if i+l > len(opts) {
			return ErrTooShort
		}
		v := opts[i : i+l]

		switch code {
		case optionMessageType:
			if l == 1 {
				m.Type = MessageType(v[0])
			}
		case optionRequestedIP:
			if l == 4 {
				m.RequestedIP = cloneIP(v)
			}
		case optionServerID:
			if l == 4 {
				m.ServerID = cloneIP(v)
			}
		case optionLeaseTime:
			if l == 4 {
				m.LeaseTime = binary.BigEndian.Uint32(v)
			}
		case optionSubnetMask:
			if l == 4 {
				m.SubnetMask = cloneIP(v)
			}
		}
		i += l
	}
	return nil
}

// Build はメッセージをシリアライズする。
func (m *Message) Build() []byte {
	out := make([]byte, headerLen+4, headerLen+4+32)
	out[0] = byte(m.Op)
	out[1] = 1 // htype: Ethernet
	out[2] = 6 // hlen
	binary.BigEndian.PutUint32(out[4:8], m.Xid)
	binary.BigEndian.PutUint16(out[10:12], m.Flags)
	putIP(out[12:16], m.CIAddr)
	putIP(out[16:20], m.YIAddr)
	putIP(out[20:24], m.SIAddr)
	putIP(out[24:28], m.GIAddr)
	copy(out[28:34], m.CHAddr)
	binary.BigEndian.PutUint32(out[236:240], magicCookie)

	out = appendOption(out, optionMessageType, []byte{byte(m.Type)})
	if m.RequestedIP != nil {
		out = appendOption(out, optionRequestedIP, m.RequestedIP.To4())
	}
	if m.ServerID != nil {
		out = appendOption(out, optionServerID, m.ServerID.To4())
	}
	if m.LeaseTime != 0 {
		lt := make([]byte, 4)
		binary.BigEndian.PutUint32(lt, m.LeaseTime)
		out = appendOption(out, optionLeaseTime, lt)
	}
	if m.SubnetMask != nil {
		out = appendOption(out, optionSubnetMask, m.SubnetMask.To4())
	}
	return append(out, byte(optionEnd))
}

func appendOption(out []byte, code optionCode, v []byte) []byte {
	out = append(out, byte(code), byte(len(v)))
	return append(out, v...)
}

func cloneIP(b []byte) net.IP {
	ip := make(net.IP, 4)
	copy(ip, b)
	return ip
}

func putIP(dst []byte, ip net.IP) {
	if ip == nil {
		return
	}
	copy(dst, ip.To4())
}
