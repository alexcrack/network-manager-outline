// Package tun opens a Linux TUN device via /dev/net/tun.
//
// Only the fd is needed: NetworkManager assigns addresses, routes and brings
// the link up based on the Config/Ip4Config signals.
package tun

import (
	"errors"
	"os"
)

const DefaultName = "outline%d"

type Device struct {
	*os.File
	Name string
}

// Open creates a TUN device (IFF_TUN|IFF_NO_PI). name may contain %d.
// TODO(phase1): ioctl(TUNSETIFF) using golang.org/x/sys/unix.
func Open(name string) (*Device, error) {
	return nil, errors.New("not implemented")
}
