package nmvpn

import "github.com/godbus/dbus/v5"

// ConnectionSettings is the a{sa{sv}} connection NM passes to Connect and
// NeedSecrets.
type ConnectionSettings map[string]map[string]dbus.Variant

// ServiceType returns vpn.service-type.
func (s ConnectionSettings) ServiceType() string {
	v, _ := s["vpn"]["service-type"].Value().(string)
	return v
}

// ID returns connection.id.
func (s ConnectionSettings) ID() string {
	v, _ := s["connection"]["id"].Value().(string)
	return v
}

// VPNData returns vpn.data (a{ss}); never nil.
func (s ConnectionSettings) VPNData() map[string]string { return s.stringDict("data") }

// VPNSecrets returns vpn.secrets (a{ss}); never nil.
func (s ConnectionSettings) VPNSecrets() map[string]string { return s.stringDict("secrets") }

func (s ConnectionSettings) stringDict(key string) map[string]string {
	if m, ok := s["vpn"][key].Value().(map[string]string); ok && m != nil {
		return m
	}
	return map[string]string{}
}
