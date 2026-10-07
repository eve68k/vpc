package frame

import "net"

// checksumSum はビッグエンディアン16bit単位の1の補数和を桁上げ処理前の32bitで返す。
// IPv4ヘッダ単体とUDP疑似ヘッダ+セグメントのように、複数区間の和を合算してから
// 一度だけ桁上げ処理したい場面があるため、畳み込みは Checksum / UDPChecksum 側で行う。
func checksumSum(data []byte) uint32 {
	var sum uint32
	n := len(data)
	for i := 0; i+1 < n; i += 2 {
		sum += uint32(data[i])<<8 | uint32(data[i+1])
	}
	if n%2 == 1 {
		sum += uint32(data[n-1]) << 8
	}
	return sum
}

func foldChecksum(sum uint32) uint16 {
	for sum>>16 != 0 {
		sum = (sum & 0xffff) + (sum >> 16)
	}
	return ^uint16(sum)
}

// Checksum はインターネット標準の1の補数和チェックサム（RFC 1071）を計算する。
func Checksum(data []byte) uint16 {
	return foldChecksum(checksumSum(data))
}

// UDPChecksum は udpSegment（チェックサムフィールドは0のままのUDPヘッダ+ペイロード）に対し、
// IPv4疑似ヘッダを含めたチェックサムを計算する。
func UDPChecksum(udpSegment []byte, srcIP, dstIP net.IP) uint16 {
	sum := pseudoHeaderSum(srcIP, dstIP, IPProtocolUDP, len(udpSegment))
	sum += checksumSum(udpSegment)
	return foldChecksum(sum)
}

// SetUDPChecksum は udpSegment のチェックサムフィールド（offset 6:8）を計算結果で埋める。
func SetUDPChecksum(udpSegment []byte, srcIP, dstIP net.IP) {
	udpSegment[6] = 0
	udpSegment[7] = 0
	c := UDPChecksum(udpSegment, srcIP, dstIP)
	udpSegment[6] = byte(c >> 8)
	udpSegment[7] = byte(c)
}

func pseudoHeaderSum(src, dst net.IP, proto IPProtocol, length int) uint32 {
	src4, dst4 := src.To4(), dst.To4()
	var sum uint32
	sum += uint32(src4[0])<<8 | uint32(src4[1])
	sum += uint32(src4[2])<<8 | uint32(src4[3])
	sum += uint32(dst4[0])<<8 | uint32(dst4[1])
	sum += uint32(dst4[2])<<8 | uint32(dst4[3])
	sum += uint32(proto)
	sum += uint32(length)
	return sum
}
