// vpc-agent は VM の L2 出口を掴み、フレームを読み取るエージェント。
// 1 プロセスで複数の VNI の Port を扱い、増減は -ctl-socket 経由で受け付ける。
// DHCP以外のフレームは Ethernet ヘッダをログ出力するだけ。カプセル化は今後追加する。
package main

import (
	"flag"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/eve68k/vpc/internal/agent"
	"github.com/eve68k/vpc/internal/ctl"
	"github.com/eve68k/vpc/internal/dhcp"
	"github.com/eve68k/vpc/internal/mapping"
	"github.com/eve68k/vpc/vni"
)

func main() {
	kind := flag.String("port-kind", "afpacket", "Port 実装 (afpacket)")
	ctlSocket := flag.String("ctl-socket", "/run/vpc-agent.sock", "VNI の増減を受け付ける unix socket のパス")
	dhcpServerMAC := flag.String("dhcp-server-mac", "02:00:00:00:00:fe", "DHCPサーバとして名乗るMACアドレス")
	ifname := flag.String("port-if", "", "起動時に Attach する Port のインターフェース名 (tap / veth)。省略可")
	vniMAC := flag.String("vni-mac", "", "-port-if のVNIのMACアドレス（モックMappingService用）")
	dhcpVPCID := flag.Uint("dhcp-vpc-id", 1, "-port-if のVNIが所属するVPCのID（モックMappingService用）")
	flag.Parse()

	serverMAC, err := net.ParseMAC(*dhcpServerMAC)
	if err != nil {
		log.Fatalf("-dhcp-server-mac: %v", err)
	}

	svc := mapping.NewMock(nil, nil)
	reg := mockRegistry{Mock: svc}
	m := &agent.Manager{
		Mapping: svc,
		Handler: &dhcp.Handler{Mapping: svc, ServerMAC: serverMAC},
		Open:    func(name string) (vni.Port, error) { return vni.Open(*kind, name) },
	}
	defer m.Close()

	if *ifname != "" {
		mac, err := net.ParseMAC(*vniMAC)
		if err != nil {
			log.Fatalf("-vni-mac is required with -port-if: %v", err)
		}
		reg.Register(mapping.VNI{Name: *ifname, VPCID: mapping.VPCID(*dhcpVPCID), MAC: mac})
		if err := m.Attach(*ifname); err != nil {
			log.Fatalf("attach %s: %v", *ifname, err)
		}
		log.Printf("attached %s (%s)", *ifname, *kind)
	}

	var src agent.Source = &ctl.Server{Path: *ctlSocket, Registry: reg}
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	go func() { <-sig; src.Close() }()

	log.Printf("listening on %s", *ctlSocket)
	if err := src.Run(m); err != nil {
		log.Fatalf("source: %v", err)
	}
}

// mockRegistry は ctl で受けた VNI を Mock に登録する。
// VPC の設定は本物のMappingServiceに差し替えるまでの暫定値で、どのVPCも同じ。
type mockRegistry struct{ *mapping.Mock }

func (r mockRegistry) Register(v mapping.VNI) {
	r.EnsureVPC(v.VPCID, defaultVPCConfig())
	r.PutVNI(v)
}

func (r mockRegistry) Unregister(name string) { r.DeleteVNI(name) }

func defaultVPCConfig() mapping.VPCConfig {
	_, subnet, _ := net.ParseCIDR("10.10.0.0/24")
	var pool []net.IP
	for i := 2; i < 254; i++ { // .1 はゲートウェイ
		pool = append(pool, net.IPv4(10, 10, 0, byte(i)))
	}
	return mapping.VPCConfig{Subnet: subnet, Pool: pool, LeaseTime: 3600}
}
