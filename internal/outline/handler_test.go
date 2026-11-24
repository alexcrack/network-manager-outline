package outline

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/netip"
	"slices"
	"sync"
	"testing"
	"time"

	"network-manager-outline/internal/nmvpn"
)

// fakeTUN blocks reads until closed.
type fakeTUN struct {
	once   sync.Once
	closed chan struct{}
}

func newFakeTUN() *fakeTUN                     { return &fakeTUN{closed: make(chan struct{})} }
func (f *fakeTUN) Name() string                { return "outline7" }
func (f *fakeTUN) Write(b []byte) (int, error) { return len(b), nil }
func (f *fakeTUN) Read(b []byte) (int, error)  { <-f.closed; return 0, io.EOF }
func (f *fakeTUN) Close() error                { f.once.Do(func() { close(f.closed) }); return nil }
func (f *fakeTUN) isClosed() bool {
	select {
	case <-f.closed:
		return true
	default:
		return false
	}
}

type fakeStack struct {
	serverErr error
	checkErr  error
	pumpErr   chan error // send to make Pump fail
	transport Transport
	mu        sync.Mutex
	probed    bool
	closed    bool
}

func (s *fakeStack) CheckServer(context.Context) error          { return s.serverErr }
func (s *fakeStack) CheckTCP(context.Context, netip.Addr) error { return s.checkErr }
func (s *fakeStack) ProbeUDP(context.Context, netip.Addr) (bool, error) {
	s.mu.Lock()
	s.probed = true
	s.mu.Unlock()
	return true, nil
}
func (s *fakeStack) Pump(tun io.ReadWriter, _ *slog.Logger) error {
	done := make(chan error, 1)
	go func() { _, err := tun.Read(make([]byte, 1)); done <- err }()
	select {
	case err := <-done:
		return err
	case err := <-s.pumpErr:
		return err
	}
}
func (s *fakeStack) Close() error { s.mu.Lock(); s.closed = true; s.mu.Unlock(); return nil }

// fakeEmitter records reports; Fail cancels the session like the plugin does.
type fakeEmitter struct {
	cancel  context.CancelFunc
	mu      sync.Mutex
	generic *nmvpn.GenericConfig
	ip4     *nmvpn.IPConfig
	ip6     *nmvpn.IPConfig
	failed  bool
	reason  nmvpn.Failure
	events  chan string
}

func newFakeEmitter(cancel context.CancelFunc) *fakeEmitter {
	return &fakeEmitter{cancel: cancel, events: make(chan string, 8)}
}
func (e *fakeEmitter) SetConfig(c nmvpn.GenericConfig) error {
	e.mu.Lock()
	e.generic = &c
	e.mu.Unlock()
	e.events <- "config"
	return nil
}
func (e *fakeEmitter) SetIP4Config(c nmvpn.IPConfig) error {
	e.mu.Lock()
	e.ip4 = &c
	e.mu.Unlock()
	e.events <- "ip4"
	return nil
}
func (e *fakeEmitter) SetIP6Config(c nmvpn.IPConfig) error {
	e.mu.Lock()
	e.ip6 = &c
	e.mu.Unlock()
	e.events <- "ip6"
	return nil
}
func (e *fakeEmitter) Fail(r nmvpn.Failure) {
	e.mu.Lock()
	e.failed = true
	e.reason = r
	e.mu.Unlock()
	e.events <- "fail"
	e.cancel()
}

func (e *fakeEmitter) next(t *testing.T) string {
	t.Helper()
	select {
	case ev := <-e.events:
		return ev
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for an emitter event")
		return ""
	}
}

type fixture struct {
	h     *Handler
	tun   *fakeTUN
	stack *fakeStack
	em    *fakeEmitter
	ctx   context.Context
	stop  context.CancelFunc
}

func newFixture(st *fakeStack) *fixture {
	f := &fixture{tun: newFakeTUN(), stack: st}
	if f.stack.pumpErr == nil {
		f.stack.pumpErr = make(chan error, 1)
	}
	f.h = NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil)))
	f.h.openTUN = func() (tunnelDevice, error) { return f.tun, nil }
	f.h.newStack = func(_ context.Context, tr Transport) (stack, error) {
		f.stack.transport = tr
		return f.stack, nil
	}
	f.ctx, f.stop = context.WithCancel(context.Background())
	f.em = newFakeEmitter(f.stop)
	return f
}

// disconnect mimics the plugin: cancel, then Handler.Disconnect.
func (f *fixture) disconnect(t *testing.T) {
	t.Helper()
	f.stop()
	if err := f.h.Disconnect(); err != nil {
		t.Fatal(err)
	}
}

