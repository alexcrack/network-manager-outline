package nmvpn

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/netip"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
)

// fakeBus records emitted signals.
type fakeBus struct {
	mu       sync.Mutex
	signals  []string
	states   []ServiceState
	failures []Failure
}

func (b *fakeBus) Emit(_ dbus.ObjectPath, name string, values ...any) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.signals = append(b.signals, name)
	switch name {
	case PluginInterface + ".StateChanged":
		b.states = append(b.states, ServiceState(values[0].(uint32)))
	case PluginInterface + ".Failure":
		b.failures = append(b.failures, Failure(values[0].(uint32)))
	}
	return nil
}

func (b *fakeBus) snapshot() ([]string, []ServiceState) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return slices.Clone(b.signals), slices.Clone(b.states)
}

// fakeHandler emits the given configs synchronously from Connect.
type fakeHandler struct {
	connectErr  error
	emit        func(Emitter)
	disconnects int
	ctx         context.Context
}

func (h *fakeHandler) NeedSecrets(ConnectionSettings) (string, error) { return "", nil }
func (h *fakeHandler) Connect(ctx context.Context, _ ConnectionSettings, e Emitter) error {
	h.ctx = ctx
	if h.connectErr != nil {
		return h.connectErr
	}
	if h.emit != nil {
		h.emit(e)
	}
	return nil
}
func (h *fakeHandler) Disconnect() error { h.disconnects++; return nil }

var testGeneric = GenericConfig{TunDev: "outline0", Gateway: netip.MustParseAddr("203.0.113.7"), HasIP4: true}
var testIP4 = IPConfig{Address: netip.MustParsePrefix("10.233.233.1/32")}

func newTestPlugin(h Handler, opts Options) (*Plugin, *fakeBus) {
	opts.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	if opts.IdleTimeout == 0 {
		opts.IdleTimeout = -1
	}
	p := New(h, opts)
	b := &fakeBus{}
	p.bus = b
	return p, b
}

