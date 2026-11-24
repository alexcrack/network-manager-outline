package nmvpn

import (
	"encoding/binary"
	"net/netip"
	"testing"

	"github.com/godbus/dbus/v5"
)

func TestIP4ToUint32MemoryLayout(t *testing.T) {
	a := netip.MustParseAddr("10.233.233.1")
	u := IP4ToUint32(a)
	var b [4]byte
	binary.NativeEndian.PutUint32(b[:], u)
	if b != [4]byte{10, 233, 233, 1} {
		t.Fatalf("in-memory bytes = %v, want address bytes", b)
	}
	if got := Uint32ToIP4(u); got != a {
		t.Fatalf("roundtrip = %s", got)
	}
	if IP4ToUint32(netip.MustParseAddr("::ffff:10.233.233.1")) != u {
		t.Fatal("4in6 not unmapped")
	}
}

func sig(t *testing.T, d map[string]dbus.Variant, key, want string) {
	t.Helper()
	v, ok := d[key]
	if !ok {
		t.Fatalf("key %q missing", key)
	}
	if got := v.Signature().String(); got != want {
		t.Fatalf("key %q signature = %s, want %s", key, got, want)
	}
}

func TestGenericConfigDict(t *testing.T) {
	d, err := GenericConfig{
		TunDev: "outline0", Gateway: netip.MustParseAddr("203.0.113.7"),
		MTU: 1500, HasIP4: true, HasIP6: true,
	}.Dict()
	if err != nil {
		t.Fatal(err)
	}
	sig(t, d, ConfigTunDev, "s")
	sig(t, d, ConfigExtGateway, "u")
	sig(t, d, ConfigMTU, "u")
	sig(t, d, ConfigHasIP4, "b")
	sig(t, d, ConfigHasIP6, "b")
	if _, ok := d[ConfigCanPersist]; ok {
		t.Fatal("unset can-persist should be omitted")
	}

	d, err = GenericConfig{Gateway: netip.MustParseAddr("2001:db8::1"), HasIP4: true}.Dict()
	if err != nil {
		t.Fatal(err)
	}
	sig(t, d, ConfigExtGateway, "ay")
}

func TestGenericConfigValidation(t *testing.T) {
	if _, err := (GenericConfig{HasIP4: true}).Dict(); err == nil {
		t.Fatal("missing gateway accepted")
	}
	if _, err := (GenericConfig{Gateway: netip.MustParseAddr("1.2.3.4")}).Dict(); err == nil {
		t.Fatal("no address family accepted")
	}
}

func TestIP4Dict(t *testing.T) {
	d, err := IPConfig{
		Address:      netip.MustParsePrefix("10.233.233.1/32"),
		DNS:          []netip.Addr{netip.MustParseAddr("1.1.1.1")},
		Routes:       []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")},
		Domains:      []string{"~."},
		NeverDefault: true,
	}.IP4Dict()
	if err != nil {
		t.Fatal(err)
	}
	sig(t, d, IPConfigAddress, "u")
	sig(t, d, IPConfigPrefix, "u")
	sig(t, d, IPConfigDNS, "au")
	sig(t, d, IPConfigRoutes, "aau")
	sig(t, d, IPConfigDomains, "as")
	sig(t, d, IPConfigNeverDefault, "b")
	if p := d[IPConfigPrefix].Value().(uint32); p != 32 {
		t.Fatalf("prefix = %d", p)
	}
	r := d[IPConfigRoutes].Value().([][]uint32)[0]
	if Uint32ToIP4(r[0]) != netip.MustParseAddr("10.0.0.0") || r[1] != 8 {
		t.Fatalf("route = %v", r)
	}
}

func TestIP6Dict(t *testing.T) {
	d, err := IPConfig{
		Address: netip.MustParsePrefix("fd00:233::1/128"),
		DNS:     []netip.Addr{netip.MustParseAddr("2606:4700:4700::1111")},
		Routes:  []netip.Prefix{netip.MustParsePrefix("2000::/3")},
	}.IP6Dict()
	if err != nil {
		t.Fatal(err)
	}
	sig(t, d, IPConfigAddress, "ay")
	sig(t, d, IPConfigDNS, "aay")
	sig(t, d, IPConfigRoutes, "a(ayuayu)")
}

func TestIPConfigFamilyMismatch(t *testing.T) {
	c := IPConfig{Address: netip.MustParsePrefix("10.0.0.1/32"), DNS: []netip.Addr{netip.MustParseAddr("::1")}}
	if _, err := c.IP4Dict(); err == nil {
		t.Fatal("IPv6 DNS accepted in IPv4 config")
	}
	if _, err := c.IP6Dict(); err == nil {
		t.Fatal("IPv4 address accepted in IPv6 config")
	}
}

func TestConnectionSettings(t *testing.T) {
	s := ConnectionSettings{
		"connection": {"id": dbus.MakeVariant("work")},
		"vpn": {
			"service-type": dbus.MakeVariant("org.freedesktop.NetworkManager.outline"),
			"data":         dbus.MakeVariant(map[string]string{"dns": "1.1.1.1"}),
			"secrets":      dbus.MakeVariant(map[string]string{"access-key": "ss://x"}),
		},
	}
	if s.ID() != "work" || s.ServiceType() != "org.freedesktop.NetworkManager.outline" {
		t.Fatal("id/service-type")
	}
	if s.VPNData()["dns"] != "1.1.1.1" || s.VPNSecrets()["access-key"] != "ss://x" {
		t.Fatal("data/secrets")
	}
	if (ConnectionSettings{}).VPNSecrets() == nil {
		t.Fatal("nil map for missing secrets")
	}
}

func TestToDBusError(t *testing.T) {
	if e := toDBusError(Errorf(ErrBadArguments, "x")); e.Name != ErrorPrefix+".BadArguments" {
		t.Fatal(e.Name)
	}
	if e := toDBusError(errString("boom")); e.Name != ErrorPrefix+".Failed" || e.Body[0] != "boom" {
		t.Fatal(e)
	}
}

type errString string

func (e errString) Error() string { return string(e) }
