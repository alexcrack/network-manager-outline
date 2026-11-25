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
yet, and for fetching the plasma-nm headers the first time. It builds for the
Ubuntu release it runs on, with the plasma-nm installed there.

## Packages for Ubuntu releases and the PPA

```sh
make deb-series SERIES=noble          # or jammy, resolute; SNAPSHOT=1 for test builds
ls dist/noble/
```

`tools/deb-series.py` builds the source package (and the binaries) for one
Ubuntu release so that it builds without network, as Launchpad requires. It
copies the working tree to `build/deb/<series>/` and there:

- **vendors the Go modules** (`vendor/`), and adds the C sources of cgo
  modules that `go mod vendor` leaves out: it copies only directories with Go
  files, and go-tun2socks keeps lwIP's C code in others (205 files);
- **bundles the plasma-nm headers** (26.04 only) of the plasma-nm version
  apt would install, which is what Launchpad builds against, and pins the
  build dependency to it, e.g. `plasma-nm (>= 4:6.6.7), plasma-nm (<< 4:6.6.8~)`;
- **adapts `debian/control`**: 22.04 and 24.04 (Plasma 5) get only
  `network-manager-outline`, without the plugin's build dependencies, and
  Go from `golang-1.25-go`;
- **adds a changelog entry** with the version `<version>~ubuntu<NN.NN>.1`,
  e.g. `0.2.0~ubuntu22.04.1`. The suffixes sort in release order and below
  `0.2.0`, so upgrading Ubuntu upgrades the package.

`debian/rules` uses `vendor/` when it is present (with `GOPROXY=off`), keeps
Go's cache inside the build tree (Launchpad builders have no writable
`HOME`), finds `golang-1.25-go` in `/usr/lib/go-1.25`, and skips the plugin
when `debian/control` does not list it.

The Build workflow runs this for every release and then builds the binaries
from the `.dsc` in a clean container, like Launchpad; see [ci.md](ci.md).
All three were checked this way, offline, before the workflows existed.

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

Prepare release adds the `debian/changelog` entry, tags and builds; Publish
release uploads to the PPA. See [ci.md](ci.md#releasing).
