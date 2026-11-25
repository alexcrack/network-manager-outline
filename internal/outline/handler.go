package outline

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"sync"
	"time"

	"network-manager-outline/internal/nmvpn"
	"network-manager-outline/internal/tun"
)

const (
	resolveTimeout    = 10 * time.Second
	checkTimeout      = 10 * time.Second
	probeTimeout      = 10 * time.Second
	disconnectTimeout = 10 * time.Second
)

// Tunnel-side addresses. Only this host uses them; NM routes traffic into
// the TUN device, where lwIP terminates it.
var (
	tunAddr4 = netip.MustParsePrefix("10.233.233.1/32")
	tunAddr6 = netip.MustParsePrefix("fdcc:a7f5:9233::1/128")
)

// tunnelDevice is the TUN side; *tun.Device implements it.
type tunnelDevice interface {
	io.ReadWriteCloser
	Name() string
}

// stack is the outline-sdk side; *Device implements it.
type stack interface {
	CheckServer(ctx context.Context) error
	CheckTCP(ctx context.Context, resolver netip.Addr) error
	ProbeUDP(ctx context.Context, resolver netip.Addr) (bool, error)
	Pump(tun io.ReadWriter, log *slog.Logger) error
	Close() error
}

// Handler implements nmvpn.Handler for Outline.
type Handler struct {
	log *slog.Logger

	// Injection points for tests.
	openTUN    func() (tunnelDevice, error)
	newStack   func(ctx context.Context, t Transport) (stack, error)
	resolver   *net.Resolver
	httpClient *http.Client // nil = default

	mu   sync.Mutex
	sess *session
}

var _ nmvpn.Handler = (*Handler)(nil)

func NewHandler(log *slog.Logger) *Handler {
	return &Handler{
		log: log,
		openTUN: func() (tunnelDevice, error) {
			return tun.Open(tun.DefaultName, MTU)
		},
		newStack: func(ctx context.Context, t Transport) (stack, error) {
			return NewDevice(ctx, t)
		},
	}
}

func (h *Handler) NeedSecrets(s nmvpn.ConnectionSettings) (string, error) {
	if s.VPNSecrets()[SecretAccessKey] != "" {
		return "", nil
	}
	return "vpn", nil
}

// Connect validates the settings synchronously and brings the tunnel up in
// the background; see session.run.
func (h *Handler) Connect(ctx context.Context, s nmvpn.ConnectionSettings, e nmvpn.Emitter) error {
	st, err := ParseSettings(s)
	if err != nil {
		return err
	}
	log := h.log.With("connection", s.ID())
	if st.DynamicKey != nil {
		log = log.With("key_server", st.DynamicKey.Host()) // never the path: often a token
	} else {
		log = log.With("server", st.AccessKey.Server())
	}
	sess := &session{h: h, st: st, e: e, done: make(chan struct{}), log: log}

	h.mu.Lock()
	h.sess = sess
	h.mu.Unlock()

	go func() {
		defer close(sess.done)
		sess.run(ctx)
	}()
	return nil
}

// Disconnect waits for the session to tear down. The plugin cancels the
// session context before calling it.
func (h *Handler) Disconnect() error {
	h.mu.Lock()
	sess := h.sess
	h.sess = nil
	h.mu.Unlock()
	if sess == nil {
		return nil
	}
	select {
	case <-sess.done:
		return nil
	case <-time.After(disconnectTimeout):
		return errors.New("timed out waiting for the tunnel to stop")
	}
}

// session is one connection attempt and its lifetime.
type session struct {
	h   *Handler
	st  *Settings
	e   nmvpn.Emitter
	log *slog.Logger

	done chan struct{}
}

