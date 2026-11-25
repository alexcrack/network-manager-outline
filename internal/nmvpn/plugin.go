package nmvpn

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"
	"github.com/godbus/dbus/v5/prop"
)

// Handler is implemented by the concrete VPN backend (see internal/outline).
//
// The Plugin serialises Connect/Disconnect: Connect is only called in the
// init/stopped state and Disconnect only after a successful Connect.
type Handler interface {
	// NeedSecrets returns the setting name ("vpn") if secrets are missing, or "".
	NeedSecrets(s ConnectionSettings) (string, error)
	// Connect starts bringing the tunnel up and must return promptly.
	// Long-running work belongs in goroutines that report through the Emitter:
	// SetConfig, then SetIP4Config and/or SetIP6Config (as announced by
	// HasIP4/HasIP6), after which the plugin reports STARTED. ctx is cancelled
	// when the connection is torn down. Return Errorf(ErrBadArguments, ...)
	// for invalid settings.
	Connect(ctx context.Context, s ConnectionSettings, e Emitter) error
	// Disconnect tears the tunnel down. It is called without plugin locks held.
	Disconnect() error
}

// Emitter lets a Handler report configuration and failures back to NM.
type Emitter interface {
	SetConfig(GenericConfig) error
	SetIP4Config(IPConfig) error
	SetIP6Config(IPConfig) error
	// Fail reports a failure and tears the connection down asynchronously.
	Fail(Failure)
}

// Options configure a Plugin. Zero values select libnm's defaults.
type Options struct {
	// BusName is the D-Bus service name, e.g. org.freedesktop.NetworkManager.outline.
	BusName string
	// ConnectTimeout bounds STARTING -> STARTED (libnm: 60s). <0 disables.
	ConnectTimeout time.Duration
	// IdleTimeout quits the service after this long in init/stopped state
	// (libnm: 180s). <0 disables.
	IdleTimeout time.Duration
	// WatchPeer disconnects when the caller of Connect (NetworkManager) leaves
	// the bus, and quits as soon as the connection stops (libnm "watch-peer").
	WatchPeer bool
	Logger    *slog.Logger
}

const (
	defaultConnectTimeout = 60 * time.Second
	defaultIdleTimeout    = 180 * time.Second
	// failStopDelay lets a failed Connect's error reply reach NM before the
	// STOPPED signal, like libnm's idle-scheduled fail_stop.
	failStopDelay = 100 * time.Millisecond
	// quitDelay lets pending replies flush before quitting.
	quitDelay = 100 * time.Millisecond
)

// signalBus is the part of *dbus.Conn the plugin uses to emit signals.
type signalBus interface {
	Emit(path dbus.ObjectPath, name string, values ...any) error
}

// Plugin exports a Handler as an NM VPN service plugin.
type Plugin struct {
	h    Handler
	opts Options
	log  *slog.Logger

	mu        sync.Mutex
	bus       signalBus
	props     *prop.Properties
	state     ServiceState
	gotConfig bool
	hasIP4    bool
	hasIP6    bool
	gotIP4    bool
	gotIP6    bool
	cancel    context.CancelFunc
	failed    bool // a Failure was emitted for the current connection
	peer      string
	connTimer *time.Timer
	quitTimer *time.Timer

	quit     chan struct{}
	quitOnce sync.Once
}

var _ Emitter = (*Plugin)(nil)

func New(h Handler, opts Options) *Plugin {
	if opts.ConnectTimeout == 0 {
		opts.ConnectTimeout = defaultConnectTimeout
	}
	if opts.IdleTimeout == 0 {
		opts.IdleTimeout = defaultIdleTimeout
	}
	log := opts.Logger
	if log == nil {
		log = slog.Default()
	}
	return &Plugin{h: h, opts: opts, log: log, state: StateInit, quit: make(chan struct{})}
}

// State returns the current service state.
func (p *Plugin) State() ServiceState {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.state
}

// Run connects to the system bus and serves until ctx is cancelled or the
// plugin quits (idle timeout, or connection stopped with WatchPeer).
func (p *Plugin) Run(ctx context.Context) error {
	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		return fmt.Errorf("connect system bus: %w", err)
	}
	defer conn.Close()
	return p.Serve(ctx, conn)
}

