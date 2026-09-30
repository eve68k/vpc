//go:build !linux

package port

import "errors"

func openAFPacket(string) (Port, error) {
	return nil, errors.New("afpacket is only supported on linux")
}
