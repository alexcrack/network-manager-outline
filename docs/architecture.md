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
- `accesskey.go`: turns `ss://` / `ssconf://` into an outline-sdk `configurl`
  string and the server host.
- `device.go`: wraps outline-sdk (stream dialer, packet listener, UDP relay
  with DNS-truncate fallback, lwIP device).
- `handler.go`: implements `nmvpn.Handler`.

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
Connect(settings)                ─► state STARTING; Handler.Connect:
                                      1. resolve access key, resolve server IP
                                      2. open TUN outline0
                                      3. build outline-sdk device, probe UDP, start packet pump
                                    ◄─ reply (connect timer: 60 s to reach STARTED)
                                    ◄─ Config {tundev, gateway=server, mtu, has-ip4, has-ip6}
                                    ◄─ Ip4Config {address 10.233.233.1/32, dns}
                                    ◄─ Ip6Config {address fd…/128, dns}   (if has-ip6)
                                    ◄─ StateChanged STARTED
apply addresses, routes, DNS,
host route to server via the
underlying device
...
Disconnect()                     ─► STOPPING; cancel ctx; Handler.Disconnect; STOPPED
                                    with peer watching: quit shortly after STOPPED
```

Failures after STARTED (for example the packet pump stops) go through
`Emitter.Fail`, which emits `Failure` and then disconnects. NetworkManager
marks the connection failed when it sees STOPPED.

## Routing and DNS

- **Server route.** `Config.gateway` carries the resolved server address.
  NetworkManager adds a host route to it through the current default device,
  so Shadowsocks traffic does not loop into the tunnel. The server must be
  resolved before the tunnel's routes are installed.
- **Default route.** Unless `never-default=yes`, NetworkManager routes all
  traffic through `outline0`.
- **DNS.** DNS servers from `vpn.data dns` (default `1.1.1.1`) are pushed in
  `Ip4Config.dns`; NetworkManager configures systemd-resolved. Queries go
  through the tunnel as UDP (or are truncated to force TCP when the server has
  no UDP support).
- **IPv6.** Default `tunnel`: a ULA address and default route so IPv6 does not
  leak. `block` is the alternative (phase 2).

## Settings

Stored in the NetworkManager connection profile. Names are defined in
`internal/outline/keys.go` and mirrored in `plasma/src/nm-outline-service.h`.

| Key | Where | Values | Default |
|---|---|---|---|
| `access-key` | `vpn.secrets` | `ss://…` or `ssconf://…` | required |
| `dns` | `vpn.data` | comma-separated IPs | `1.1.1.1` |
| `ipv6` | `vpn.data` | `tunnel`, `block` | `tunnel` |
| `never-default` | `vpn.data` | `yes`, `no` | `no` |
| `transport-prefix` | `vpn.data` | outline-sdk `configurl` prefix, e.g. `split:2` | empty |

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

- `make test` runs unit tests and the D-Bus integration tests. The integration
  tests start a private `dbus-daemon` (skipped when it is not installed) and
  need no root.
- End-to-end tests need root, an installed build and a real access key; see
  phase 1 step 4 in [plan.md](plan.md).
