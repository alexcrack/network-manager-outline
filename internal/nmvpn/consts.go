// Package nmvpn implements the NetworkManager VPN service plugin D-Bus
// interface (org.freedesktop.NetworkManager.VPN.Plugin) in pure Go, without
// libnm. It mirrors the behaviour of libnm's NMVpnServicePlugin.
//
// Names, values and D-Bus types are verified against NetworkManager 1.54.3:
//   - libnm/nm-vpn-dbus-interface.h            (constants)
//   - libnm/nm-errors.h                         (NMVpnPluginError)
//   - introspection/org.freedesktop.NetworkManager.VPN.Plugin.xml
//   - src/core/vpn/nm-vpn-connection.c          (how NM parses config dicts)
//   - src/libnm-client-impl/nm-vpn-service-plugin.c (state machine, timers)
//
// See docs/nm-vpn-dbus-protocol.md for details.
package nmvpn

const (
	PluginInterface = "org.freedesktop.NetworkManager.VPN.Plugin"  // NM_VPN_DBUS_PLUGIN_INTERFACE
	PluginPath      = "/org/freedesktop/NetworkManager/VPN/Plugin" // NM_VPN_DBUS_PLUGIN_PATH
)

// ServiceState is NMVpnServiceState.
type ServiceState uint32

const (
	StateUnknown  ServiceState = 0
	StateInit     ServiceState = 1
	StateShutdown ServiceState = 2
	StateStarting ServiceState = 3
	StateStarted  ServiceState = 4
	StateStopping ServiceState = 5
	StateStopped  ServiceState = 6
)

func (s ServiceState) String() string {
	switch s {
	case StateInit:
		return "init"
	case StateShutdown:
		return "shutdown"
	case StateStarting:
		return "starting"
	case StateStarted:
		return "started"
	case StateStopping:
		return "stopping"
	case StateStopped:
		return "stopped"
	}
	return "unknown"
}

// Failure is NMVpnPluginFailure, the argument of the Failure signal.
type Failure uint32

const (
	FailureLoginFailed   Failure = 0
	FailureConnectFailed Failure = 1
	FailureBadIPConfig   Failure = 2
)

// Keys of the generic Config signal dictionary (NM_VPN_PLUGIN_CONFIG_*).
// D-Bus types as parsed by NM are noted for each key.
const (
	ConfigTunDev     = "tundev"      // s
	ConfigProxyPAC   = "pac"         // s
	ConfigBanner     = "banner"      // s
	ConfigExtGateway = "gateway"     // u (IPv4, host byte order of the in-memory address) or ay (IPv6). REQUIRED.
	ConfigMTU        = "mtu"         // u
	ConfigHasIP4     = "has-ip4"     // b, missing = false
	ConfigHasIP6     = "has-ip6"     // b, missing = false
	ConfigCanPersist = "can-persist" // b
)

// Keys of the Ip4Config / Ip6Config signal dictionaries
// (NM_VPN_PLUGIN_IP4_CONFIG_* / NM_VPN_PLUGIN_IP6_CONFIG_*).
const (
	IPConfigIntGateway     = "internal-gateway" // u | ay
	IPConfigAddress        = "address"          // u | ay
	IPConfigPTP            = "ptp"              // u | ay
	IPConfigPrefix         = "prefix"           // u (default 24 for IPv4, 128 for IPv6)
	IPConfigDNS            = "dns"              // au | aay
	IPConfigNBNS           = "nbns"             // au (IPv4 only)
	IPConfigMSS            = "mss"              // u
	IPConfigDomain         = "domain"           // s
	IPConfigDomains        = "domains"          // as
	IPConfigRoutes         = "routes"           // aau [net, plen, gw, metric(, pref_src)] | a(ayuayu)
	IPConfigPreserveRoutes = "preserve-routes"  // b
	IPConfigNeverDefault   = "never-default"    // b, missing = false
)
