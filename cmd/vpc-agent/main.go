// vpc-agent は VM の L2 出口を掴み、フレームを読み取る最小のエージェント。
// 現状は Ethernet ヘッダをログ出力するだけ。マッピング問い合わせ・カプセル化は今後追加する。
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

	"github.com/eve68k/vpc/port"
)

func main() {
	kind := flag.String("port-kind", "afpacket", "Port 実装 (afpacket)")
	ifname := flag.String("port-if", "", "VM 側に向くインターフェース名 (tap / veth)")
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
		if n < 14 {
			continue
		}
		dst := net.HardwareAddr(buf[0:6])
		src := net.HardwareAddr(buf[6:12])
		et := binary.BigEndian.Uint16(buf[12:14])
		log.Printf("frame len=%d dst=%s src=%s ethertype=0x%04x", n, dst, src, et)
	}
}
