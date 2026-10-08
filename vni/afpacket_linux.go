//go:build linux

package vni

import (
	"fmt"
	"net"
	"sync/atomic"
	"syscall"
)

// AFPacket は AF_PACKET (SOCK_RAW) で単一インターフェースの L2 フレームを読み書きする。
type AFPacket struct {
	fd     int
	name   string
	closed atomic.Bool
}

func htons(v uint16) uint16 { return v<<8 | v>>8 }

func openAFPacket(ifname string) (Port, error) {
	ifi, err := net.InterfaceByName(ifname)
	if err != nil {
		return nil, err
	}
	proto := htons(syscall.ETH_P_ALL)
	fd, err := syscall.Socket(syscall.AF_PACKET, syscall.SOCK_RAW|syscall.SOCK_CLOEXEC, int(proto))
	if err != nil {
		return nil, fmt.Errorf("socket: %w", err)
	}
	if err := syscall.Bind(fd, &syscall.SockaddrLinklayer{Protocol: proto, Ifindex: ifi.Index}); err != nil {
		syscall.Close(fd)
		return nil, fmt.Errorf("bind %s: %w", ifname, err)
	}
	return &AFPacket{fd: fd, name: ifname}, nil
}

func (p *AFPacket) Name() string { return p.name }

func (p *AFPacket) ReadFrame(buf []byte) (int, error) {
	for {
		n, from, err := syscall.Recvfrom(p.fd, buf, 0)
		if err != nil {
			if err == syscall.EINTR {
				continue
			}
			if p.closed.Load() {
				return 0, ErrClosed
			}
			return 0, err
		}
		// 自分自身が WriteFrame した送出フレームは読み飛ばす。
		if ll, ok := from.(*syscall.SockaddrLinklayer); ok && ll.Pkttype == syscall.PACKET_OUTGOING {
			continue
		}
		return n, nil
	}
}

func (p *AFPacket) WriteFrame(frame []byte) error {
	if p.closed.Load() {
		return ErrClosed
	}
	_, err := syscall.Write(p.fd, frame)
	return err
}

func (p *AFPacket) Close() error {
	if p.closed.Swap(true) {
		return nil
	}
	return syscall.Close(p.fd)
}
