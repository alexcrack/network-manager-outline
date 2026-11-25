// Package outline glues outline-sdk to the nmvpn plugin interface.
package outline

// Keys stored in the NM connection profile.
//
//	vpn.data:    dns, ipv6, never-default, transport-prefix
//	vpn.secrets: access-key
const (
	ServiceName = "org.freedesktop.NetworkManager.outline"

	KeyDNS             = "dns"              // comma-separated DNS servers pushed to NM (default 1.1.1.1)
	KeyIPv6            = "ipv6"             // "tunnel" (default) | "off"
	KeyNeverDefault    = "never-default"    // "yes" to not install a default route
	KeyTransportPrefix = "transport-prefix" // optional configurl prefix for TCP, e.g. "split:2"

	SecretAccessKey = "access-key" // ss://..., or a dynamic ssconf://... / https://... key
)
