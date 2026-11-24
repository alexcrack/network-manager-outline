# Architecture

## Components

```
Plasma settings ──(plasma/ Qt plugin)──► NM connection profile
                                         vpn.service-type = org.freedesktop.NetworkManager.outline
                                         vpn.data         = dns, ipv6, never-default, transport-prefix
                                         vpn.secrets      = access-key
                                                │
NetworkManager ──D-Bus (system bus)──► nm-outline-service (Go, runs as root)
                                         ├─ internal/nmvpn    D-Bus protocol, state machine
                                         └─ internal/outline  access key → outline-sdk device
                                              ├─ internal/tun  TUN device (outline0)
                                              └─ outline-sdk   lwip2transport + Shadowsocks
NetworkManager then applies addresses, routes, DNS and a host route to the server.
```

| Component | Language | Installed as |
|---|---|---|
| `cmd/nm-outline-service` | Go | `/usr/libexec/nm-outline-service` |
| `data/nm-outline-service.name.in` | INI | `/usr/lib/NetworkManager/VPN/nm-outline-service.name` |
| `data/nm-outline-service.conf` | D-Bus policy | `/usr/share/dbus-1/system.d/nm-outline-service.conf` |
| `plasma/` | C++/Qt6/KF6 | `<qt6 plugins>/plasma/network/vpn/plasmanetworkmanagement_outlineui.so` |

## How NetworkManager finds the service

NetworkManager reads every `.name` file in `/usr/lib/NetworkManager/VPN/`. A
connection with `vpn.service-type=org.freedesktop.NetworkManager.outline` (or
the short form `vpn-type outline` in nmcli) maps to our file, which names the
D-Bus service and the program to start. When the connection is activated,
NetworkManager starts the program as root and waits for it to claim the bus
name. The D-Bus policy allows only root to own the name.

Plasma finds its UI plugin by the `X-NetworkManager-Services` key in the
plugin's JSON metadata, which must match the same service name.

## Packages

### `internal/nmvpn`
A reusable, pure-Go implementation of `org.freedesktop.NetworkManager.VPN.Plugin`.
It knows nothing about Outline.

- `Plugin` exports the D-Bus object, claims the bus name, runs the state
  machine and timers. See [nm-vpn-dbus-protocol.md](nm-vpn-dbus-protocol.md).
- `Handler` is the interface a backend implements: `NeedSecrets`, `Connect`,
  `Disconnect`.
- `Emitter` is what the backend uses to report back: `SetConfig`,
  `SetIP4Config`, `SetIP6Config`, `Fail`. The plugin reports STARTED by itself
  once all announced configs have been sent.
- `GenericConfig` and `IPConfig` are typed structs that encode to the exact
  D-Bus dictionaries NetworkManager parses. This keeps address-encoding details
  out of the backend.
- `ConnectionSettings` gives typed access to `vpn.data`, `vpn.secrets` and the
  connection ID.
- Errors: backends return `nmvpn.Errorf(code, ...)` to choose the D-Bus error
  name; other errors map to `...VPN.Error.Failed`.

### `internal/outline`
The Outline backend.

- `keys.go`: setting names stored in the connection profile.
- `accesskey.go`: parses `ss://` keys and the profile settings, resolves the
  server, and builds the `configurl` strings (`Transport.Stream` with the
  optional prefix, `Transport.Packet` without).
- `device.go`: wraps outline-sdk (stream dialer, packet listener, UDP relay
  with DNS-truncate fallback, lwIP device), plus the TCP check, the UDP probe
  and the packet pump.
- `handler.go`: implements `nmvpn.Handler`. Each connection is a `session`
  goroutine that owns the TUN device and the stack. TUN and stack creation are
  injectable so the flow is tested without root.

Dependencies: `golang.getoutline.org/sdk` v0.1.0 and
`golang.getoutline.org/sdk/x` v0.3.0 (Go 1.25).

### `internal/tun`
Opens a TUN device. Only the file descriptor is needed: NetworkManager assigns
the address and brings the link up.

## Connection lifecycle

```
NetworkManager                      nm-outline-service
──────────────                      ──────────────────
spawn program, wait for bus name ─► Serve(): export object, claim name, start idle timer
NeedSecrets(settings)            ─► "" if vpn.secrets has access-key, else "vpn"
  (if "vpn": ask secret agent, e.g. Plasma/KWallet)
ConnectInteractive(...)          ─► error InteractiveNotSupported
Connect(settings)                ─► state STARTING; Handler.Connect parses the settings
                                    (invalid → BadArguments, STOPPED) and starts a session
                                    goroutine; reply (connect timer: 60 s to reach STARTED)
                                    session:
                                      1. resolve the server host (10 s), prefer IPv4
                                      2. build the outline-sdk stack with the IP, not the hostname
                                      3. TCP check: resolve www.google.com over DNS-over-TCP
                                         through the server (10 s); fails on wrong key/unreachable
                                      4. open TUN outline%d (MTU 1500, link up), start the pump
                                    ◄─ Config {tundev, gateway=server IP, mtu 1500, has-ip4, has-ip6}
                                    ◄─ Ip4Config {10.233.233.1/32, dns, domains ~., never-default}
                                    ◄─ Ip6Config {fdcc:a7f5:9233::1/128, dns}   (ipv6=tunnel)
                                    ◄─ StateChanged STARTED
                                      5. probe UDP in the background (10 s); switch the UDP
                                         relay from DNS-truncate to the server if supported
apply addresses, routes, DNS,
host route to server via the
underlying device
...
Disconnect()                     ─► STOPPING; cancel ctx; session closes TUN then the stack;
                                    Handler.Disconnect waits (10 s); STOPPED
                                    with peer watching: quit shortly after STOPPED
```

