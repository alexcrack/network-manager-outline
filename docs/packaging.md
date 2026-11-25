# Debian packaging

## Packages

| Package | Contents | Depends on |
|---|---|---|
| `network-manager-outline` | `/usr/libexec/nm-outline-service`, the NetworkManager `.name` file, the D-Bus policy, the `nm-outline` icon (SVG, and PNG at 16–128 px) | `network-manager`, libc |
| `plasma-nm-outline` | The Plasma VPN plugin | `network-manager-outline` of the same version, `plasma-nm` of the exact upstream version it was built against |

## Building

```sh
sudo apt install debhelper golang-go   # plus anything `dpkg-checkbuilddeps` lists
make deb
ls dist/
```

`make deb`:

1. fetches the plasma-nm sources matching the installed plasma-nm into
   `plasma/third_party/plasma-nm` (headers only, see
   [plasma-plugin.md](plasma-plugin.md));
2. runs `dpkg-buildpackage -us -uc -b`, which builds the service and the
   plugin, runs both test suites, and packs the `.deb` files;
3. moves the results from the parent directory into `dist/`.

Skip the tests with `DEB_BUILD_OPTIONS=nocheck make deb`.

The build needs network access when Go modules are not in the module cache
yet, and for fetching the plasma-nm headers the first time. Building in a
clean chroot (pbuilder, sbuild, a PPA) would need the Go modules vendored and
the plasma-nm headers in the source package; that is not set up yet.

## How `debian/rules` works

- **Two build systems in one tree.** The Go service is built with the
  top-level Makefile (`make service`, `make install-service`), the plugin with
  CMake in `obj-plasma/`. Each installs straight into its package directory.
- **Hardening.** Debian's build flags are passed to cgo, and the service is
  built as a position-independent executable (`-buildmode=pie`).
  `GOTOOLCHAIN=local` stops Go from downloading another toolchain and
  `-mod=readonly` from editing `go.mod`.
- **plasma-nm pinning.** The plugin links plasma-nm's internal editor
  library, which has no stable ABI and no shlibs information. `debian/rules`
  reads the installed plasma-nm version and generates
  `plasma-nm (>= 4:6.6.6), plasma-nm (<< 4:6.6.7~)` (for 4:6.6.6-0ubuntu0.1):
  any Debian revision of the same upstream version, nothing newer.
- **Go and dwz.** `dh_dwz` is skipped for the Go binary, whose DWARF dwz does
  not handle.

## Upgrading plasma-nm

Because of the pinning, apt keeps a newer plasma-nm back (or offers to remove
`plasma-nm-outline`) instead of letting System Settings load a plugin built
for another version. After the plasma-nm upgrade, rebuild and reinstall:

```sh
make clean && rm -rf plasma/third_party/plasma-nm   # fetch the new headers
make deb
sudo apt install ./dist/plasma-nm-outline_*.deb ./dist/network-manager-outline_*.deb
```

## Releasing a new version

Add an entry to `debian/changelog` (the version there is the package
version), for example with `dch -v 0.2.0` from `devscripts`.