func TestHandlerConnectReportsConfig(t *testing.T) {
	f := newFixture(&fakeStack{})
	s := settings(map[string]string{KeyDNS: "9.9.9.9,2620:fe::fe", KeyTransportPrefix: "split:2"},
		map[string]string{SecretAccessKey: testKey})
	if err := f.h.Connect(f.ctx, s, f.em); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"config", "ip4", "ip6"} {
		if got := f.em.next(t); got != want {
			t.Fatalf("event %q, want %q", got, want)
		}
	}

	g := f.em.generic
	if g.TunDev != "outline7" || g.Gateway != netip.MustParseAddr("203.0.113.7") || g.MTU != MTU || !g.HasIP4 || !g.HasIP6 {
		t.Fatalf("generic = %+v", *g)
	}
	if _, err := g.Dict(); err != nil {
		t.Fatal(err)
	}
	ip4 := f.em.ip4
	if ip4.Address != tunAddr4 || !slices.Equal(ip4.DNS, []netip.Addr{netip.MustParseAddr("9.9.9.9")}) ||
		!slices.Equal(ip4.Domains, []string{"~."}) || ip4.NeverDefault {
		t.Fatalf("ip4 = %+v", *ip4)
	}
	if _, err := ip4.IP4Dict(); err != nil {
		t.Fatal(err)
	}
	ip6 := f.em.ip6
	if ip6.Address != tunAddr6 || !slices.Equal(ip6.DNS, []netip.Addr{netip.MustParseAddr("2620:fe::fe")}) {
		t.Fatalf("ip6 = %+v", *ip6)
	}
	if _, err := ip6.IP6Dict(); err != nil {
		t.Fatal(err)
	}
	if f.stack.transport.Stream[:8] != "split:2|" || f.stack.transport.Packet[:5] != "ss://" {
		t.Fatalf("transport = %+v", f.stack.transport)
	}

	f.disconnect(t)
	if !f.tun.isClosed() || !f.stack.closed || f.em.failed {
		t.Fatalf("after disconnect: tun closed=%v stack closed=%v failed=%v", f.tun.isClosed(), f.stack.closed, f.em.failed)
	}
}

func TestHandlerIPv6OffNeverDefault(t *testing.T) {
	f := newFixture(&fakeStack{})
	s := settings(map[string]string{KeyIPv6: "off", KeyNeverDefault: "yes"}, map[string]string{SecretAccessKey: testKey})
	if err := f.h.Connect(f.ctx, s, f.em); err != nil {
		t.Fatal(err)
	}
	f.em.next(t)
	f.em.next(t)
	if f.em.generic.HasIP6 || !f.em.ip4.NeverDefault || f.em.ip4.Domains != nil {
		t.Fatalf("generic=%+v ip4=%+v", *f.em.generic, *f.em.ip4)
	}
	f.disconnect(t)
	if f.em.ip6 != nil {
		t.Fatal("Ip6Config sent with ipv6=off")
	}
}

func TestHandlerCheckFailures(t *testing.T) {
	tests := []struct {
		name  string
		stack *fakeStack
		want  nmvpn.Failure
	}{
		{"server unreachable", &fakeStack{serverErr: errors.New("connection refused")}, nmvpn.FailureConnectFailed},
		{"wrong key (timeout)", &fakeStack{checkErr: fmt.Errorf("read salt: %w", context.DeadlineExceeded)}, nmvpn.FailureLoginFailed},
		{"server closed", &fakeStack{checkErr: io.EOF}, nmvpn.FailureConnectFailed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(tt.stack)
			opened := false
			f.h.openTUN = func() (tunnelDevice, error) { opened = true; return f.tun, nil }
			if err := f.h.Connect(f.ctx, settings(nil, map[string]string{SecretAccessKey: testKey}), f.em); err != nil {
				t.Fatal(err)
			}
			if ev := f.em.next(t); ev != "fail" {
				t.Fatalf("event %q, want fail", ev)
			}
			f.disconnect(t)
			if f.em.reason != tt.want {
				t.Fatalf("reason = %d, want %d", f.em.reason, tt.want)
			}
			if opened || !f.stack.closed || f.em.generic != nil {
				t.Fatalf("opened=%v stack closed=%v config sent=%v", opened, f.stack.closed, f.em.generic != nil)
			}
		})
	}
}

func TestHandlerPumpFailure(t *testing.T) {
	f := newFixture(&fakeStack{})
	if err := f.h.Connect(f.ctx, settings(nil, map[string]string{SecretAccessKey: testKey}), f.em); err != nil {
		t.Fatal(err)
	}
	f.em.next(t) // config
	f.em.next(t) // ip4
	f.em.next(t) // ip6
	f.stack.pumpErr <- errors.New("tun read: input/output error")
	if ev := f.em.next(t); ev != "fail" {
		t.Fatalf("event %q, want fail", ev)
	}
	f.disconnect(t)
	if !f.tun.isClosed() || !f.stack.closed {
		t.Fatal("resources not released")
	}
}

func TestHandlerDisconnectDuringSetup(t *testing.T) {
	f := newFixture(&fakeStack{})
	block := make(chan struct{})
	f.h.newStack = func(ctx context.Context, tr Transport) (stack, error) {
		close(block)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if err := f.h.Connect(f.ctx, settings(nil, map[string]string{SecretAccessKey: testKey}), f.em); err != nil {
		t.Fatal(err)
	}
	<-block
	f.disconnect(t)
	if f.em.failed {
		t.Fatal("Fail reported for a user disconnect")
	}
}

func TestHandlerBadSettings(t *testing.T) {
	f := newFixture(&fakeStack{})
	err := f.h.Connect(f.ctx, settings(nil, map[string]string{SecretAccessKey: "ss://nope"}), f.em)
	var pe *nmvpn.Error
	if !errors.As(err, &pe) || pe.Code != nmvpn.ErrBadArguments {
		t.Fatalf("err = %v", err)
	}
	if err := f.h.Disconnect(); err != nil {
		t.Fatal(err)
	}
}
