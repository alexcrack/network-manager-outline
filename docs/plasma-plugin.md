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
| `src/outlinewidget.{h,cpp,ui}` | Settings form: access key (`PasswordField` with secret-storage options), DNS, IPv6 mode, never-default, transport prefix |
| `src/outlineauth.{h,cpp,ui}` | Access-key prompt |
| `src/nm-outline-service.h` | Setting names, mirrored from `internal/outline/keys.go` |
| `src/outlineutils.{h,cpp}` | Key detection, connection names, secret flags ↔ storage menu |
| `src/compat/plasmanm_editor_export.h` | Stand-in for plasma-nm's generated export header |
| `src/plasmanetworkmanagement_outlineui.json` | Plugin metadata |
| `tests/plugintest.cpp` | Offscreen test of the built plugin |

## Implementation notes

- **Export header.** plasma-nm's headers include `plasmanm_editor_export.h`,
  which CMake generates in plasma-nm's build tree, so it is not in the source
  checkout. `src/compat/plasmanm_editor_export.h` defines the macros as
  imports, which is all a plugin needs.
- **Secrets.** The key is `vpn.secrets.access-key`; how it is stored is
  `vpn.data.access-key-flags` (NM secret flags): `0` everyone (stored by
  NetworkManager), `1` this user (agent, KWallet), `2` always ask. "Not
  required" is disabled because the key is always needed. With "always ask"
  no secret is saved and the prompt (`askUser`) appears on connect.
- **Stored settings.** Only non-default values go into `vpn.data`, so the
  service's defaults apply.
- **Import.** "Import VPN connection…" picks the plugin by file extension and
  the first match wins. The plugin claims `*.txt` only (OpenVPN and WireGuard
  claim `*.conf`). It takes the first `ss://` or `ssconf://` key in the file,
  names the connection after the key's `#fragment` (or the server), and stores
  the key for this user, like a new connection in the editor.
- **Activation.** plasma-nm refuses to activate any VPN connection whose type
  has no UI plugin ("Plasma is missing support for … VPN connections"), even
  if NetworkManager supports it. Installing this plugin is what makes the
  applet work.

## Testing

`make plasma-test` loads the built `.so` through `KPluginMetaData` and
`KPluginFactory`, as Plasma does, with `QT_QPA_PLATFORM=offscreen`, and checks:

- a new connection is invalid until a key is entered, and stores only the key
  and its flags;
- existing settings load and save back unchanged;
- "always ask" stores no secret;
- the prompt returns the key;
- importing a text file creates a valid connection named after the key, and
  a file without a key is rejected.

`ldd -r` on the plugin shows no unresolved symbols against the installed
plasma-nm, KF6 NetworkManagerQt and libnm.

## Status

Built and tested offscreen against plasma-nm 6.6.6, and used end to end in
System Settings and the network applet on Plasma 6.6.6 (2026-11-24).
