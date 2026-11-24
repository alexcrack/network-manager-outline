# network-manager-outline

NetworkManager VPN plugin for [Outline](https://getoutline.org) built on
[outline-sdk](https://github.com/OutlineFoundation/outline-sdk). Connect with an
Outline access key from the system network settings, no Outline client needed.

**Status: phase 1 in progress.** The NetworkManager D-Bus layer is done and tested; the tunnel itself is not implemented yet, so nothing connects. See [docs/plan.md](docs/plan.md).

Documentation: [docs/](docs/README.md).

## Architecture

```
Plasma settings ──(plasma/ Qt plugin)──► NM profile (vpn.service-type=org.freedesktop.NetworkManager.outline)
NetworkManager ──D-Bus──► nm-outline-service (Go, root)
                              ├─ resolve access key (ss:// / ssconf://)
                              ├─ open TUN
                              ├─ outline-sdk lwip2transport + shadowsocks
                              └─ report tundev / IP / DNS / server address to NM
```

NetworkManager handles addresses, routes (including the host route to the
server), DNS and teardown.

## Layout

| Path | Purpose |
|---|---|
| `cmd/nm-outline-service` | Service entry point |
| `internal/nmvpn` | Pure-Go `org.freedesktop.NetworkManager.VPN.Plugin` D-Bus implementation |
| `internal/outline` | Access-key handling, outline-sdk device, NM handler |
| `internal/tun` | TUN device open |
| `data/` | NM `.name` file, D-Bus policy |
| `plasma/` | Plasma (plasma-nm) VPN editor plugin, Qt6/KF6 |
| `gnome/` | libnma editor plugin (phase 4) |
| `debian/` | Packaging (phase 4) |
| `docs/` | Plan, architecture, protocol reference |

## Building

```sh
make service            # Go service -> bin/nm-outline-service
sudo make install       # service + .name + D-Bus policy
make plasma             # Plasma plugin (fetches plasma-nm headers matching the installed version)
sudo make install-plasma
```

Plasma plugin build dependencies (Ubuntu):

```sh
sudo apt install qt6-base-dev libkf6networkmanagerqt-dev libkf6i18n-dev \
    libkf6coreaddons-dev libkf6widgetsaddons-dev libnm-dev extra-cmake-modules
```

The Plasma plugin links the internal `libplasmanm_editor.so`, which has no
stable ABI. Rebuild after every plasma-nm upgrade.

## Using it without a GUI (once phase 1 is done)

```sh
nmcli connection add type vpn vpn-type outline con-name outline \
    vpn.secrets "access-key=ss://..." vpn.data "dns=1.1.1.1"
nmcli connection up outline
```

## Roadmap

See [docs/plan.md](docs/plan.md).

## License

Service: Apache-2.0 (same as outline-sdk). Plasma plugin: GPL-2.0-or-later (links plasma-nm).
