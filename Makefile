# Build as your user, install as root:
#
#   make && sudo make install
#
# Install targets never build: sudo's PATH usually has no `go`, and building
# as root would leave root-owned files in the tree and the Go cache.

PREFIX     ?= /usr
LIBEXECDIR ?= $(PREFIX)/libexec
NMVPNDIR   ?= $(PREFIX)/lib/NetworkManager/VPN
DBUSDIR    ?= $(PREFIX)/share/dbus-1/system.d
ICONDIR    ?= $(PREFIX)/share/icons/hicolor/scalable/apps
HICOLORDIR ?= $(PREFIX)/share/icons/hicolor
ICON_SIZES := 16 32 64 128
DESTDIR    ?=

GO  ?= go
BIN := bin/nm-outline-service

PLASMA_NM_VERSION ?= $(shell dpkg-query -W -f='$${Version}' plasma-nm 2>/dev/null | sed -E 's/^[0-9]+://; s/-.*//')

.PHONY: all service test vet icons plasma plasma-test deb install install-service install-plasma uninstall clean

all: service

service:
	$(GO) build -trimpath -o $(BIN) ./cmd/nm-outline-service

test:
	$(GO) test ./...

vet:
	$(GO) vet ./...

# All variants are also rendered as PNG in ICON_SIZES (committed, so building
# does not need inkscape). nm-outline.svg and its PNGs are installed
# (transparent). The -light/-dark variants (not installed) are for
# the README, where GitHub picks one by color scheme: the white gear sits on a
# dark blue badge, deeper for light pages and lighter for dark ones.
icons:
	python3 -I data/icons/make_icon.py > data/icons/nm-outline.svg
	python3 -I data/icons/make_icon.py --background '#123049' > data/icons/nm-outline-light.svg
	python3 -I data/icons/make_icon.py --background '#24405c' > data/icons/nm-outline-dark.svg
	@command -v inkscape >/dev/null || { echo "inkscape is needed to render the PNG icons" >&2; exit 1; }
	for v in "" -light -dark; do for s in $(ICON_SIZES); do \
	  inkscape data/icons/nm-outline$$v.svg --export-type=png --export-width=$$s --export-height=$$s \
	    --export-filename=data/icons/nm-outline$$v-$$s.png 2>/dev/null || exit 1; \
	done; done

plasma/third_party/plasma-nm:
	git clone --depth 1 --branch v$(PLASMA_NM_VERSION) https://invent.kde.org/plasma/plasma-nm.git $@

plasma: plasma/third_party/plasma-nm
	cmake -S plasma -B plasma/build -DCMAKE_INSTALL_PREFIX=$(PREFIX) -DBUILD_TESTING=ON
	cmake --build plasma/build

# Loads the built plugin like Plasma does and exercises it offscreen.
plasma-test: plasma
	cd plasma/build && QT_QPA_PLATFORM=offscreen ctest --output-on-failure

# Debian packages (network-manager-outline, plasma-nm-outline) into dist/.
# Needs debhelper and the build dependencies in debian/control
# (`dpkg-checkbuilddeps` lists what is missing).
DEB_VERSION = $(shell dpkg-parsechangelog -S Version 2>/dev/null)

deb: plasma/third_party/plasma-nm
	dpkg-buildpackage -us -uc -b
	mkdir -p dist
	mv ../network-manager-outline*_$(DEB_VERSION)_* ../plasma-nm-outline*_$(DEB_VERSION)_* dist/
	@ls -1 dist/*.deb

install: install-service

install-service:
	@test -x $(BIN) || { echo "$(BIN) not built; run 'make' as your user first" >&2; exit 1; }
	@stale=$$(find cmd internal go.mod go.sum -newer $(BIN) -print -quit); \
	  test -z "$$stale" || echo "warning: $$stale is newer than $(BIN); run 'make' to rebuild" >&2
	install -Dm755 $(BIN) $(DESTDIR)$(LIBEXECDIR)/nm-outline-service
	install -d $(DESTDIR)$(NMVPNDIR)
	sed 's|@LIBEXECDIR@|$(LIBEXECDIR)|g' data/nm-outline-service.name.in > $(DESTDIR)$(NMVPNDIR)/nm-outline-service.name
	chmod 644 $(DESTDIR)$(NMVPNDIR)/nm-outline-service.name
	install -Dm644 data/nm-outline-service.conf $(DESTDIR)$(DBUSDIR)/nm-outline-service.conf
	install -Dm644 data/icons/nm-outline.svg $(DESTDIR)$(ICONDIR)/nm-outline.svg
	for s in $(ICON_SIZES); do \
	  install -Dm644 data/icons/nm-outline-$$s.png $(DESTDIR)$(HICOLORDIR)/$${s}x$${s}/apps/nm-outline.png || exit 1; \
	done
	@echo "Installed. On first install, run: sudo systemctl restart NetworkManager"

install-plasma:
	@test -d plasma/build || { echo "Plasma plugin not built; run 'make plasma' as your user first" >&2; exit 1; }
	DESTDIR=$(DESTDIR) cmake --install plasma/build

uninstall:
	rm -f $(DESTDIR)$(LIBEXECDIR)/nm-outline-service
	rm -f $(DESTDIR)$(PREFIX)/lib/*/qt6/plugins/plasma/network/vpn/plasmanetworkmanagement_outlineui.so
	rm -f $(DESTDIR)$(NMVPNDIR)/nm-outline-service.name
	rm -f $(DESTDIR)$(DBUSDIR)/nm-outline-service.conf
	rm -f $(DESTDIR)$(ICONDIR)/nm-outline.svg
	for s in $(ICON_SIZES); do rm -f $(DESTDIR)$(HICOLORDIR)/$${s}x$${s}/apps/nm-outline.png; done

clean:
	rm -rf bin plasma/build obj-plasma dist
