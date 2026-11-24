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
DESTDIR    ?=

GO  ?= go
BIN := bin/nm-outline-service

PLASMA_NM_VERSION ?= $(shell dpkg-query -W -f='$${Version}' plasma-nm 2>/dev/null | sed -E 's/^[0-9]+://; s/-.*//')

.PHONY: all service test vet icons plasma plasma-test install install-service install-plasma uninstall clean

all: service

service:
	$(GO) build -trimpath -o $(BIN) ./cmd/nm-outline-service

test:
	$(GO) test ./...

vet:
	$(GO) vet ./...

# outline-vpn.svg is installed (transparent). The -light/-dark variants are for
# the README, where GitHub picks one by color scheme: the white gear sits on a
# dark blue badge, deeper for light pages and lighter for dark ones.
icons:
	python3 -I data/icons/make_icon.py > data/icons/outline-vpn.svg
	python3 -I data/icons/make_icon.py --background '#123049' > data/icons/outline-vpn-light.svg
	python3 -I data/icons/make_icon.py --background '#24405c' > data/icons/outline-vpn-dark.svg

plasma/third_party/plasma-nm:
	git clone --depth 1 --branch v$(PLASMA_NM_VERSION) https://invent.kde.org/plasma/plasma-nm.git $@

plasma: plasma/third_party/plasma-nm
	cmake -S plasma -B plasma/build -DCMAKE_INSTALL_PREFIX=$(PREFIX) -DBUILD_TESTING=ON
	cmake --build plasma/build

# Loads the built plugin like Plasma does and exercises it offscreen.
plasma-test: plasma
	cd plasma/build && QT_QPA_PLATFORM=offscreen ctest --output-on-failure

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
	install -Dm644 data/icons/outline-vpn.svg $(DESTDIR)$(ICONDIR)/outline-vpn.svg
	@echo "Installed. On first install, run: sudo systemctl restart NetworkManager"

install-plasma:
	@test -d plasma/build || { echo "Plasma plugin not built; run 'make plasma' as your user first" >&2; exit 1; }
	DESTDIR=$(DESTDIR) cmake --install plasma/build

uninstall:
	rm -f $(DESTDIR)$(LIBEXECDIR)/nm-outline-service
	rm -f $(DESTDIR)$(PREFIX)/lib/*/qt6/plugins/plasma/network/vpn/plasmanetworkmanagement_outlineui.so
	rm -f $(DESTDIR)$(NMVPNDIR)/nm-outline-service.name
	rm -f $(DESTDIR)$(DBUSDIR)/nm-outline-service.conf
	rm -f $(DESTDIR)$(ICONDIR)/outline-vpn.svg

clean:
	rm -rf bin plasma/build
