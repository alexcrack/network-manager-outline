PREFIX     ?= /usr
LIBEXECDIR ?= $(PREFIX)/libexec
NMVPNDIR   ?= $(PREFIX)/lib/NetworkManager/VPN
DBUSDIR    ?= $(PREFIX)/share/dbus-1/system.d
DESTDIR    ?=

PLASMA_NM_VERSION ?= $(shell dpkg-query -W -f='$${Version}' plasma-nm 2>/dev/null | sed -E 's/^[0-9]+://; s/-.*//')

.PHONY: all service test vet plasma plasma-deps install install-service install-plasma clean

all: service

service:
	go build -trimpath -o bin/nm-outline-service ./cmd/nm-outline-service

test:
	go test ./...

vet:
	go vet ./...

plasma/third_party/plasma-nm:
	git clone --depth 1 --branch v$(PLASMA_NM_VERSION) https://invent.kde.org/plasma/plasma-nm.git $@

plasma: plasma/third_party/plasma-nm
	cmake -S plasma -B plasma/build -DCMAKE_INSTALL_PREFIX=$(PREFIX)
	cmake --build plasma/build

install: install-service

install-service: service
	install -Dm755 bin/nm-outline-service $(DESTDIR)$(LIBEXECDIR)/nm-outline-service
	sed 's|@LIBEXECDIR@|$(LIBEXECDIR)|g' data/nm-outline-service.name.in > bin/nm-outline-service.name
	install -Dm644 bin/nm-outline-service.name $(DESTDIR)$(NMVPNDIR)/nm-outline-service.name
	install -Dm644 data/nm-outline-service.conf $(DESTDIR)$(DBUSDIR)/nm-outline-service.conf

install-plasma: plasma
	DESTDIR=$(DESTDIR) cmake --install plasma/build

clean:
	rm -rf bin plasma/build