Any failure in the session (resolve, stack, TCP check, TUN, config, or the
packet pump stopping later) goes through `Emitter.Fail`: the plugin emits
`Failure(CONNECT_FAILED)` and disconnects, and NetworkManager marks the
connection failed when it sees STOPPED. A user disconnect during setup is not
reported as a failure.

The lwIP stack in outline-sdk is a process-wide singleton, which matches
`supports-multiple-connections=false`. The plugin never starts a new
connection before the previous session has been torn down.

## Routing and DNS

- **Server route.** `Config.gateway` carries the resolved server address.
  NetworkManager adds a host route to it through the current default device,
  so Shadowsocks traffic does not loop into the tunnel. The server must be
  resolved before the tunnel's routes are installed.
- **Default route.** Unless `never-default=yes`, NetworkManager routes all
  traffic through `outline0`.
- **DNS.** IPv4 servers from `vpn.data dns` (default `1.1.1.1`) go in
  `Ip4Config.dns`, IPv6 ones in `Ip6Config.dns`. Unless `never-default` is set,
  the routing domain `~.` is pushed too, so systemd-resolved sends all lookups
  to these servers and none leak to other links. Queries go through the tunnel
  as UDP once the probe confirms the server relays UDP; until then (or if it
  does not) DNS answers are truncated so resolvers retry over TCP.
- **IPv6.** `tunnel` (default): a ULA address and default route into the
  tunnel so IPv6 does not leak; lwIP (built with `LWIP_IPV6`) terminates it and
  the server dials IPv6 destinations if it can. `off`: no IPv6 config, so IPv6
  traffic bypasses the VPN. `block` is planned (phase 2).
- **Tunnel addresses.** `10.233.233.1/32` and `fdcc:a7f5:9233::1/128` are only
  used on this host; lwIP terminates all connections locally.

## Settings

Stored in the NetworkManager connection profile. Names are defined in
`internal/outline/keys.go` and mirrored in `plasma/src/nm-outline-service.h`.

| Key | Where | Values | Default |
|---|---|---|---|
| `access-key` | `vpn.secrets` | `ss://…` or `ssconf://…` | required |
| `dns` | `vpn.data` | comma-separated IPs | `1.1.1.1` |
| `ipv6` | `vpn.data` | `tunnel`, `off` | `tunnel` |
| `never-default` | `vpn.data` | `yes`, `no` (also `true`/`false`, `1`/`0`) | `no` |
| `transport-prefix` | `vpn.data` | outline-sdk `configurl` prefix for TCP, e.g. `split:2`, `tlsfrag:1` | empty |

Invalid values fail the connection with `BadArguments` before anything is
started. Errors never include the access key.

Supported access keys: `ss://` in Outline/SIP002 form (base64url, standard
base64 or plain userinfo), and the legacy all-base64 form, with optional
`?prefix=` and `#name`. `ssconf://` keys are rejected for now (phase 2).

The access key contains the Shadowsocks password, so it is a secret. Its
storage follows the standard `access-key-flags`: system-owned (stored by
NetworkManager), agent-owned (KWallet via Plasma), or always ask.

## Repository layout

| Path | Purpose |
|---|---|
| `cmd/nm-outline-service` | Service entry point |
| `internal/nmvpn` | NM VPN plugin protocol |
| `internal/outline` | Outline backend |
| `internal/tun` | TUN device |
| `data/` | NM `.name` file, D-Bus policy |
| `plasma/` | Plasma UI plugin |
| `gnome/` | GNOME plugin (phase 4) |
| `debian/` | Packaging (phase 4) |
| `docs/` | This documentation |

## Testing

- `make test` runs everything that needs no root:
  - `internal/nmvpn`: unit tests and D-Bus integration tests against a private
    `dbus-daemon` (skipped when it is not installed).
  - `internal/outline`: key and settings parsing; the real outline-sdk stack
    against an in-process Shadowsocks server and fake DNS server (success,
    wrong password, unreachable server); the connect flow with fake TUN, stack
    and emitter.
- `sudo -E go test ./internal/tun` exercises the real TUN device (skipped
  without CAP_NET_ADMIN).
- End-to-end tests need root, an installed build and a real access key; see
  phase 1 step 4 in [plan.md](plan.md).