// Serve exports the plugin on conn, claims Options.BusName and blocks until
// ctx is cancelled or the plugin quits. An active connection is disconnected
// before returning.
func (p *Plugin) Serve(ctx context.Context, conn *dbus.Conn) error {
	if p.opts.BusName == "" {
		return errors.New("nmvpn: BusName is required")
	}
	if err := conn.Export(&dbusIface{p}, PluginPath, PluginInterface); err != nil {
		return err
	}
	props, err := prop.Export(conn, PluginPath, prop.Map{
		PluginInterface: {"State": {Value: uint32(p.State()), Emit: prop.EmitTrue}},
	})
	if err != nil {
		return err
	}
	if err := conn.Export(introspect.Introspectable(introspectXML), PluginPath,
		"org.freedesktop.DBus.Introspectable"); err != nil {
		return err
	}

	p.mu.Lock()
	p.bus, p.props = conn, props
	p.mu.Unlock()

	if p.opts.WatchPeer {
		stop, err := p.watchPeers(conn)
		if err != nil {
			return err
		}
		defer stop()
	}

	reply, err := conn.RequestName(p.opts.BusName, dbus.NameFlagDoNotQueue)
	if err != nil {
		return fmt.Errorf("request name %s: %w", p.opts.BusName, err)
	}
	if reply != dbus.RequestNameReplyPrimaryOwner {
		return fmt.Errorf("bus name already taken: %s", p.opts.BusName)
	}
	defer func() { _, _ = conn.ReleaseName(p.opts.BusName) }()
	p.log.Info("plugin ready", "name", p.opts.BusName)

	p.mu.Lock()
	p.scheduleQuitLocked()
	p.mu.Unlock()

	select {
	case <-ctx.Done():
		p.log.Info("shutting down")
	case <-p.quit:
	}
	p.shutdown()
	return nil
}

// Quit makes Serve return.
func (p *Plugin) Quit() { p.quitOnce.Do(func() { close(p.quit) }) }

func (p *Plugin) shutdown() {
	p.mu.Lock()
	active := p.state == StateStarting || p.state == StateStarted
	stopTimer(&p.connTimer)
	stopTimer(&p.quitTimer)
	p.mu.Unlock()
	if active {
		if err := p.disconnect(); err != nil {
			p.log.Warn("disconnect on shutdown", "err", err)
		}
	}
}

// ---- connection lifecycle ----

func (p *Plugin) connect(sender string, s ConnectionSettings) error {
	p.mu.Lock()
	if p.state != StateInit && p.state != StateStopped {
		st := p.state
		p.mu.Unlock()
		return Errorf(ErrWrongState, "could not start connection: wrong plugin state %s", st)
	}
	ctx, cancel := context.WithCancel(context.Background())
	p.cancel = cancel
	p.peer = sender
	p.gotConfig, p.hasIP4, p.hasIP6, p.gotIP4, p.gotIP6 = false, false, false, false, false
	p.failed = false
	// Enter STARTING before calling the handler so config it emits from its
	// goroutines is never seen in the stopped state.
	p.setStateLocked(StateStarting)
	p.mu.Unlock()

	p.log.Info("connect", "id", s.ID(), "peer", sender)
	if err := p.h.Connect(ctx, s, p); err != nil {
		cancel()
		p.log.Error("connect failed", "err", err)
		time.AfterFunc(failStopDelay, func() {
			p.mu.Lock()
			defer p.mu.Unlock()
			if p.state == StateStarting {
				p.cancel = nil
				p.setStateLocked(StateStopped)
			}
		})
		return err
	}

	p.mu.Lock()
	if p.state == StateStarting && p.opts.ConnectTimeout > 0 {
		stopTimer(&p.connTimer)
		p.connTimer = time.AfterFunc(p.opts.ConnectTimeout, func() {
			p.log.Warn("connect timer expired, disconnecting")
			if err := p.disconnect(); err != nil {
				p.log.Warn("disconnect after timeout", "err", err)
			}
		})
	}
	p.mu.Unlock()
	return nil
}

