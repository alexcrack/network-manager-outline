#!/usr/bin/env python3
"""Builds the Debian source package (and, unless --source-only, the binary
packages) for one Ubuntu release, without network access at build time.

    tools/deb-series.py --series noble            # -> dist/noble/
    tools/deb-series.py --series resolute --snapshot --source-only

It copies the working tree (tracked and untracked, not ignored files) into
build/deb/<series>/, then in that copy:

- vendors the Go modules (vendor/), so the build needs no network;
- adds the C sources of cgo modules that `go mod vendor` leaves out (it only
  copies directories with Go files; go-tun2socks keeps lwIP in others);
- for releases with Plasma 6, bundles the plasma-nm editor headers of the
  plasma-nm version apt would install (the archive's candidate, which is what
  Launchpad builds against) and pins the build to that version;
  for the others, drops plasma-nm-outline and its build dependencies;
- uses golang-1.25-go where the default Go is older (Ubuntu 22.04, 24.04);
- adds a changelog entry for the release: <version>~ubuntu<NN.NN>.1, which
  sorts below <version> and in release order, so upgrading Ubuntu upgrades the
  package.

Needs dpkg-dev, debhelper and Go (and git for --snapshot); the binary build
needs the build dependencies of the generated debian/control.
"""
import argparse
import email.utils
import os
import re
import shutil
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
SOURCE = "network-manager-outline"

# Ubuntu releases: version number, Go build dependency, Plasma 6 available.
SERIES = {
    "jammy": ("22.04", "golang-1.25-go", False),
    "noble": ("24.04", "golang-1.25-go", False),
    "resolute": ("26.04", "golang-go (>= 2:1.25~)", True),
}

# Build dependencies only the Plasma plugin needs.
PLASMA_BUILD_DEPS = re.compile(
    r"^(cmake|extra-cmake-modules|pkgconf|libnm-dev|qt6-base-dev|libkf6\S*|plasma-nm)\b")


def run(cmd, **kw):
    print("+", " ".join(str(c) for c in cmd), flush=True)
    return subprocess.run(cmd, check=True, **kw)


def output(cmd, **kw):
    return subprocess.run(cmd, check=True, capture_output=True, text=True, **kw).stdout.strip()


def go_env():
    """Environment with a Go >= 1.25 first in PATH (golang-1.25-go is outside PATH)."""
    env = dict(os.environ, GOTOOLCHAIN="local", GOFLAGS="")
    go_dirs = sorted(Path("/usr/lib").glob("go-1.2[5-9]"))
    if go_dirs:
        env["PATH"] = f"{go_dirs[-1]}/bin:{env['PATH']}"
    return env


def plasma_nm_version():
    """The plasma-nm upstream version apt would install, e.g. ("4:6.6.7", "4:6.6.8")."""
    policy = subprocess.run(["apt-cache", "policy", "plasma-nm"], capture_output=True, text=True,
                            env=dict(os.environ, LC_ALL="C")).stdout
    m = re.search(r"^\s*Candidate:\s*(\S+)", policy, re.M)
    if not m or m.group(1) == "(none)":
        sys.exit("plasma-nm is not available from apt here; the Plasma plugin can't be built")
    upstream = re.sub(r"-[^-]*$", "", m.group(1))  # drop the Debian revision
    parts = upstream.split(".")
    parts[-1] = str(int(parts[-1]) + 1)
    return upstream, ".".join(parts)


def copy_tree(dest):
    files = output(["git", "ls-files", "-co", "--exclude-standard", "-z"], cwd=ROOT).split("\0")
    for rel in filter(None, files):
        if rel.startswith(".github/"):
            continue
        src = ROOT / rel
        if not src.is_file():  # deleted but not yet committed
            continue
        (dest / rel).parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(src, dest / rel)


def bundle_plasma_headers(dest):
    """Copies the editor headers (and licenses) of the plasma-nm version the
    package is pinned to into the tree."""
    upstream, _ = plasma_nm_version()
    tag = "v" + upstream.split(":")[-1]  # drop the epoch
    src = ROOT / "build/plasma-nm" / tag
    if not (src / "libs/editor/vpnuiplugin.h").exists():
        shutil.rmtree(src, ignore_errors=True)
        run(["git", "clone", "-q", "--depth", "1", "--branch", tag,
             "https://invent.kde.org/plasma/plasma-nm.git", str(src)])
    out = dest / "plasma/third_party/plasma-nm"
    for f in (src / "libs/editor").rglob("*.h"):
        (out / f.relative_to(src)).parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(f, out / f.relative_to(src))
    if (src / "LICENSES").is_dir():
        shutil.copytree(src / "LICENSES", out / "LICENSES", dirs_exist_ok=True)


C_SOURCES = {".c", ".h", ".S", ".s", ".cc", ".cpp", ".hh", ".hpp", ".inc"}


