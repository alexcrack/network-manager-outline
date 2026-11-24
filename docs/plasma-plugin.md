# Plasma plugin

## How Plasma loads VPN UIs

The Plasma network settings (plasma-nm) list a VPN type only if a UI plugin
for it exists. Plugins live in
`/usr/lib/x86_64-linux-gnu/qt6/plugins/plasma/network/vpn/` and are matched to
a connection by the `X-NetworkManager-Services` key in their JSON metadata
(`org.freedesktop.NetworkManager.outline` for us).

A plugin subclasses `VpnUiPlugin` and provides:

| Method | Purpose |
|---|---|
| `widget()` | The settings form (`SettingWidget`) shown in the connection editor |
| `askUser()` | The secret prompt shown by Plasma's secret agent |
| `suggestedFileName()` | Export file name |
| `supportedFileExtensions()`, `importConnectionSettings()` | "Import VPN connection…" |

`SettingWidget::setting()` returns the VPN setting as a `QVariantMap`
(`service-type`, `data`, `secrets`, secret flags).

## The ABI problem

plasma-nm does not install its editor headers (`vpnuiplugin.h`,
`settingwidget.h`, `passwordfield.h`), and it has no stable plugin API. Out-of-tree
plugins therefore:

1. compile against a plasma-nm **source checkout of exactly the installed
   version** (headers only), and
2. link the installed `libplasmanm_editor.so` and `libplasmanm_internal.so`.

`make plasma` reads the installed plasma-nm version with `dpkg-query` and clones
the matching tag into `plasma/third_party/plasma-nm`.

Consequences:
- The plugin must be rebuilt after every plasma-nm upgrade; otherwise it may
  fail to load or crash System Settings.
- A Debian package must depend on the exact plasma-nm version it was built
  against.
- The long-term fix is to upstream the plugin into plasma-nm, which already
  hosts third-party VPN UIs (fortisslvpn, sstp, iodine, …).

## Build dependencies (Ubuntu 26.04)

```sh
sudo apt install qt6-base-dev libkf6networkmanagerqt-dev libkf6i18n-dev \
    libkf6coreaddons-dev libkf6widgetsaddons-dev libnm-dev extra-cmake-modules
```

## Files

| File | Purpose |
|---|---|
| `plasma/CMakeLists.txt` | Build; finds the plasma-nm sources and system libraries |
| `src/outline.{h,cpp}` | `OutlineUiPlugin` (`VpnUiPlugin` subclass) |
| `src/outlinewidget.{h,cpp,ui}` | Settings form: access key (`PasswordField` with secret-storage options), DNS, IPv6 mode, transport prefix |
| `src/outlineauth.{h,cpp,ui}` | Access-key prompt |
| `src/nm-outline-service.h` | Setting names, mirrored from `internal/outline/keys.go` |
| `src/plasmanetworkmanagement_outlineui.json` | Plugin metadata |

## Status

Scaffold only (phase 3 fills it in). It has not been compiled yet because the
Qt6/KF6 development packages are not installed.
