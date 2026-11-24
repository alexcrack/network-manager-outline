# Plan

## Goal

Connect to Outline servers from the system network settings (KDE Plasma
first) using an Outline access key, without the Outline client. The tunnel is
implemented with [outline-sdk](https://github.com/OutlineFoundation/outline-sdk)
and managed by NetworkManager like any other VPN.

Target system: Ubuntu 26.04, KDE Plasma 6.6.6, NetworkManager 1.54.3.

## Status (2026-11-24)

The service works end to end with `nmcli`: a real Outline server carries IPv4,
IPv6, DNS and UDP traffic, and disconnect and reconnect are clean. A few
phase 1 checks remain (see below). There is no settings UI yet, so connections
are created with `nmcli`.

| PR | Contents |
|---|---|
| #1 | D-Bus layer verified against libnm, docs |
| #2 | Outline tunnel on outline-sdk |
| #3 | Docs: running the TUN test as root |
| #4 | Fixes from the first end-to-end run |

## Next steps

1. Finish phase 1: retest the wrong key (expect a login-failure message) with
   other VPNs disconnected, and check the Plasma applet with a
   `nmcli`-created connection.
2. Phase 3 (Plasma plugin) before phase 2. The goal is connecting from the
   system dialog, and phase 2 items are robustness work that the current
   service can do without. Doing the UI earlier also finds out sooner whether
   the out-of-tree plugin loads in Plasma at all (the main project risk).
3. Phase 2, starting with `ssconf://` keys (needed by many Outline providers)
   and reconnect on network change.

## Roadmap

### Phase 0: skeleton (done)
- [x] Repository layout, Go module, Makefile
- [x] NM `.name` file and D-Bus policy
- [x] Plasma plugin scaffold (CMake, widget/auth stubs, plugin metadata)

### Phase 1: service and nmcli (almost done)
Goal: `nmcli connection up` works with a real `ss://` key.

1. **D-Bus layer verified against libnm (done)**
   - [x] Constants, error names and D-Bus types checked against NM 1.54.3 sources
   - [x] Typed encoders for `Config`, `Ip4Config` and `Ip6Config` (`internal/nmvpn/config.go`)
   - [x] State machine matching libnm's `NMVpnServicePlugin`: connect timeout, auto-STARTED, disconnect rules, failure handling
   - [x] Full introspection data
   - [x] Idle exit (180 s) and peer watching (quit when NetworkManager disconnects)
   - [x] Unit tests and integration tests against a private `dbus-daemon`
2. **Fill in the stubs (done)**
   - [x] `internal/tun`: open `/dev/net/tun` with `TUNSETIFF` (`IFF_TUN|IFF_NO_PI`), set MTU, bring the link up
   - [x] `internal/outline/accesskey.go`: parse `ss://` (SIP002 and legacy base64), keep `?prefix=`, resolve the server, build configurl strings with the resolved IP
   - [x] `internal/outline/device.go`: outline-sdk v0.1.0 / sdk/x v0.3.0; stream dialer, packet listener, UDP relay with DNS-truncate fallback, `lwip2transport` device, TCP check, UDP probe
   - [x] `internal/outline/handler.go`: the connect flow (see [architecture.md](architecture.md#connection-lifecycle))
3. **Tests (done)**
   - [x] Access-key parsing (Outline SIP002, plain and std-base64 userinfo, legacy base64, prefix, fragment, IPv6 server, errors)
   - [x] Generated configurl strings accepted by outline-sdk, with and without a transport prefix
   - [x] End-to-end through the real outline-sdk stack against an in-process Shadowsocks server: success, wrong password, unreachable server
   - [x] Handler flow with fake TUN/stack/emitter: reported config, `ipv6=off`, check failure, pump failure, disconnect during setup, bad settings
   - [x] TUN device test as root (skipped otherwise): `go test -c -o bin/tun.test ./internal/tun && sudo bin/tun.test -test.v`
4. **Manual end-to-end check** (needs root and a real key). First run on 2026-11-24, Ubuntu 26.04, NetworkManager 1.54.3:
   - [x] Default route through `outline0`, host route to the server via the physical link
   - [x] DNS through the tunnel (`resolvectl status`: `1.1.1.1`, domain `~.`)
   - [x] TCP over IPv4 and IPv6 (`curl -4/-6 https://ifconfig.me` show the server), UDP relayed (`dig`)
   - [x] `nmcli connection down` removes `outline0`; reconnect works
   - [x] Service exits right after disconnect (fixed in PR #4; retested: the next connection started a new process)
   - [x] Wrong password fails after 10 s (the check timeout; Outline servers never answer a wrong key). nmcli said "Unknown reason"; now it reports a login failure, see below.
   - [ ] Connection created with `nmcli` appears in the Plasma network applet and can be toggled there (no UI plugin needed for this)
   - [ ] DNS-truncate fallback with a server without UDP (needs such a server; optional)

   Bugs found by the first run, fixed in PR #4:
   - The service treated `NM_VPN_LOG_LEVEL` being set as debug mode. NetworkManager always sets it (to a syslog level), so the service never quit after disconnect and logged at debug level. Now only `--debug` disables quitting; the level comes from the variable's value.
   - An outline-sdk error quoted the transport config, which put the key's credentials in the journal. All logged errors are now redacted.
   - A key with an unknown cipher failed with a confusing base64 error. The cipher is now validated when the settings are parsed, so it fails immediately with "unsupported cipher".
   Found by the retest, fixed:
   - nmcli printed "Unknown reason" for a wrong key. NetworkManager only gives a specific reason for `LOGIN_FAILED`, and the service always sent `CONNECT_FAILED`, which disconnecting during STARTING sent again on top. The service now checks plain TCP reachability first, reports a timeout through Shadowsocks as `LOGIN_FAILED` (wrong key), and keeps the first reported reason.
   - Note: the official Outline client was connected during this retest (`outline-tun1`, source address `10.0.85.5` in the log), so the check traffic went through it. Disconnect other VPNs when testing.
5. **Install**
   - [x] `make && sudo make install` works. Install targets no longer build (they depended on the phony `service` target, so `sudo make install` ran `go build` as root, and sudo's PATH has no `go`). They fail clearly if nothing is built and warn if sources are newer than the binary. Added `uninstall`.

### Phase 2: dynamic keys and robustness
- [ ] `ssconf://` keys (currently rejected with a clear error): fetch over HTTPS; response is an `ss://` URL or JSON (`server`, `server_port`, `password`, `method`, `prefix`)
- [ ] IPv6 `block` mode (route IPv6 into the tunnel and reject it there). `tunnel` (default) and `off` exist since phase 1.
- [ ] Reconnect on network change (`can-persist`, re-resolve the server)
- [ ] Interactive secrets (`ConnectInteractive`, `SecretsRequired`, `NewSecrets`) if needed
- [x] Logging to the journal: done in phase 1. NetworkManager passes the service's stderr to the journal, and the level follows `NM_VPN_LOG_LEVEL`.

### Phase 3: Plasma plugin (next)
- [ ] Install Qt6/KF6 dev packages (see [plasma-plugin.md](plasma-plugin.md))
- [ ] Build against plasma-nm 6.6.6 sources and confirm Plasma loads the plugin ("Outline VPN" appears under Add connection)
- [ ] Settings widget: load and save `vpn.data`, the access-key secret and its flags
- [ ] Secret prompt widget
- [ ] Import a `.txt` file or pasted `ss://` / `ssconf://` key
- [ ] End-to-end: create, edit, connect and disconnect from Plasma System Settings and the network applet

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

Costs: a larger binary (16 MB; it links only libc), cgo for lwIP (a C compiler
is needed to build), and we must match libnm's plugin behaviour ourselves (see
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

### Server hostname replaced by its IP
The service resolves the server's hostname once, before NetworkManager
changes routes and DNS, and dials that IP. Dialing the hostname would loop:
after the tunnel is up, DNS goes through the tunnel. NetworkManager also
routes only the resolved IP (the `gateway` in `Config`) around the tunnel.

### Transport prefix applies to TCP only
Prefixes such as `split:2` and `tlsfrag:1` only exist for stream dialers in
outline-sdk, so the prefix is applied to the TCP dialer and UDP uses the plain
Shadowsocks config. Found by a test against the SDK.

### Fail early on a bad key or unreachable server
Before reporting the tunnel to NetworkManager, the service resolves a name
over TCP through the server. A wrong password or unreachable server then fails
the connection in the dialog instead of leaving a dead tunnel. The UDP probe
runs after the tunnel is up so it does not delay connecting; DNS works over
TCP (DNS-truncate mode) until the probe confirms UDP.

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
  placeholder. The repository is now at `github.com/alexcrack/network-manager-outline`;
  renaming the module to that path is a one-line change plus imports.
