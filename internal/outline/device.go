package outline

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/netip"
	"os"
	"sync"
	"time"

	"golang.getoutline.org/sdk/dns"
	"golang.getoutline.org/sdk/network"
	"golang.getoutline.org/sdk/network/dnstruncate"
	"golang.getoutline.org/sdk/network/lwip2transport"
	"golang.getoutline.org/sdk/network/packetrelay"
	"golang.getoutline.org/sdk/transport"
	"golang.getoutline.org/sdk/x/configurl"
	"golang.getoutline.org/sdk/x/connectivity"
)

const (
	// MTU of the lwIP device (lwip2transport's packetMTU).
	MTU = 1500
	// udpIdleTimeout ends a UDP association after this long without sends.
	udpIdleTimeout = 30 * time.Second
	// probeDomain is resolved through the tunnel to test connectivity.
	probeDomain = "www.google.com"
)

var registry = configurl.NewDefaultRegistry()

// Device is the userspace network stack: IP packets from the TUN device are
// terminated by lwIP and re-dialed through the Outline transport.
//
// UDP starts in DNS-truncate mode (DNS answers carry TC=1 so resolvers retry
// over TCP) and switches to relaying through the server once ProbeUDP
// confirms the server supports UDP.
//
// lwIP is a process-wide singleton: only one Device may exist at a time.
type Device struct {
	server   netip.AddrPort
	ip       network.IPDevice
	sd       transport.StreamDialer
	pl       transport.PacketListener
	relay    packetrelay.DelegatePacketRelay
	remote   packetrelay.PacketRelay
	fallback packetrelay.PacketRelay

	closeOnce sync.Once
}

// NewDevice builds the transport stack. It does no network I/O.
func NewDevice(ctx context.Context, t Transport) (*Device, error) {
	d := &Device{server: t.Server}
	var err error
	if d.sd, err = configurl.New[transport.StreamDialer](ctx, registry, t.Stream); err != nil {
		return nil, fmt.Errorf("create stream dialer: %w", err)
	}
	if d.pl, err = configurl.New[transport.PacketListener](ctx, registry, t.Packet); err != nil {
		return nil, fmt.Errorf("create packet listener: %w", err)
	}
	if d.remote, err = packetrelay.NewPacketRelayFromPacketListener(d.pl, udpIdleTimeout); err != nil {
		return nil, fmt.Errorf("create UDP relay: %w", err)
	}
	if d.fallback, err = dnstruncate.NewPacketRelay(); err != nil {
		return nil, fmt.Errorf("create DNS-truncate relay: %w", err)
	}
	if d.relay, err = packetrelay.NewDelegatePacketRelay(d.fallback); err != nil {
		return nil, fmt.Errorf("create delegate relay: %w", err)
	}
	if d.ip, err = lwip2transport.ConfigureDeviceWithRelay(d.sd, d.relay); err != nil {
		return nil, fmt.Errorf("configure lwIP: %w", err)
	}
	return d, nil
}

// CheckServer opens and closes a plain TCP connection to the server.
func (d *Device) CheckServer(ctx context.Context) error {
	var dialer net.Dialer
	c, err := dialer.DialContext(ctx, "tcp", d.server.String())
	if err != nil {
		return err
	}
	return c.Close()
}

// IsTimeout reports whether err is a timeout. Outline servers do not reply
// to a wrong key: they drain the connection until the client gives up, so
// after CheckServer succeeded, a CheckTCP timeout means a wrong access key.
func IsTimeout(err error) bool {
	var ne net.Error
	return errors.Is(err, context.DeadlineExceeded) || errors.Is(err, os.ErrDeadlineExceeded) ||
		(errors.As(err, &ne) && ne.Timeout())
}

// CheckTCP resolves probeDomain over TCP through the tunnel. It fails if the
// server is unreachable or rejects the credentials.
func (d *Device) CheckTCP(ctx context.Context, resolver netip.Addr) error {
	r := dns.NewTCPResolver(d.sd, netip.AddrPortFrom(resolver, 53).String())
	result, err := connectivity.TestConnectivityWithResolver(ctx, r, probeDomain)
	if err != nil {
		return err
	}
	if result != nil {
		return fmt.Errorf("%s: %w", result.Op, result.Err)
	}
	return nil
}

// ProbeUDP tests whether the server relays UDP and switches the UDP relay
// accordingly. It returns whether UDP is relayed through the server.
func (d *Device) ProbeUDP(ctx context.Context, resolver netip.Addr) (bool, error) {
	r := dns.NewUDPResolver(transport.PacketListenerDialer{Listener: d.pl}, netip.AddrPortFrom(resolver, 53).String())
	result, err := connectivity.TestConnectivityWithResolver(ctx, r, probeDomain)
	if err != nil {
		return false, err // inconclusive; keep the current mode
	}
	if result != nil {
		d.relay.SetRelay(d.fallback)
		return false, nil
	}
	d.relay.SetRelay(d.remote)
	return true, nil
}

// Pump copies packets between tun and the lwIP stack until one side fails
// or is closed. It returns the first error.
func (d *Device) Pump(tun io.ReadWriter, log *slog.Logger) error {
	errc := make(chan error, 2)
	go func() {
		n, err := io.Copy(d.ip, tun)
		log.Debug("tun -> lwIP stopped", "bytes", n, "err", err)
		errc <- fmt.Errorf("tun -> lwIP: %w", orEOF(err))
	}()
	go func() {
		n, err := io.Copy(tun, d.ip)
		log.Debug("lwIP -> tun stopped", "bytes", n, "err", err)
		errc <- fmt.Errorf("lwIP -> tun: %w", orEOF(err))
	}()
	err := <-errc
	// The caller closes tun and the device, which ends the other direction.
	go func() { <-errc }()
	return err
}

// Close shuts down the lwIP stack.
func (d *Device) Close() error {
	var err error
	d.closeOnce.Do(func() { err = d.ip.Close() })
	return err
}

func orEOF(err error) error {
	if err == nil {
		return io.EOF
	}
	return err
}