func waitState(t *testing.T, p *Plugin, want ServiceState) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for p.State() != want {
		if time.Now().After(deadline) {
			t.Fatalf("state = %s, want %s", p.State(), want)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestConnectDisconnect(t *testing.T) {
	h := &fakeHandler{emit: func(e Emitter) {
		if err := e.SetConfig(testGeneric); err != nil {
			t.Error(err)
		}
		if err := e.SetIP4Config(testIP4); err != nil {
			t.Error(err)
		}
	}}
	p, b := newTestPlugin(h, Options{})

	if err := p.connect(":1.1", nil); err != nil {
		t.Fatal(err)
	}
	if p.State() != StateStarted {
		t.Fatalf("state = %s", p.State())
	}
	if err := p.disconnect(); err != nil {
		t.Fatal(err)
	}
	if h.disconnects != 1 || h.ctx.Err() == nil {
		t.Fatal("handler not disconnected / ctx not cancelled")
	}
	_, states := b.snapshot()
	want := []ServiceState{StateStarting, StateStarted, StateStopping, StateStopped}
	if !slices.Equal(states, want) {
		t.Fatalf("states = %v, want %v", states, want)
	}
}

func TestWaitsForAnnouncedIP6(t *testing.T) {
	g := testGeneric
	g.HasIP6 = true
	var em Emitter
	h := &fakeHandler{emit: func(e Emitter) { em = e; _ = e.SetConfig(g); _ = e.SetIP4Config(testIP4) }}
	p, _ := newTestPlugin(h, Options{})
	if err := p.connect("", nil); err != nil {
		t.Fatal(err)
	}
	if p.State() != StateStarting {
		t.Fatalf("started before Ip6Config: %s", p.State())
	}
	_ = em.SetIP6Config(IPConfig{Address: netip.MustParsePrefix("fd00::1/128")})
	if p.State() != StateStarted {
		t.Fatalf("state = %s", p.State())
	}
}

func TestConnectWrongState(t *testing.T) {
	p, _ := newTestPlugin(&fakeHandler{}, Options{})
	if err := p.connect("", nil); err != nil {
		t.Fatal(err)
	}
	var pe *Error
	if err := p.connect("", nil); !errors.As(err, &pe) || pe.Code != ErrWrongState {
		t.Fatalf("second connect: %v", err)
	}
}

func TestConnectFailureStops(t *testing.T) {
	p, b := newTestPlugin(&fakeHandler{connectErr: errors.New("boom")}, Options{})
	if err := p.connect("", nil); err == nil {
		t.Fatal("expected error")
	}
	waitState(t, p, StateStopped)
	_, states := b.snapshot()
	if !slices.Equal(states, []ServiceState{StateStarting, StateStopped}) {
		t.Fatalf("states = %v", states)
	}
}

func TestDisconnectStates(t *testing.T) {
	p, _ := newTestPlugin(&fakeHandler{}, Options{})
	if err := p.disconnect(); err != nil { // init -> stopped
		t.Fatal(err)
	}
	var pe *Error
	if err := p.disconnect(); !errors.As(err, &pe) || pe.Code != ErrAlreadyStopped {
		t.Fatalf("got %v", err)
	}
}

func TestConnectTimeout(t *testing.T) {
	p, b := newTestPlugin(&fakeHandler{}, Options{ConnectTimeout: 50 * time.Millisecond})
	if err := p.connect("", nil); err != nil {
		t.Fatal(err)
	}
	waitState(t, p, StateStopped)
	signals, _ := b.snapshot()
	if !slices.Contains(signals, PluginInterface+".Failure") {
		t.Fatal("no Failure signal on timeout")
	}
}

func TestFailTearsDown(t *testing.T) {
	var em Emitter
	h := &fakeHandler{emit: func(e Emitter) { em = e; _ = e.SetConfig(testGeneric); _ = e.SetIP4Config(testIP4) }}
	p, b := newTestPlugin(h, Options{})
	_ = p.connect("", nil)
	em.Fail(FailureConnectFailed)
	waitState(t, p, StateStopped)
	if h.disconnects != 1 {
		t.Fatal("handler not disconnected")
	}
	signals, _ := b.snapshot()
	if !slices.Contains(signals, PluginInterface+".Failure") {
		t.Fatal("no Failure signal")
	}
}

func TestPeerVanished(t *testing.T) {
	h := &fakeHandler{emit: func(e Emitter) { _ = e.SetConfig(testGeneric); _ = e.SetIP4Config(testIP4) }}
	p, _ := newTestPlugin(h, Options{})
	_ = p.connect(":1.42", nil)
	p.peerVanished(":1.99") // someone else
	if p.State() != StateStarted {
		t.Fatal("disconnected on unrelated peer")
	}
	p.peerVanished(":1.42")
	waitState(t, p, StateStopped)
}

func TestIdleQuit(t *testing.T) {
	p, _ := newTestPlugin(&fakeHandler{}, Options{IdleTimeout: 30 * time.Millisecond})
	p.mu.Lock()
	p.scheduleQuitLocked()
	p.mu.Unlock()
	select {
	case <-p.quit:
	case <-time.After(time.Second):
		t.Fatal("did not quit when idle")
	}
}

func TestFailReasonKeptDuringStarting(t *testing.T) {
	var em Emitter
	h := &fakeHandler{emit: func(e Emitter) { em = e }}
	p, b := newTestPlugin(h, Options{})
	if err := p.connect("", nil); err != nil {
		t.Fatal(err)
	}
	em.Fail(FailureLoginFailed)
	waitState(t, p, StateStopped)
	b.mu.Lock()
	defer b.mu.Unlock()
	if !slices.Equal(b.failures, []Failure{FailureLoginFailed}) {
		t.Fatalf("failures = %v, want only LOGIN_FAILED (NM keeps the last one)", b.failures)
	}
}

func TestTimeoutStillReportsConnectFailed(t *testing.T) {
	p, b := newTestPlugin(&fakeHandler{}, Options{ConnectTimeout: 30 * time.Millisecond})
	if err := p.connect("", nil); err != nil {
		t.Fatal(err)
	}
	waitState(t, p, StateStopped)
	b.mu.Lock()
	defer b.mu.Unlock()
	if !slices.Equal(b.failures, []Failure{FailureConnectFailed}) {
		t.Fatalf("failures = %v", b.failures)
	}
}
