// Package tun opens a Linux TUN device via /dev/net/tun.
//
// NetworkManager assigns addresses and routes from the Config/Ip4Config
// signals; this package only creates the device, sets its MTU and brings it up.
package tun

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// DefaultName lets the kernel pick the first free outlineN name.
const DefaultName = "outline%d"

// Device is an open TUN device. Read and Write transfer one IP packet per call.
// Close unblocks pending reads.
type Device struct {
	*os.File
	name string
}

// Name returns the interface name chosen by the kernel.
func (d *Device) Name() string { return d.name }

// Open creates a TUN device (IFF_TUN|IFF_NO_PI). name may contain %d.
// Requires CAP_NET_ADMIN.
func Open(name string, mtu int) (*Device, error) {
	// O_NONBLOCK makes os.NewFile use the runtime poller, so Close unblocks Read.
	fd, err := unix.Open("/dev/net/tun", unix.O_RDWR|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, fmt.Errorf("open /dev/net/tun: %w", err)
	}
	ifr, err := unix.NewIfreq(name)
	if err != nil {
		unix.Close(fd)
		return nil, fmt.Errorf("tun name %q: %w", name, err)
	}
	ifr.SetUint16(unix.IFF_TUN | unix.IFF_NO_PI)
	if err := unix.IoctlIfreq(fd, unix.TUNSETIFF, ifr); err != nil {
		unix.Close(fd)
		return nil, fmt.Errorf("TUNSETIFF %q: %w", name, err)
	}
	d := &Device{File: os.NewFile(uintptr(fd), "/dev/net/tun"), name: ifr.Name()}
	if err := setUp(d.name, mtu); err != nil {
		d.Close()
		return nil, err
	}
	return d, nil
}

// setUp sets the MTU and the IFF_UP flag through an AF_INET control socket.
func setUp(name string, mtu int) error {
	s, err := unix.Socket(unix.AF_INET, unix.SOCK_DGRAM|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		return fmt.Errorf("control socket: %w", err)
	}
	defer unix.Close(s)

	ifr, err := unix.NewIfreq(name)
	if err != nil {
		return err
	}
	if mtu > 0 {
		ifr.SetUint32(uint32(mtu))
		if err := unix.IoctlIfreq(s, unix.SIOCSIFMTU, ifr); err != nil {
			return fmt.Errorf("set MTU of %s: %w", name, err)
		}
	}
	if err := unix.IoctlIfreq(s, unix.SIOCGIFFLAGS, ifr); err != nil {
		return fmt.Errorf("get flags of %s: %w", name, err)
	}
	ifr.SetUint16(ifr.Uint16() | unix.IFF_UP)
	if err := unix.IoctlIfreq(s, unix.SIOCSIFFLAGS, ifr); err != nil {
		return fmt.Errorf("bring %s up: %w", name, err)
	}
	return nil
}
