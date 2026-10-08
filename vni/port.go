// Package vni は VNI の L2 出口である Port（本番では tap、開発では veth）を抽象化する。
package vni

import "errors"

// ErrClosed は Close 済みの Port を操作したときに返る。
var ErrClosed = errors.New("port closed")

// Port は L2 フレームの読み書きができる NIC を表す。
// 本番: tap (/dev/net/tun) や AF_PACKET、将来: AF_XDP など。
// 開発/テスト: veth + AF_PACKET、またはメモリ実装。
type Port interface {
	Name() string
	// ReadFrame は 1 フレームを buf に読み込み、そのバイト数を返す。
	ReadFrame(buf []byte) (int, error)
	// WriteFrame は 1 フレームを送出する。
	WriteFrame(frame []byte) error
	Close() error
}