def vendor_cgo_sources(vendor, env):
    """Adds the C sources `go mod vendor` leaves out of cgo modules."""
    modules = re.findall(r"^# (\S+) (\S+)", (vendor / "modules.txt").read_text(), re.M)
    for mod, ver in modules:
        vdir = vendor / mod
        if not vdir.is_dir() or not any('import "C"' in f.read_text(errors="ignore") for f in vdir.rglob("*.go")):
            continue
        mdir = Path(output(["go", "list", "-m", "-f", "{{.Dir}}", f"{mod}@{ver}"], cwd=ROOT, env=env))
        added = 0
        for f in mdir.rglob("*"):
            rel = f.relative_to(mdir)
            if (f.is_file() and f.suffix in C_SOURCES and not (vdir / rel).exists()
                    and not any(p in ("testdata", ".git") or p.startswith(("_", ".")) for p in rel.parts[:-1])):
                (vdir / rel).parent.mkdir(parents=True, exist_ok=True)
                shutil.copy2(f, vdir / rel)
                (vdir / rel).chmod(0o644)
                added += 1
        print(f"vendor: added {added} C source files of {mod}")


def edit_control(path, go_dep, plasma):
    paragraphs = path.read_text().strip().split("\n\n")
    out = []
    for para in paragraphs:
        if not plasma and re.search(r"^Package: plasma-nm-outline$", para, re.M):
            continue
        lines = []
        in_build_deps = False
        for line in para.split("\n"):
            if re.match(r"^[A-Za-z-]+:", line):
                in_build_deps = line.startswith("Build-Depends:")
            if in_build_deps and line.startswith(" "):
                dep = line.strip()
                if dep.startswith("golang-go"):
                    line = f" {go_dep},"
                elif PLASMA_BUILD_DEPS.match(dep):
                    if not plasma:
                        continue
                    if dep.startswith("plasma-nm"):
                        upstream, nxt = plasma_nm_version()
                        line = f" plasma-nm (>= {upstream}), plasma-nm (<< {nxt}~),"
            lines.append(line)
        para = "\n".join(lines)
        if not plasma:  # the Suggests field of the service names only the plugin
            para = re.sub(r"^Suggests:\n plasma-nm-outline,\n", "", para, flags=re.M)
        out.append(para)
    path.write_text("\n\n".join(out) + "\n")


def add_changelog_entry(path, version, series, number):
    text = path.read_text()
    maintainer = re.search(r"^ -- (.+?)  ", text, re.M).group(1)
    entry = (f"{SOURCE} ({version}) {series}; urgency=medium\n\n"
             f"  * Build for Ubuntu {number}.\n\n"
             f" -- {maintainer}  {email.utils.formatdate(localtime=True)}\n\n")
    path.write_text(entry + text)


def main():
    ap = argparse.ArgumentParser(description=__doc__.split("\n\n")[0])
    ap.add_argument("--series", required=True, choices=sorted(SERIES))
    ap.add_argument("--snapshot", action="store_true",
                    help="not a release: append +git<date>.<commit> to the version")
    ap.add_argument("--source-only", action="store_true", help="build only the source package")
    ap.add_argument("--out", type=Path, help="where to put the results (default dist/<series>)")
    args = ap.parse_args()

    number, go_dep, plasma = SERIES[args.series]
    base = output(["dpkg-parsechangelog", "-S", "Version"], cwd=ROOT)
    if args.snapshot:
        stamp = output(["git", "log", "-1", "--format=%cd.%h", "--date=format:%Y%m%d%H%M"], cwd=ROOT)
        base += f"+git{stamp}"
    version = f"{base}~ubuntu{number}.1"

    work = ROOT / "build/deb" / args.series
    tree = work / f"{SOURCE}-{version}"
    shutil.rmtree(work, ignore_errors=True)
    tree.mkdir(parents=True)

    copy_tree(tree)
    run(["go", "mod", "vendor", "-o", str(tree / "vendor")], cwd=ROOT, env=go_env())
    vendor_cgo_sources(tree / "vendor", go_env())
    if plasma:
        bundle_plasma_headers(tree)
    edit_control(tree / "debian/control", go_dep, plasma)
    add_changelog_entry(tree / "debian/changelog", version, args.series, number)

    # Source package first (no build dependencies needed), then the binaries.
    run(["dpkg-buildpackage", "-S", "-d", "-us", "-uc"], cwd=tree)
    if not args.source_only:
        run(["dpkg-buildpackage", "-b", "-us", "-uc"], cwd=tree)

    out = args.out or ROOT / "dist" / args.series
    out.mkdir(parents=True, exist_ok=True)
    for f in work.iterdir():
        if f.is_file():
            shutil.move(str(f), out / f.name)
    print(f"\n{version}:")
    for f in sorted(out.iterdir()):
        print(f"  {f.relative_to(ROOT) if f.is_relative_to(ROOT) else f}")


if __name__ == "__main__":
    main()