// run brings the tunnel up, reports it to NM, and keeps it running until ctx
// is cancelled. Any failure is reported with Emitter.Fail; the plugin then
// cancels ctx, and run releases everything before returning.
func (s *session) run(ctx context.Context) {
	var cleanup []func()
	defer func() {
		for i := len(cleanup) - 1; i >= 0; i-- {
			cleanup[i]()
		}
		s.log.Info("tunnel stopped")
	}()

	failWith := func(reason nmvpn.Failure, step string, err error) {
		if ctx.Err() != nil {
			return // already tearing down
		}
		s.log.Error("tunnel failed", "step", step, "err", s.st.redact(err.Error()))
		s.e.Fail(reason)
		<-ctx.Done()
	}
	fail := func(step string, err error) { failWith(nmvpn.FailureConnectFailed, step, err) }

	// 0. Fetch a dynamic key. Like the official client, on every connect.
	if dk := s.st.DynamicKey; dk != nil {
		key, err := dk.Fetch(ctx, s.h.httpClient)
		if err != nil {
			var fe *FetchError
			if errors.As(err, &fe) && fe.Rejected {
				failWith(nmvpn.FailureLoginFailed, "fetch", err)
			} else {
				fail("fetch", err)
			}
			return
		}
		s.st.AccessKey = key
		s.log = s.log.With("server", key.Server())
		s.log.Info("fetched dynamic key")
	}

	// 1. Resolve the server before NM changes routes and DNS.
	rctx, cancel := context.WithTimeout(ctx, resolveTimeout)
	serverIP, err := s.st.AccessKey.ResolveServer(rctx, s.h.resolver)
	cancel()
	if err != nil {
		fail("resolve", err)
		return
	}
	s.log = s.log.With("server_ip", serverIP)

	// 2. Transport stack (no I/O), then checks so that an unreachable server
	//    or a wrong key fails the connection instead of leaving a dead tunnel:
	//    a plain TCP connect, then a DNS lookup over TCP through the server.
	dev, err := s.h.newStack(ctx, s.st.AccessKey.TransportConfig(serverIP, s.st.TransportPrefix))
	if err != nil {
		fail("transport", err)
		return
	}
	cleanup = append(cleanup, func() { dev.Close() })

	cctx, cancel := context.WithTimeout(ctx, checkTimeout)
	err = dev.CheckServer(cctx)
	cancel()
	if err != nil {
		fail("check", fmt.Errorf("server unreachable: %w", err))
		return
	}

	resolver := s.st.checkResolver()
	cctx, cancel = context.WithTimeout(ctx, checkTimeout)
	err = dev.CheckTCP(cctx, resolver)
	cancel()
	switch {
	case err == nil:
	case IsTimeout(err):
		failWith(nmvpn.FailureLoginFailed, "check",
			fmt.Errorf("server did not answer; the access key is probably wrong: %w", err))
		return
	default:
		fail("check", fmt.Errorf("server closed the connection; it may be unable to reach the internet: %w", err))
		return
	}

	// 3. TUN device and packet pump.
	td, err := s.h.openTUN()
	if err != nil {
		fail("tun", err)
		return
	}
	pumpErr := make(chan error, 1)
	go func() { pumpErr <- dev.Pump(td, s.log) }()
	// Close the TUN before the stack so both pump directions end.
	cleanup = append(cleanup, func() { td.Close() })

	// 4. Report the configuration; the plugin then reports STARTED.
	if err := s.report(td.Name(), serverIP); err != nil {
		fail("config", err)
		return
	}
	s.log.Info("tunnel up", "tundev", td.Name())

	// 5. Probe UDP in the background; DNS works via TCP meanwhile.
	go func() {
		pctx, cancel := context.WithTimeout(ctx, probeTimeout)
		defer cancel()
		ok, err := dev.ProbeUDP(pctx, resolver)
		switch {
		case err != nil:
			s.log.Warn("UDP probe inconclusive, staying in DNS-truncate mode", "err", s.st.redact(err.Error()))
		case ok:
			s.log.Info("server relays UDP")
		default:
			s.log.Warn("server does not relay UDP, using DNS-truncate mode")
		}
	}()

	select {
	case <-ctx.Done():
	case err := <-pumpErr:
		fail("pump", err)
	}
}

func (s *session) report(tundev string, serverIP netip.Addr) error {
	ipv6 := s.st.IPv6 == IPv6Tunnel
	if err := s.e.SetConfig(nmvpn.GenericConfig{
		TunDev:  tundev,
		Gateway: serverIP,
		MTU:     MTU,
		HasIP4:  true,
		HasIP6:  ipv6,
	}); err != nil {
		return err
	}

	// "~." routes all DNS lookups to the tunnel's servers in
	// systemd-resolved, so queries do not leak to other links.
	var domains []string
	if !s.st.NeverDefault {
		domains = []string{"~."}
	}
	if err := s.e.SetIP4Config(nmvpn.IPConfig{
		Address:      tunAddr4,
		DNS:          s.st.DNS4(),
		Domains:      domains,
		NeverDefault: s.st.NeverDefault,
	}); err != nil {
		return err
	}
	if ipv6 {
		if err := s.e.SetIP6Config(nmvpn.IPConfig{
			Address:      tunAddr6,
			DNS:          s.st.DNS6(),
			NeverDefault: s.st.NeverDefault,
		}); err != nil {
			return err
		}
	}
	return nil
}
