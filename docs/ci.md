# CI and releases

Four GitHub Actions workflows in `.github/workflows/`. Only Check PR runs by
itself; the others are started by hand (Actions → workflow → Run workflow),
like in MediaGun.

| Workflow | Runs | Does |
|---|---|---|
| [Check PR](../.github/workflows/check-pr.yaml) | on pull requests into `master` and `release/**` | lints the Go code, runs all tests |
| [Build](../.github/workflows/build.yaml) | by hand, on any branch; called by Prepare release | builds the packages for Ubuntu 22.04, 24.04 and 26.04 |
| [Prepare release](../.github/workflows/prepare-release.yaml) | by hand, on a `release/A.B` branch | version, notes, changelog, tag `vA.B.C`, build, draft GitHub release |
| [Publish release](../.github/workflows/publish-release.yaml) | by hand, by a release manager | signs and uploads to the Launchpad PPA, publishes the draft |

## Check PR

- **Lint** (`ubuntu-24.04`): golangci-lint v2.15.0 with `.golangci.yml`: the
  standard linters (errcheck, govet, ineffassign, staticcheck, unused) and
  gofmt. Unchecked errors from `Close` and similar are allowed
  (`std-error-handling`). Run the same locally with `make lint`.
- **Test** (`ubuntu:26.04` container, the only release with Plasma 6): the Go
  tests with the race detector, including the D-Bus tests against a private
  dbus-daemon, and the Plasma plugin's offscreen tests (`make plasma-test`).
  The TUN test is skipped (containers have no `/dev/net/tun`).

## Build

One job per Ubuntu release, each in a clean container of that release:

| Release | Container | Packages | Go |
|---|---|---|---|
| 22.04 jammy | `ubuntu:22.04` | `network-manager-outline` | `golang-1.25-go` (jammy-updates) |
| 24.04 noble | `ubuntu:24.04` | `network-manager-outline` | `golang-1.25-go` (noble-updates) |
| 26.04 resolute | `ubuntu:26.04` | `network-manager-outline`, `plasma-nm-outline` | `golang-go` (1.26) |

22.04 and 24.04 have Plasma 5 (plasma-nm 5.24 and 5.27), and the plugin is
for Plasma 6, so they get the service only (usable with nmcli).

Each job:

1. makes the source package with `tools/deb-series.py` (see
   [packaging.md](packaging.md#packages-for-ubuntu-releases-and-the-ppa));
2. unpacks it, installs exactly its build dependencies (`apt-get build-dep`)
   and builds the binaries from it, running the tests, as Launchpad does;
3. keeps everything as the artifact `deb-<series>`: `.deb`, debug symbols
   (`.ddeb`), and the source package (`.dsc`, `.tar.xz`, `_source.changes`,
   `_source.buildinfo`).

Started by hand, versions get a `+git<date>.<commit>` suffix, e.g.
`0.2.0+git202611251030.ab12cd3~ubuntu24.04.1`, so test builds never look like
releases.

## Releasing

Branches: `master` is the development branch. A release line `release/A.B`
is created by hand from `master` (or from where the line should start):

```sh
git switch -c release/0.2 master
git push -u origin release/0.2
```

Then:

1. **Prepare release** on `release/0.2` (optionally a dry run first, which only
   shows the version and the notes):
   - the version: `v0.2.0` for the branch's first release, then the next patch
     (`v0.2.1`, ...); or the version entered, which must match the branch;
   - the notes: the commit subjects since the previous release, for `A.B.0`
     since this line split from the previous release's;
   - commits the `debian/changelog` entry ("Release v0.2.0") to the branch,
     tags it, builds the packages (Build) and creates a **draft** GitHub
     release with all packages, the source packages and `SHA256SUMS`.
2. Read the draft release.
3. **Publish release** with the tag, as a release manager. Its dry run (the
   default) checks the files and signs them; run it again without dry run to
   upload the source packages to the PPA and publish the GitHub release.
   Launchpad then builds the packages for each Ubuntu release (watch the PPA's
   "View package details" page); they appear in the PPA after an hour or so.

Fixes for a released line: merge them into `release/A.B` (cherry-pick from
`master`), then Prepare release again for the next patch.

## One-time setup

### Launchpad

1. **PPA**: on <https://launchpad.net>, your profile → "Create a new PPA",
   e.g. `network-manager-outline`. Its default "Ubuntu dependencies" (release,
   security and updates) are needed: Go 1.25 for 22.04 and 24.04 is in the
   updates pocket.
2. **Signing key**: a dedicated GPG key, with an encryption subkey (Launchpad
   confirms a key by sending encrypted mail) and a passphrase:

   ```sh
   gpg --quick-gen-key "Your Name <you@example.com>" default default 2y
   gpg --list-secret-keys --keyid-format long          # note the fingerprint
   gpg --keyserver keyserver.ubuntu.com --send-keys FINGERPRINT
   ```

   Add it on Launchpad: your profile → "OpenPGP keys" → import the
   fingerprint, then decrypt and follow the confirmation mail. If Launchpad
   asks, sign the Ubuntu Code of Conduct.

### GitHub repository settings

Settings → Secrets and variables → Actions:

| Kind | Name | Value |
|---|---|---|
| Secret | `LAUNCHPAD_GPG_PRIVATE_KEY` | `gpg --armor --export-secret-keys FINGERPRINT` |
| Secret | `LAUNCHPAD_GPG_PASSPHRASE` | the key's passphrase |
| Variable | `PPA` | `owner/name`, e.g. `alexcrack/network-manager-outline` |
| Variable | `RELEASE_MANAGERS` | GitHub logins allowed to publish, space-separated |

Prepare release pushes the changelog commit and the tag as
`github-actions[bot]`: if `release/**` branches are protected, allow GitHub
Actions to push to them.

## Notes and limits

- **plasma-nm pinning on 26.04.** The source package bundles the headers of
  the plasma-nm version the archive offers when it is built and pins its
  build dependency to that version. If Ubuntu updates plasma-nm before
  Launchpad builds it, the build waits for a dependency that no longer
  exists ("Dependency wait"); after a plasma-nm update the plugin must be
  rebuilt anyway, so make a new patch release.
- **amd64 only.** Launchpad builds the architectures enabled for the PPA;
  the packages are `linux-any`, but only amd64 is tested.
- **Asset names.** GitHub replaces `~` in release asset names with `.`, so
  `network-manager-outline_0.2.0~ubuntu24.04.1_amd64.deb` is published as
  `network-manager-outline_0.2.0.ubuntu24.04.1_amd64.deb`. Prepare release
  names the files that way before writing `SHA256SUMS`, so it matches the
  downloads; Publish release restores the Debian names from each
  `_source.changes` (which lists every file's name and SHA-256) before
  signing and uploading.
- **Upload over FTP.** dput uploads to `ppa.launchpad.net` over FTP, which
  GitHub-hosted runners allow.