// disconnect follows nm_vpn_service_plugin_disconnect.
func (p *Plugin) disconnect() error {
	p.mu.Lock()
	switch p.state {
	case StateStopping:
		p.mu.Unlock()
		return Errorf(ErrStoppingInProgress, "the VPN connection is already being stopped")
	case StateStopped:
		p.mu.Unlock()
		return Errorf(ErrAlreadyStopped, "no VPN connection was active")
	case StateInit:
		p.setStateLocked(StateStopped)
		p.mu.Unlock()
		return nil
	case StateStarting:
		// Unlike libnm, keep a reason the handler already reported:
		// NM keeps only the last one, and LOGIN_FAILED is more useful.
		if !p.failed {
			p.signalLocked("Failure", uint32(FailureConnectFailed))
		}
	}
	p.setStateLocked(StateStopping)
	cancel := p.cancel
	p.cancel = nil
	p.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	err := p.h.Disconnect()

	p.mu.Lock()
	p.setStateLocked(StateStopped)
	p.mu.Unlock()
	return err
}

// ---- Emitter ----

func (p *Plugin) SetConfig(c GenericConfig) error {
	d, err := c.Dict()
	if err != nil {
		return err
	}
	return p.config(d)
}

func (p *Plugin) SetIP4Config(c IPConfig) error {
	d, err := c.IP4Dict()
	if err != nil {
		return err
	}
	return p.ipConfig(true, d)
}

func (p *Plugin) SetIP6Config(c IPConfig) error {
	d, err := c.IP6Dict()
	if err != nil {
		return err
	}
	return p.ipConfig(false, d)
}

func (p *Plugin) Fail(f Failure) {
	p.mu.Lock()
	p.failed = true
	p.signalLocked("Failure", uint32(f))
	p.mu.Unlock()
	go func() {
		if err := p.disconnect(); err != nil {
			p.log.Debug("disconnect after failure", "err", err)
		}
	}()
}

func (p *Plugin) config(d map[string]dbus.Variant) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.gotConfig = true
	p.hasIP4 = lookupBool(d, ConfigHasIP4)
	p.hasIP6 = lookupBool(d, ConfigHasIP6)
	if err := p.emitLocked("Config", d); err != nil {
		return err
	}
	p.checkCompleteLocked()
	return nil
}

func (p *Plugin) ipConfig(v4 bool, d map[string]dbus.Variant) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	signal := "Ip6Config"
	if v4 {
		signal = "Ip4Config"
		p.gotIP4 = true
		if !p.gotConfig { // libnm compat: old plugins send only Ip4Config
			p.hasIP4 = true
		}
	} else {
		p.gotIP6 = true
	}
	if err := p.emitLocked(signal, d); err != nil {
		return err
	}
	p.checkCompleteLocked()
	return nil
}

func (p *Plugin) checkCompleteLocked() {
	if p.state == StateStarting && p.gotConfig && p.hasIP4 == p.gotIP4 && p.hasIP6 == p.gotIP6 {
		p.setStateLocked(StateStarted)
	}
}

// ---- state & timers (p.mu held) ----

func (p *Plugin) setStateLocked(s ServiceState) {
	if p.state == s {
		return
	}
	p.log.Debug("state", "from", p.state, "to", s)
	p.state = s
	if p.props != nil {
		p.props.SetMust(PluginInterface, "State", uint32(s))
	}
	p.signalLocked("StateChanged", uint32(s))

	switch s {
	case StateStarting:
		stopTimer(&p.quitTimer)
	case StateStopped:
		stopTimer(&p.connTimer)
		p.peer = ""
		if p.opts.WatchPeer {
			time.AfterFunc(quitDelay, p.Quit)
		} else {
			p.scheduleQuitLocked()
		}
	default:
		stopTimer(&p.connTimer)
		stopTimer(&p.quitTimer)
	}
}

func (p *Plugin) scheduleQuitLocked() {
	stopTimer(&p.quitTimer)
	if p.opts.IdleTimeout > 0 {
		p.quitTimer = time.AfterFunc(p.opts.IdleTimeout, func() {
			p.log.Info("idle timeout, quitting")
			p.Quit()
		})
	}
}

// signalLocked emits a signal whose failure the caller cannot act on; it is logged.
func (p *Plugin) signalLocked(name string, args ...any) {
	if err := p.emitLocked(name, args...); err != nil {
		p.log.Warn("emit signal", "signal", name, "err", err)
	}
}

func (p *Plugin) emitLocked(name string, args ...any) error {
	if p.bus == nil {
		return errors.New("nmvpn: not connected to a bus")
	}
	return p.bus.Emit(PluginPath, PluginInterface+"."+name, args...)
}

