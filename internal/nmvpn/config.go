package nmvpn

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net/netip"

	"github.com/godbus/dbus/v5"
)

// GenericConfig is the payload of the Config signal.
type GenericConfig struct {
	// TunDev is the interface carrying VPN traffic.
	TunDev string
	// Gateway is the external VPN server address. NM requires it and installs
	// a host route to it via the underlying device so tunnel traffic does not
	// loop back into the tunnel.
	Gateway    netip.Addr
	MTU        uint32
	HasIP4     bool
	HasIP6     bool
	CanPersist bool
	Banner     string
	ProxyPAC   string
}

// Dict encodes the config for D-Bus.
func (c GenericConfig) Dict() (map[string]dbus.Variant, error) {
	if !c.Gateway.IsValid() || c.Gateway.IsUnspecified() {
		return nil, errors.New("config: external gateway is required")
	}
	if !c.HasIP4 && !c.HasIP6 {
		return nil, errors.New("config: at least one of HasIP4/HasIP6 must be set")
	}
	d := map[string]dbus.Variant{
		ConfigExtGateway: addrVariant(c.Gateway),
		ConfigHasIP4:     dbus.MakeVariant(c.HasIP4),
		ConfigHasIP6:     dbus.MakeVariant(c.HasIP6),
	}
	if c.TunDev != "" {
		d[ConfigTunDev] = dbus.MakeVariant(c.TunDev)
	}
	if c.MTU != 0 {
		d[ConfigMTU] = dbus.MakeVariant(c.MTU)
	}
	if c.CanPersist {
		d[ConfigCanPersist] = dbus.MakeVariant(true)
	}
	if c.Banner != "" {
		d[ConfigBanner] = dbus.MakeVariant(c.Banner)
	}
	if c.ProxyPAC != "" {
		d[ConfigProxyPAC] = dbus.MakeVariant(c.ProxyPAC)
	}
	return d, nil
}

// IPConfig is the payload of the Ip4Config or Ip6Config signal. All addresses
// must belong to the same family; use IP4Dict or IP6Dict accordingly.
type IPConfig struct {
	// Address is the local tunnel address and prefix length.
	Address         netip.Prefix
	InternalGateway netip.Addr
	PTP             netip.Addr
	DNS             []netip.Addr
	Domains         []string
	// Routes are extra routes through the tunnel (gateway: none).
	Routes         []netip.Prefix
	NeverDefault   bool
	PreserveRoutes bool
	MSS            uint32
}

// IP4Dict encodes the config for the Ip4Config signal.
func (c IPConfig) IP4Dict() (map[string]dbus.Variant, error) {
	if err := c.checkFamily(true); err != nil {
		return nil, err
	}
	d := c.common()
	if len(c.DNS) > 0 {
		dns := make([]uint32, len(c.DNS))
		for i, a := range c.DNS {
			dns[i] = IP4ToUint32(a)
		}
		d[IPConfigDNS] = dbus.MakeVariant(dns)
	}
	if len(c.Routes) > 0 {
		routes := make([][]uint32, len(c.Routes))
		for i, r := range c.Routes {
			r = r.Masked()
			routes[i] = []uint32{IP4ToUint32(r.Addr()), uint32(r.Bits()), 0, 0}
		}
		d[IPConfigRoutes] = dbus.MakeVariant(routes)
	}
	return d, nil
}

// ip6Route is the a(ayuayu) element: dest, prefix, next hop, metric.
type ip6Route struct {
	Dest    []byte
	Prefix  uint32
	NextHop []byte
	Metric  uint32
}

// IP6Dict encodes the config for the Ip6Config signal.
func (c IPConfig) IP6Dict() (map[string]dbus.Variant, error) {
	if err := c.checkFamily(false); err != nil {
		return nil, err
	}
	d := c.common()
	if len(c.DNS) > 0 {
		dns := make([][]byte, len(c.DNS))
		for i, a := range c.DNS {
			dns[i] = a.AsSlice()
		}
		d[IPConfigDNS] = dbus.MakeVariant(dns)
	}
	if len(c.Routes) > 0 {
		routes := make([]ip6Route, len(c.Routes))
		for i, r := range c.Routes {
			r = r.Masked()
			routes[i] = ip6Route{r.Addr().AsSlice(), uint32(r.Bits()), netip.IPv6Unspecified().AsSlice(), 0}
		}
		d[IPConfigRoutes] = dbus.MakeVariant(routes)
	}
	return d, nil
}

func (c IPConfig) common() map[string]dbus.Variant {
	d := map[string]dbus.Variant{}
	if c.Address.IsValid() {
		d[IPConfigAddress] = addrVariant(c.Address.Addr())
		d[IPConfigPrefix] = dbus.MakeVariant(uint32(c.Address.Bits()))
	}
	if c.InternalGateway.IsValid() {
		d[IPConfigIntGateway] = addrVariant(c.InternalGateway)
	}
	if c.PTP.IsValid() {
		d[IPConfigPTP] = addrVariant(c.PTP)
	}
	if len(c.Domains) > 0 {
		d[IPConfigDomains] = dbus.MakeVariant(c.Domains)
	}
	if c.NeverDefault {
		d[IPConfigNeverDefault] = dbus.MakeVariant(true)
	}
	if c.PreserveRoutes {
		d[IPConfigPreserveRoutes] = dbus.MakeVariant(true)
	}
	if c.MSS != 0 {
		d[IPConfigMSS] = dbus.MakeVariant(c.MSS)
	}
	return d
}

func (c IPConfig) checkFamily(v4 bool) error {
	check := func(what string, a netip.Addr) error {
		if a.IsValid() && a.Unmap().Is4() != v4 {
			return fmt.Errorf("ip config: %s %s has wrong address family", what, a)
		}
		return nil
	}
	if c.Address.IsValid() {
		if err := check("address", c.Address.Addr()); err != nil {
			return err
		}
	}
	for _, a := range append([]netip.Addr{c.InternalGateway, c.PTP}, c.DNS...) {
		if err := check("address", a); err != nil {
			return err
		}
	}
	for _, r := range c.Routes {
		if err := check("route", r.Addr()); err != nil {
			return err
		}
	}
	return nil
}

// IP4ToUint32 encodes an IPv4 address the way NM expects: a native-endian
// uint32 whose in-memory bytes are the address bytes (NM reads the "u" and
// writes it back with unaligned_write_ne32).
func IP4ToUint32(a netip.Addr) uint32 {
	b := a.Unmap().As4()
	return binary.NativeEndian.Uint32(b[:])
}

// Uint32ToIP4 is the inverse of IP4ToUint32.
func Uint32ToIP4(u uint32) netip.Addr {
	var b [4]byte
	binary.NativeEndian.PutUint32(b[:], u)
	return netip.AddrFrom4(b)
}

func addrVariant(a netip.Addr) dbus.Variant {
	if a.Unmap().Is4() {
		return dbus.MakeVariant(IP4ToUint32(a))
	}
	return dbus.MakeVariant(a.AsSlice())
}
