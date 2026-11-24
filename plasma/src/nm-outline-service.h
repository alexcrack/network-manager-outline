// Setting names shared with the service. Keep in sync with
// internal/outline/keys.go.
#pragma once

#define NM_DBUS_SERVICE_OUTLINE "org.freedesktop.NetworkManager.outline"

#define NM_OUTLINE_KEY_DNS "dns" // comma-separated IPs, default 1.1.1.1
#define NM_OUTLINE_KEY_IPV6 "ipv6" // "tunnel" (default) | "off"
#define NM_OUTLINE_KEY_NEVER_DEFAULT "never-default" // "yes" | "no"
#define NM_OUTLINE_KEY_TRANSPORT_PREFIX "transport-prefix" // configurl prefix, TCP only

#define NM_OUTLINE_SECRET_ACCESS_KEY "access-key" // ss://... or ssconf://...
#define NM_OUTLINE_SECRET_ACCESS_KEY_FLAGS "access-key-flags" // NMSettingSecretFlags, in vpn.data

#define NM_OUTLINE_IPV6_TUNNEL "tunnel"
#define NM_OUTLINE_IPV6_OFF "off"