func stopTimer(t **time.Timer) {
	if *t != nil {
		(*t).Stop()
		*t = nil
	}
}

func lookupBool(d map[string]dbus.Variant, key string) bool {
	v, _ := d[key].Value().(bool)
	return v
}

// ---- peer watching ----

func (p *Plugin) watchPeers(conn *dbus.Conn) (stop func(), err error) {
	opts := []dbus.MatchOption{
		dbus.WithMatchSender("org.freedesktop.DBus"),
		dbus.WithMatchInterface("org.freedesktop.DBus"),
		dbus.WithMatchMember("NameOwnerChanged"),
	}
	if err := conn.AddMatchSignal(opts...); err != nil {
		return nil, fmt.Errorf("watch peers: %w", err)
	}
	ch := make(chan *dbus.Signal, 16)
	conn.Signal(ch)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for sig := range ch {
			if sig.Name != "org.freedesktop.DBus.NameOwnerChanged" || len(sig.Body) != 3 {
				continue
			}
			name, _ := sig.Body[0].(string)
			newOwner, _ := sig.Body[2].(string)
			if newOwner == "" {
				p.peerVanished(name)
			}
		}
	}()
	return func() {
		conn.RemoveSignal(ch)
		_ = conn.RemoveMatchSignal(opts...)
		close(ch)
		<-done
	}, nil
}

func (p *Plugin) peerVanished(name string) {
	p.mu.Lock()
	match := p.peer != "" && p.peer == name
	p.mu.Unlock()
	if match {
		p.log.Warn("peer vanished, disconnecting", "peer", name)
		go func() {
			if err := p.disconnect(); err != nil {
				p.log.Debug("disconnect after peer vanished", "err", err)
			}
		}()
	}
}

// ---- D-Bus method table ----

// dbusIface holds the exported D-Bus methods, kept separate from Plugin so
// that Emitter methods (SetConfig, ...) are not exported with Go signatures.
type dbusIface struct{ p *Plugin }

func (d *dbusIface) Connect(sender dbus.Sender, s ConnectionSettings) *dbus.Error {
	return toDBusError(d.p.connect(string(sender), s))
}

// ConnectInteractive is not supported; NM then falls back to Connect.
func (d *dbusIface) ConnectInteractive(s ConnectionSettings, details map[string]dbus.Variant) *dbus.Error {
	return newDBusError(ErrInteractiveNotSupported, "plugin does not implement ConnectInteractive()")
}

func (d *dbusIface) NeedSecrets(s ConnectionSettings) (string, *dbus.Error) {
	name, err := d.p.h.NeedSecrets(s)
	if err != nil {
		return "", toDBusError(err)
	}
	if name != "" {
		// Keep the service alive while the user is asked for secrets.
		d.p.mu.Lock()
		if d.p.state == StateInit || d.p.state == StateStopped {
			d.p.scheduleQuitLocked()
		}
		d.p.mu.Unlock()
	}
	return name, nil
}

func (d *dbusIface) NewSecrets(s ConnectionSettings) *dbus.Error {
	if st := d.p.State(); st != StateStarting {
		return newDBusError(ErrWrongState, "could not accept new secrets: wrong plugin state %s", st)
	}
	return newDBusError(ErrInteractiveNotSupported, "plugin cannot process interactive secrets")
}

func (d *dbusIface) Disconnect() *dbus.Error {
	return toDBusError(d.p.disconnect())
}

// SetConfig, SetIp4Config, SetIp6Config and SetFailure are meant for helper
// processes of script-based plugins; they behave like the Emitter methods.
func (d *dbusIface) SetConfig(c map[string]dbus.Variant) *dbus.Error {
	return toDBusError(d.p.config(c))
}

func (d *dbusIface) SetIp4Config(c map[string]dbus.Variant) *dbus.Error {
	return toDBusError(d.p.ipConfig(true, c))
}

func (d *dbusIface) SetIp6Config(c map[string]dbus.Variant) *dbus.Error {
	return toDBusError(d.p.ipConfig(false, c))
}

func (d *dbusIface) SetFailure(reason string) *dbus.Error {
	d.p.log.Error("helper reported failure", "reason", reason)
	d.p.Fail(FailureBadIPConfig)
	return nil
}
