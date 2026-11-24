package nmvpn

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"
	"github.com/godbus/dbus/v5/prop"
)

// ConnectionSettings is the a{sa{sv}} dictionary NM passes to Connect/NeedSecrets.
type ConnectionSettings map[string]map[string]dbus.Variant

// Handler is implemented by the concrete VPN backend (see internal/outline).
type Handler interface {
	// NeedSecrets returns the setting name ("vpn") if secrets are missing, or "".
	NeedSecrets(s ConnectionSettings) (string, error)
	// Connect brings the tunnel up and reports configuration through the Emitter.
	// It must return promptly; long-running work belongs in goroutines.
	Connect(ctx context.Context, s ConnectionSettings, e Emitter) error
	// Disconnect tears the tunnel down.
	Disconnect() error
}

// Emitter lets the Handler report configuration and failures back to NM.
type Emitter interface {
	Config(map[string]dbus.Variant) error
	IP4Config(map[string]dbus.Variant) error
	IP6Config(map[string]dbus.Variant) error
	Failure(Failure)
	SetState(ServiceState)
}

// Plugin exports a Handler on the system bus under a VPN service name.
type Plugin struct {
	conn    *dbus.Conn
	name    string
	handler Handler
	log     *slog.Logger

	mu    sync.Mutex
	props *prop.Properties
	state ServiceState
	quit  chan struct{}
}

func New(busName string, h Handler, log *slog.Logger) *Plugin {
	return &Plugin{name: busName, handler: h, log: log, state: StateInit, quit: make(chan struct{})}
}

// Run connects to the system bus, exports the object, claims the bus name and
// blocks until the plugin is told to quit.
func (p *Plugin) Run() error {
	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		return fmt.Errorf("connect system bus: %w", err)
	}
	defer conn.Close()
	p.conn = conn

	if err := conn.Export(p, PluginPath, PluginInterface); err != nil {
		return err
	}
	p.props, err = prop.Export(conn, PluginPath, prop.Map{
		PluginInterface: {"State": {Value: uint32(p.state), Emit: prop.EmitTrue}},
	})
	if err != nil {
		return err
	}
	// TODO(phase1): full introspection XML (methods, signals, property).
	if err := conn.Export(introspect.Introspectable(introspect.IntrospectDeclarationString), PluginPath,
		"org.freedesktop.DBus.Introspectable"); err != nil {
		return err
	}

	reply, err := conn.RequestName(p.name, dbus.NameFlagDoNotQueue)
	if err != nil {
		return fmt.Errorf("request name %s: %w", p.name, err)
	}
	if reply != dbus.RequestNameReplyPrimaryOwner {
		return errors.New("bus name already taken: " + p.name)
	}
	p.log.Info("plugin ready", "name", p.name)

	// TODO(phase1): quit after an idle timeout when no connection is active,
	// like libnm's NMVpnServicePlugin does.
	<-p.quit
	return nil
}

func (p *Plugin) Stop() { close(p.quit) }

// ---- D-Bus methods (org.freedesktop.NetworkManager.VPN.Plugin) ----

func (p *Plugin) Connect(s ConnectionSettings) *dbus.Error {
	p.SetState(StateStarting)
	if err := p.handler.Connect(context.Background(), s, p); err != nil {
		p.log.Error("connect failed", "err", err)
		p.Failure(FailureConnectFailed)
		p.SetState(StateStopped)
		return dbus.MakeFailedError(err)
	}
	return nil
}

func (p *Plugin) ConnectInteractive(s ConnectionSettings, details map[string]dbus.Variant) *dbus.Error {
	// TODO(phase2): support interactive secret requests (SecretsRequired signal).
	return p.Connect(s)
}

func (p *Plugin) NeedSecrets(s ConnectionSettings) (string, *dbus.Error) {
	name, err := p.handler.NeedSecrets(s)
	if err != nil {
		return "", dbus.MakeFailedError(err)
	}
	return name, nil
}

func (p *Plugin) NewSecrets(s ConnectionSettings) *dbus.Error {
	// TODO(phase2): continue a pending interactive connect.
	return nil
}

func (p *Plugin) Disconnect() *dbus.Error {
	p.SetState(StateStopping)
	err := p.handler.Disconnect()
	p.SetState(StateStopped)
	if err != nil {
		return dbus.MakeFailedError(err)
	}
	return nil
}

// SetConfig/SetIp4Config/SetIp6Config/SetFailure are part of the interface for
// helper scripts; this plugin reports config itself, so they only forward.
func (p *Plugin) SetConfig(c map[string]dbus.Variant) *dbus.Error { return toDBusErr(p.Config(c)) }
func (p *Plugin) SetIp4Config(c map[string]dbus.Variant) *dbus.Error {
	return toDBusErr(p.IP4Config(c))
}
func (p *Plugin) SetIp6Config(c map[string]dbus.Variant) *dbus.Error {
	return toDBusErr(p.IP6Config(c))
}
func (p *Plugin) SetFailure(reason string) *dbus.Error {
	p.log.Error("helper reported failure", "reason", reason)
	p.Failure(FailureConnectFailed)
	return nil
}

// ---- Emitter ----

func (p *Plugin) Config(c map[string]dbus.Variant) error {
	return p.emit("Config", c)
}

func (p *Plugin) IP4Config(c map[string]dbus.Variant) error {
	return p.emit("Ip4Config", c)
}

func (p *Plugin) IP6Config(c map[string]dbus.Variant) error {
	return p.emit("Ip6Config", c)
}

func (p *Plugin) Failure(f Failure) {
	_ = p.emit("Failure", uint32(f))
}

func (p *Plugin) SetState(s ServiceState) {
	p.mu.Lock()
	p.state = s
	p.mu.Unlock()
	if p.props != nil {
		p.props.SetMust(PluginInterface, "State", uint32(s))
	}
	_ = p.emit("StateChanged", uint32(s))
}

func (p *Plugin) emit(signal string, args ...any) error {
	if p.conn == nil {
		return errors.New("not connected")
	}
	return p.conn.Emit(PluginPath, PluginInterface+"."+signal, args...)
}

func toDBusErr(err error) *dbus.Error {
	if err == nil {
		return nil
	}
	return dbus.MakeFailedError(err)
}
