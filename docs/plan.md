# Plan

## Goal

Connect to Outline servers from the system network settings (KDE Plasma
first) using an Outline access key, without the Outline client. The tunnel is
implemented with [outline-sdk](https://github.com/OutlineFoundation/outline-sdk)
and managed by NetworkManager like any other VPN.

Target system: Ubuntu 26.04, KDE Plasma 6.6.6, NetworkManager 1.54.3.

## Roadmap

### Phase 0: skeleton (done)
- [x] Repository layout, Go module, Makefile
- [x] NM `.name` file and D-Bus policy
- [x] Plasma plugin scaffold (CMake, widget/auth stubs, plugin metadata)

### Phase 1: service and nmcli
Goal: `nmcli connection up` works with a real `ss://` key.

1. **D-Bus layer verified against libnm (done)**
   - [x] Constants, error names and D-Bus types checked against NM 1.54.3 sources
   - [x] Typed encoders for `Config`, `Ip4Config` and `Ip6Config` (`internal/nmvpn/config.go`)
   - [x] State machine matching libnm's `NMVpnServicePlugin`: connect timeout, auto-STARTED, disconnect rules, failure handling
   - [x] Full introspection data
   - [x] Idle exit (180 s) and peer watching (quit when NetworkManager disconnects)
   - [x] Unit tests and integration tests against a private `dbus-daemon`
2. **Fill in the stubs**
   - [ ] `internal/tun`: open `/dev/net/tun` with `TUNSETIFF` (`IFF_TUN|IFF_NO_PI`)
   - [ ] `internal/outline/accesskey.go`: parse `ss://` (SIP002 and legacy base64), keep `?prefix=`, extract the server host
   - [ ] `internal/outline/device.go`: add outline-sdk; stream dialer, packet listener, UDP relay with DNS-truncate fallback, `lwip2transport` device
   - [ ] `internal/outline/handler.go`: the connect flow (see [architecture.md](architecture.md#connection-lifecycle))
3. **Tests**
   - [ ] Access-key parsing (sample keys, prefix, fragment, legacy format)
   - [ ] Handler test with a fake emitter (no root)
4. **Manual end-to-end check** (needs root and a real key)
   - [ ] Default route through `outline0`, host route to the server via the physical link
   - [ ] DNS through the tunnel (`resolvectl status`)
   - [ ] TCP and UDP traffic; DNS-truncate fallback with a server without UDP
   - [ ] `nmcli connection down` cleans up; reconnect works

### Phase 2: dynamic keys and robustness
- [ ] `ssconf://` keys: fetch over HTTPS; response is an `ss://` URL or JSON (`server`, `server_port`, `password`, `method`, `prefix`)
- [ ] IPv6 modes: `tunnel` (ULA address and `::/0` into the tunnel) and `block`
- [ ] Reconnect on network change (`can-persist`, re-resolve the server)
- [ ] Interactive secrets (`ConnectInteractive`, `SecretsRequired`, `NewSecrets`) if needed
- [ ] Logging to the journal

### Phase 3: Plasma plugin
- [ ] Install Qt6/KF6 dev packages (see [plasma-plugin.md](plasma-plugin.md))
- [ ] Settings widget: load and save `vpn.data`, the access-key secret and its flags
- [ ] Secret prompt widget
- [ ] Import a `.txt` file or pasted `ss://` / `ssconf://` key

### Phase 4: packaging and other desktops
- [ ] Debian packages (`network-manager-outline`, `plasma-nm-outline` pinned to the plasma-nm version)
- [ ] GNOME / nm-connection-editor plugin (libnma, GTK3 and GTK4)
- [ ] Propose the Plasma plugin upstream to plasma-nm

## Decisions

### Go for the service
outline-sdk is a Go library with no C API. Writing the service in Go uses it
directly. The alternatives (a C service linking the SDK built as a C library,
or a C service driving a Go helper process) still need Go glue and add a
language boundary. NetworkManager talks to VPN services over D-Bus only, so
libnm is not required: `internal/nmvpn` implements the protocol with `godbus`.
The service builds to a single binary.

Costs: a larger binary (about 10 to 15 MB), cgo for lwIP (a C compiler is needed
to build), and we must match libnm's plugin behaviour ourselves (see
[nm-vpn-dbus-protocol.md](nm-vpn-dbus-protocol.md)).

### NetworkManager owns routing and DNS
outline-cli changes routing rules and resolv.conf itself. This plugin instead
reports the tunnel device, addresses and DNS to NetworkManager, which installs
routes, configures systemd-resolved and adds a host route to the VPN server
via the underlying device. This avoids conflicts with NetworkManager and gives
clean teardown.

### UI per desktop
The settings dialog must match the desktop: C++/Qt for Plasma, C/GTK for
GNOME. The service and the UI share only the setting names
(`internal/outline/keys.go` and `plasma/src/nm-outline-service.h`).

### Makefile as the entry point
Go and CMake do the building; the Makefile wraps them and installs data files
with overridable paths (`PREFIX`, `DESTDIR`) for packaging.

## Risks and open questions

- **Plasma plugin ABI.** plasma-nm has no public plugin API; the plugin links
  internal libraries and breaks on plasma-nm upgrades. See
  [plasma-plugin.md](plasma-plugin.md).
- **No kill switch.** Traffic can leave unprotected while connecting or
  reconnecting. NetworkManager has no generic kill switch for VPN plugins; it
  would need firewall rules.
- **Servers without UDP.** DNS-truncate mode covers DNS, but QUIC and VoIP fall
  back to TCP or fail.
- **Licensing.** outline-sdk is Apache-2.0; plasma-nm is GPL/LGPL. The Plasma
  plugin must be GPL-compatible. The service stays Apache-2.0.
- **Module path.** The Go module is named `network-manager-outline` as a
  placeholder until the repository has a public home.
