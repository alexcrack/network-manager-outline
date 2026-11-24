// Package nmvpn implements the NetworkManager VPN plugin D-Bus interface
// (org.freedesktop.NetworkManager.VPN.Plugin) in pure Go, without libnm.
//
// Constants mirror libnm's nm-vpn-dbus-interface.h.
// TODO(phase1): verify every name/value against libnm-dev headers.
package nmvpn

const (
	PluginInterface = "org.freedesktop.NetworkManager.VPN.Plugin"
	PluginPath      = "/org/freedesktop/NetworkManager/VPN/Plugin"
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

// Failure is NMVpnPluginFailure.
type Failure uint32

const (
	FailureLoginFailed   Failure = 0
	FailureConnectFailed Failure = 1
	FailureBadIPConfig   Failure = 2
)

// Keys of the generic Config signal dictionary.
const (
	ConfigTunDev     = "tundev"
	ConfigExtGateway = "gateway" // VPN server address; NM adds a host route to it via the underlying device
	ConfigMTU        = "mtu"
	ConfigHasIP4     = "has-ip4"
	ConfigHasIP6     = "has-ip6"
	ConfigCanPersist = "can-persist"
)

// Keys of the Ip4Config / Ip6Config signal dictionaries.
const (
	IPConfigAddress      = "address"
	IPConfigPrefix       = "prefix"
	IPConfigIntGateway   = "internal-gateway"
	IPConfigDNS          = "dns"
	IPConfigDomains      = "domains"
	IPConfigRoutes       = "routes"
	IPConfigNeverDefault = "never-default"
	IPConfigPreserveRts  = "preserve-routes"
	IPConfigMTU          = "mtu"
)
