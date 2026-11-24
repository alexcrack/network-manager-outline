# NetworkManager VPN plugin D-Bus protocol

Reference for `internal/nmvpn`. Everything here was checked against
NetworkManager **1.54.3** (the version on the target system):

| Source | Used for |
|---|---|
| `/usr/include/libnm/nm-vpn-dbus-interface.h` | Constants, states, failure codes, config keys |
| `/usr/include/libnm/nm-errors.h` | `NMVpnPluginError` codes |
| `introspection/org.freedesktop.NetworkManager.VPN.Plugin.xml` | Method and signal signatures |
| `src/core/vpn/nm-vpn-connection.c` | How the daemon parses `Config` / `Ip4Config` / `Ip6Config` |
| `src/libnm-client-impl/nm-vpn-service-plugin.c` | libnm's reference plugin behaviour (state machine, timers) |

## Object

| | |
|---|---|
| Bus | system |
| Name | `org.freedesktop.NetworkManager.outline` (from the `.name` file) |
| Path | `/org/freedesktop/NetworkManager/VPN/Plugin` |
| Interface | `org.freedesktop.NetworkManager.VPN.Plugin` |

## Methods

| Method | Signature | Our behaviour |
|---|---|---|
| `Connect` | `(a{sa{sv}} connection)` | Allowed only in INIT or STOPPED, else `WrongState`. Enters STARTING, calls `Handler.Connect`, starts the connect timer. |
| `ConnectInteractive` | `(a{sa{sv}} connection, a{sv} details)` | Returns `InteractiveNotSupported`; NetworkManager then falls back to `Connect`. |
| `NeedSecrets` | `(a{sa{sv}} settings) → s` | Returns `"vpn"` when the access key is missing, else `""`. Pushes back the idle timer when secrets are needed. |
| `NewSecrets` | `(a{sa{sv}} connection)` | `WrongState` unless STARTING, else `InteractiveNotSupported`. |
| `Disconnect` | `()` | See [Disconnect rules](#disconnect-rules). |
| `SetConfig`, `SetIp4Config`, `SetIp6Config` | `(a{sv})` | For helper processes of script-based plugins. Same as the `Emitter` methods. |
| `SetFailure` | `(s reason)` | Emits `Failure(BAD_IP_CONFIG)` and disconnects (as libnm). |

## Property

`State` (`u`, read-only): the current `NMVpnServiceState`. Changes are
announced with `PropertiesChanged` as well as `StateChanged`.

## Signals

| Signal | Signature | Notes |
|---|---|---|
| `StateChanged` | `(u state)` | NetworkManager fails an activating or active connection when it sees STOPPED. |
| `Config` | `(a{sv})` | Generic config. Must be sent before IP configs. |
| `Ip4Config` | `(a{sv})` | |
| `Ip6Config` | `(a{sv})` | |
| `Failure` | `(u reason)` | `NMVpnPluginFailure`. |
| `SecretsRequired` | `(s message, as secrets)` | Interactive secrets only; not used yet. |
| `LoginBanner` | `(s banner)` | Not used. |

## States (`NMVpnServiceState`)

| Value | Name |
|---|---|
| 0 | UNKNOWN |
| 1 | INIT |
| 2 | SHUTDOWN |
| 3 | STARTING |
| 4 | STARTED |
| 5 | STOPPING |
| 6 | STOPPED |

### Transitions

```
INIT/STOPPED ──Connect──► STARTING ──all configs sent──► STARTED
                             │ Handler.Connect error ──► STOPPED (after 100 ms, so the error reply arrives first)
                             │ connect timer (60 s) ───► Failure(CONNECT_FAILED), STOPPING, STOPPED
STARTING/STARTED ──Disconnect / Fail / peer vanished──► STOPPING ──► STOPPED
```

**Auto STARTED.** As in libnm, the plugin switches to STARTED when, in
STARTING, `Config` has been sent and `has-ip4 == got Ip4Config` and
`has-ip6 == got Ip6Config`. If `Ip4Config` arrives before any `Config`,
`has-ip4` is assumed true (libnm compatibility for old plugins).

### Disconnect rules

Follows `nm_vpn_service_plugin_disconnect`:

| State | Result |
|---|---|
| STOPPING | error `StoppingInProgress` |
| STOPPED | error `AlreadyStopped` |
| INIT | STOPPED, success |
| STARTING | emit `Failure(CONNECT_FAILED)` unless a failure was already emitted, then as STARTED |
| STARTED | STOPPING → cancel connection context → `Handler.Disconnect` → STOPPED |

### Timers and lifetime

| Timer | Value | Behaviour |
|---|---|---|
| Connect timer | 60 s | Started after a successful `Connect`; disconnects if STARTED is not reached. |
| Idle (quit) timer | 180 s | Runs in INIT and STOPPED; the service exits when it fires. Cancelled on STARTING. |
| Peer watching | on unless `--debug` | Remembers the sender of `Connect` (NetworkManager). If it leaves the bus, the connection is torn down. On STOPPED the service exits after 100 ms instead of waiting for the idle timer. Same as nm-openvpn-service's `watch-peer`. |

SIGINT and SIGTERM disconnect an active connection, then exit.

## Failure codes (`NMVpnPluginFailure`)

| Value | Name |
|---|---|
| 0 | LOGIN_FAILED |
| 1 | CONNECT_FAILED |
| 2 | BAD_IP_CONFIG |

## Errors

D-Bus error names are `org.freedesktop.NetworkManager.VPN.Error.<Nick>`:
`Failed`, `StartingInProgress`, `AlreadyStarted`, `StoppingInProgress`,
`AlreadyStopped`, `WrongState`, `BadArguments`, `LaunchFailed`,
`InvalidConnection`, `InteractiveNotSupported`.

Handlers choose one with `nmvpn.Errorf(nmvpn.ErrBadArguments, ...)`; any other
error becomes `Failed`.

## Config dictionaries

### `Config`

| Key | Type | Notes |
|---|---|---|
| `gateway` | `u` (IPv4) or `ay` (IPv6) | **Required.** External VPN server address. Without it NetworkManager logs "no VPN gateway address received" and fails. NM adds a host route to it via the underlying device. |
| `tundev` | `s` | Tunnel interface name. NM looks up its ifindex; `""` and `_none_` mean no device. |
| `mtu` | `u` | |
| `has-ip4` | `b` | Missing means false. |
| `has-ip6` | `b` | Missing means false. |
| `can-persist` | `b` | Service can keep the tunnel across underlying device changes. |
| `banner` | `s` | |
| `pac` | `s` | Proxy auto-config URL. |

### `Ip4Config` / `Ip6Config`

| Key | IPv4 type | IPv6 type | Notes |
|---|---|---|---|
| `address` | `u` | `ay` | Zero address is rejected. |
| `prefix` | `u` | `u` | Default 24 (IPv4) / 128 (IPv6). Must be 1–32 / 1–128. |
| `ptp` | `u` | `ay` | IPv4 default: same as `address`. |
| `internal-gateway` | `u` | `ay` | |
| `dns` | `au` | `aay` | |
| `nbns` | `au` | – | |
| `domain` | `s` | `s` | |
| `domains` | `as` | `as` | |
| `routes` | `aau` | `a(ayuayu)` or `a(ayuayuay)` | IPv4 items: `[network, prefix, gateway, metric]` with optional 5th `pref_src`. IPv6: `(dest, prefix, next_hop, metric[, pref_src])`. |
| `preserve-routes` | `b` | `b` | |
| `never-default` | `b` | `b` | Missing means false (default route via the VPN). |
| `mss` | `u` | `u` | |

### IPv4 address encoding

NetworkManager reads IPv4 addresses as a `u` and writes that value to memory
in **native** byte order (`unaligned_write_ne32`). The `u` must therefore be
the native-endian integer whose in-memory bytes are the address bytes:

```go
b := addr.As4()
u := binary.NativeEndian.Uint32(b[:]) // 10.233.233.1 → bytes 0a e9 e9 01 in memory
```

This is not network byte order on little-endian machines. `nmvpn.IP4ToUint32`
does this and is covered by a test.

## Differences from libnm's NMVpnServicePlugin

| Area | libnm | nmvpn | Why |
|---|---|---|---|
| Entering STARTING | after `connect()` returns success | before calling `Handler.Connect` | Handler goroutines may emit config immediately; this avoids config arriving in INIT. A failed connect goes STARTING → STOPPED. |
| Failed-connect stop | idle callback | 100 ms timer | godbus sends the reply after the method returns; the delay keeps the reply before the STOPPED signal. |
| Idle timer at startup | only after STOPPED | also at startup | A service started but never connected also exits. |
| Failure reason during STARTING | disconnecting emits `CONNECT_FAILED` after the plugin's own `Failure` | only if no `Failure` was emitted yet | NM keeps only the last reason, and `LOGIN_FAILED` is the one nmcli and Plasma can explain. |
| `Fail` → disconnect | synchronous | asynchronous | Lets handlers call `Fail` from goroutines that `Disconnect` waits for, without deadlock. |
| `NeedSecrets` idle reschedule | always | only in INIT/STOPPED | Avoids arming the quit timer during an active connection. |
| `Ip4Config` compat merge | copies `tundev`, `gateway`, `mtu`, `banner` into `Ip4Config` | not done | Only needed for NetworkManager before 1.2. |
| `ConnectInteractive` | optional | always `InteractiveNotSupported` | Not needed until interactive secrets (phase 2). |
