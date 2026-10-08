// vpc-agent は VM の L2 出口を掴み、フレームを読み取る最小のエージェント。
// DHCP以外のフレームは Ethernet ヘッダをログ出力するだけ。カプセル化は今後追加する。
package main

import (
	"encoding/binary"
	"errors"
	"flag"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/eve68k/vpc/internal/dhcp"
	"github.com/eve68k/vpc/internal/mapping"
	"github.com/eve68k/vpc/vni"
)

func main() {
	kind := flag.String("port-kind", "afpacket", "Port 実装 (afpacket)")
	ifname := flag.String("port-if", "", "VM 側に向くインターフェース名 (tap / veth)")
	dhcpServerMAC := flag.String("dhcp-server-mac", "02:00:00:00:00:fe", "DHCPサーバとして名乗るMACアドレス")
	dhcpVPCID := flag.Uint("dhcp-vpc-id", 1, "このPortが所属するVPCのID（モックMappingService用）")
	flag.Parse()
	if *ifname == "" {
		log.Fatal("-port-if is required")
	}

	p, err := vni.Open(*kind, *ifname)
	if err != nil {
		log.Fatalf("open port: %v", err)
	}
	defer p.Close()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	go func() { <-sig; p.Close() }()

	h, err := newDHCPHandler(*dhcpServerMAC, p.Name(), mapping.VPCID(*dhcpVPCID))
	if err != nil {
		log.Fatalf("dhcp setup: %v", err)
	}

	log.Printf("capturing on %s (%s)", p.Name(), *kind)
	buf := make([]byte, 65536)
	for {
		n, err := p.ReadFrame(buf)
		if err != nil {
			if errors.Is(err, vni.ErrClosed) {
				return
			}
			log.Fatalf("read: %v", err)
		}

		handled, err := h.HandleFrame(buf[:n], p)
		if err != nil {
			log.Printf("dhcp: %v", err)
			continue
		}
		if handled {
			continue
		}

		if n < 14 {
			continue
		}
		dst := net.HardwareAddr(buf[0:6])
		src := net.HardwareAddr(buf[6:12])
		et := binary.BigEndian.Uint16(buf[12:14])
		log.Printf("frame len=%d dst=%s src=%s ethertype=0x%04x", n, dst, src, et)
	}
}

func newDHCPHandler(serverMAC, portName string, vpcID mapping.VPCID) (*dhcp.Handler, error) {
	mac, err := net.ParseMAC(serverMAC)
	if err != nil {
		return nil, err
	}

	// 本物のMappingServiceクライアントに差し替えるまでの暫定値。
	_, subnet, err := net.ParseCIDR("10.10.0.0/24")
	if err != nil {
		return nil, err
	}
	var pool []net.IP
	for i := 2; i < 254; i++ { // .1 はゲートウェイ
		pool = append(pool, net.IPv4(10, 10, 0, byte(i)))
	}

	return &dhcp.Handler{
		Mapping: mapping.NewMock(
			map[string]mapping.VPCID{portName: vpcID},
			map[mapping.VPCID]mapping.VPCConfig{vpcID: {Subnet: subnet, Pool: pool, LeaseTime: 3600}},
		),
		ServerMAC: mac,
	}, nil
}
