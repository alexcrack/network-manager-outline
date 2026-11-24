package outline

import (
	"errors"
	"io"
	"net/netip"
)

// Device is the userspace network stack: packets read from the TUN fd are
// terminated by lwIP and re-dialed through the Outline transport.
//
// Built from outline-sdk:
//   - configurl.New[transport.StreamDialer] / [transport.PacketListener]
//   - packetrelay.NewPacketRelayFromPacketListener (+ dnstruncate fallback)
//   - lwip2transport.ConfigureDeviceWithRelay
type Device struct {
	ServerIP netip.Addr
}

// NewDevice creates the lwIP device for the given transport config.
// TODO(phase1): implement following x/examples/outline-cli/outline_device.go.
func NewDevice(transport string, serverIP netip.Addr) (*Device, error) {
	return nil, errors.New("not implemented")
}

// Pump copies packets between the TUN device and the lwIP stack until either
// side closes. TODO(phase1).
func (d *Device) Pump(tun io.ReadWriteCloser) error {
	return errors.New("not implemented")
}

// CheckUDP probes UDP support and switches to DNS-truncate mode if unsupported.
// TODO(phase1).
func (d *Device) CheckUDP() error { return nil }

func (d *Device) Close() error { return nil }
