package outline

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/netip"
	"sync"

	"golang.getoutline.org/sdk/network/packetrelay"
	"golang.getoutline.org/sdk/transport"
)

// Traffic to the Outline server's own address must never go through the
// tunnel: the service's connections to the server would loop back into it.
// NetworkManager normally prevents that with a host route to the server via
// the physical device. But a persistent VPN (vpn.persistent=yes) stays up
// while the network changes, and between losing one network and getting the
// next, that host route is gone while the tunnel's default route stays. Each
// dial to the server would then enter the tunnel, which dials the server
// again: an endless storm of connections. The guard refuses such traffic, so
// those dials fail at once and work again when the new network's route exists.

var errLoop = errors.New("refusing traffic to the Outline server through the tunnel (routing loop)")

// loopGuard drops tunnel traffic to the server and logs it once per episode.
type loopGuard struct {
	server netip.Addr
	log    *slog.Logger

	mu     sync.Mutex
	warned bool
}

func (g *loopGuard) blocks(ip netip.Addr) bool {
	if ip.Unmap() != g.server {
		g.mu.Lock()
		g.warned = false // traffic flows normally again
		g.mu.Unlock()
		return false
	}
	g.mu.Lock()
	first := !g.warned
	g.warned = true
	g.mu.Unlock()
	if first && g.log != nil {
		g.log.Warn("traffic to the server entered the tunnel (network changing?), refusing it until the route to the server is back")
	}
	return true
}

// guardedStreamDialer refuses TCP connections to the server.
type guardedStreamDialer struct {
	transport.StreamDialer
	guard *loopGuard
}

func (d guardedStreamDialer) DialStream(ctx context.Context, raddr string) (transport.StreamConn, error) {
	if host, _, err := net.SplitHostPort(raddr); err == nil {
		if ip, err := netip.ParseAddr(host); err == nil && d.guard.blocks(ip) {
			return nil, errLoop
		}
	}
	return d.StreamDialer.DialStream(ctx, raddr)
}

// guardedPacketRelay drops UDP packets to the server.
type guardedPacketRelay struct {
	packetrelay.PacketRelay
	guard *loopGuard
}

func (r guardedPacketRelay) NewAssociation() (packetrelay.PacketSender, packetrelay.PacketReceiver, error) {
	s, recv, err := r.PacketRelay.NewAssociation()
	if err != nil {
		return nil, nil, err
	}
	return guardedSender{s, r.guard}, recv, nil
}

type guardedSender struct {
	packetrelay.PacketSender
	guard *loopGuard
}

func (s guardedSender) SendPacket(p []byte, destination netip.AddrPort) error {
	if s.guard.blocks(destination.Addr()) {
		return errLoop
	}
	return s.PacketSender.SendPacket(p, destination)
}
