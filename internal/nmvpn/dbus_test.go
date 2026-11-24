package nmvpn

import (
	"bufio"
	"context"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
)

const testBusName = "org.freedesktop.NetworkManager.test"

// startBus runs a private dbus-daemon and returns its address.
func startBus(t *testing.T) string {
	t.Helper()
	daemon, err := exec.LookPath("dbus-daemon")
	if err != nil {
		t.Skip("dbus-daemon not installed")
	}
	dir := t.TempDir()
	cfg := filepath.Join(dir, "bus.conf")
	conf := `<!DOCTYPE busconfig PUBLIC "-//freedesktop//DTD D-Bus Bus Configuration 1.0//EN"
 "http://www.freedesktop.org/standards/dbus/1.0/busconfig.dtd">
<busconfig>
  <type>session</type>
  <listen>unix:path=` + filepath.Join(dir, "bus") + `</listen>
  <auth>EXTERNAL</auth>
  <policy context="default">
    <allow send_destination="*" eavesdrop="true"/>
    <allow eavesdrop="true"/>
    <allow own="*"/>
  </policy>
</busconfig>`
	if err := os.WriteFile(cfg, []byte(conf), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(daemon, "--config-file="+cfg, "--nofork", "--print-address")
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	addr, err := bufio.NewReader(out).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(addr)
}

func dial(t *testing.T, addr string) *dbus.Conn {
	t.Helper()
	c, err := dbus.Connect(addr)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

type busFixture struct {
	plugin *Plugin
	served chan error
	client *dbus.Conn
	obj    dbus.BusObject
	sigs   chan *dbus.Signal
}

func newBusFixture(t *testing.T, h Handler, opts Options) *busFixture {
	addr := startBus(t)
	opts.BusName = testBusName
	opts.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	if opts.IdleTimeout == 0 {
		opts.IdleTimeout = -1
	}
	f := &busFixture{plugin: New(h, opts), served: make(chan error, 1)}

	srv := dial(t, addr)
	t.Cleanup(func() { srv.Close() })
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { f.served <- f.plugin.Serve(ctx, srv) }()

	f.client = dial(t, addr)
	t.Cleanup(func() { f.client.Close() })
	if err := f.client.AddMatchSignal(dbus.WithMatchInterface(PluginInterface)); err != nil {
		t.Fatal(err)
	}
	f.sigs = make(chan *dbus.Signal, 32)
	f.client.Signal(f.sigs)

	// Wait for the name to be claimed.
	deadline := time.Now().Add(2 * time.Second)
	for {
		var has bool
		_ = f.client.BusObject().Call("org.freedesktop.DBus.NameHasOwner", 0, testBusName).Store(&has)
		if has {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("plugin did not claim bus name")
		}
		time.Sleep(10 * time.Millisecond)
	}
	f.obj = f.client.Object(testBusName, PluginPath)
	return f
}

func (f *busFixture) expectStates(t *testing.T, want ...ServiceState) {
	t.Helper()
	for _, w := range want {
		for {
			select {
			case s := <-f.sigs:
				if s.Name != PluginInterface+".StateChanged" {
					continue
				}
				if got := ServiceState(s.Body[0].(uint32)); got != w {
					t.Fatalf("StateChanged %s, want %s", got, w)
				}
			case <-time.After(2 * time.Second):
				t.Fatalf("timeout waiting for state %s", w)
			}
			break
		}
	}
}

func TestDBusIntrospection(t *testing.T) {
	f := newBusFixture(t, &fakeHandler{}, Options{})
	var xml string
	if err := f.obj.Call("org.freedesktop.DBus.Introspectable.Introspect", 0).Store(&xml); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{PluginInterface, "ConnectInteractive", "NeedSecrets", `name="State"`, "Ip4Config"} {
		if !strings.Contains(xml, want) {
			t.Fatalf("introspection lacks %q", want)
		}
	}
}

func TestDBusConnectFlow(t *testing.T) {
	h := &fakeHandler{emit: func(e Emitter) {
		go func() { _ = e.SetConfig(testGeneric); _ = e.SetIP4Config(testIP4) }()
	}}
	f := newBusFixture(t, h, Options{})

	settings := map[string]map[string]dbus.Variant{
		"vpn": {"secrets": dbus.MakeVariant(map[string]string{"access-key": "ss://x"})},
	}
	var setting string
	if err := f.obj.Call(PluginInterface+".NeedSecrets", 0, settings).Store(&setting); err != nil {
		t.Fatal(err)
	}
	if call := f.obj.Call(PluginInterface+".ConnectInteractive", 0, settings, map[string]dbus.Variant{}); call.Err == nil ||
		call.Err.(dbus.Error).Name != ErrorPrefix+".InteractiveNotSupported" {
		t.Fatalf("ConnectInteractive: %v", call.Err)
	}
	if err := f.obj.Call(PluginInterface+".Connect", 0, settings).Err; err != nil {
		t.Fatal(err)
	}
	f.expectStates(t, StateStarting, StateStarted)

	v, err := f.obj.GetProperty(PluginInterface + ".State")
	if err != nil || ServiceState(v.Value().(uint32)) != StateStarted {
		t.Fatalf("State property = %v, %v", v, err)
	}

	err = f.obj.Call(PluginInterface+".Connect", 0, settings).Err
	if de, ok := err.(dbus.Error); !ok || de.Name != ErrorPrefix+".WrongState" {
		t.Fatalf("second Connect: %v", err)
	}

	if err := f.obj.Call(PluginInterface+".Disconnect", 0).Err; err != nil {
		t.Fatal(err)
	}
	f.expectStates(t, StateStopping, StateStopped)
}

func TestDBusWatchPeerQuits(t *testing.T) {
	h := &fakeHandler{emit: func(e Emitter) { _ = e.SetConfig(testGeneric); _ = e.SetIP4Config(testIP4) }}
	f := newBusFixture(t, h, Options{WatchPeer: true})
	if err := f.obj.Call(PluginInterface+".Connect", 0, map[string]map[string]dbus.Variant{}).Err; err != nil {
		t.Fatal(err)
	}
	f.expectStates(t, StateStarting, StateStarted)

	f.client.Close() // NetworkManager "crashes"
	select {
	case err := <-f.served:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("plugin did not quit after peer vanished")
	}
	if f.plugin.State() != StateStopped || h.disconnects != 1 {
		t.Fatalf("state = %s, disconnects = %d", f.plugin.State(), h.disconnects)
	}
}
