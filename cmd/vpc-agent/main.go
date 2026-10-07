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
	"strconv"
	"strings"
	"syscall"

	"github.com/eve68k/vpc/internal/dhcp"
	"github.com/eve68k/vpc/internal/mapping"
	"github.com/eve68k/vpc/port"
)

func main() {
	kind := flag.String("port-kind", "afpacket", "Port 実装 (afpacket)")
	ifname := flag.String("port-if", "", "VM 側に向くインターフェース名 (tap / veth)")
	dhcpServerMAC := flag.String("dhcp-server-mac", "02:00:00:00:00:fe", "DHCPサーバとして名乗るMACアドレス")
	dhcpServerIP := flag.String("dhcp-server-ip", "10.10.0.254", "DHCPサーバ識別子・ゲートウェイとして配布するIP")
	dhcpSubnetMask := flag.String("dhcp-subnet-mask", "255.255.255.0", "配布するサブネットマスク")
	dhcpLeaseSeconds := flag.Uint("dhcp-lease-seconds", 3600, "配布するリース時間(秒)")
	dhcpPool := flag.String("dhcp-pool", "", "DHCPで配布するIPのカンマ区切りリスト（モックMappingService用）")
	flag.Parse()
	if *ifname == "" {
		log.Fatal("-port-if is required")
	}

	p, err := port.Open(*kind, *ifname)
	if err != nil {
		log.Fatalf("open port: %v", err)
	}
	defer p.Close()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	go func() { <-sig; p.Close() }()

	h, err := newDHCPHandler(*dhcpServerMAC, *dhcpServerIP, *dhcpSubnetMask, uint32(*dhcpLeaseSeconds), *dhcpPool)
	if err != nil {
		log.Fatalf("dhcp setup: %v", err)
	}

	log.Printf("capturing on %s (%s)", p.Name(), *kind)
	buf := make([]byte, 65536)
	for {
		n, err := p.ReadFrame(buf)
		if err != nil {
			if errors.Is(err, port.ErrClosed) {
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

func newDHCPHandler(serverMAC, serverIP, subnetMask string, leaseSeconds uint32, pool string) (*dhcp.Handler, error) {
	mac, err := net.ParseMAC(serverMAC)
	if err != nil {
		return nil, err
	}
	ip := net.ParseIP(serverIP)
	if ip == nil {
		return nil, errors.New("invalid -dhcp-server-ip")
	}
	mask := net.ParseIP(subnetMask)
	if mask == nil {
		return nil, errors.New("invalid -dhcp-subnet-mask")
	}

	ips, err := parseIPPool(pool)
	if err != nil {
		return nil, err
	}

	return &dhcp.Handler{
		Mapping:    mapping.NewMock(ips),
		ServerMAC:  mac,
		ServerIP:   ip,
		SubnetMask: mask,
		LeaseTime:  leaseSeconds,
	}, nil
}

func parseIPPool(pool string) ([]net.IP, error) {
	var ips []net.IP
	for _, s := range strings.Split(pool, ",") {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		ip := net.ParseIP(s)
		if ip == nil {
			return nil, errors.New("invalid IP in -dhcp-pool: " + strconv.Quote(s))
		}
		ips = append(ips, ip)
	}
	return ips, nil
}
