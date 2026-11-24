package outline

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"golang.getoutline.org/sdk/transport/shadowsocks"

	"network-manager-outline/internal/nmvpn"
)

// AccessKey is a parsed static Outline (Shadowsocks) access key.
type AccessKey struct {
	Method   string
	Password string
	Host     string // hostname or IP literal, without brackets
	Port     uint16
	Prefix   string // optional salt prefix (?prefix=)
	Name     string // optional display name (#fragment)
}

// ParseAccessKey parses an ss:// key in SIP002 form
// (ss://base64url(method:password)@host:port/?prefix=...#name, userinfo
// optionally plain) or legacy form (ss://base64(method:password@host:port)#name),
// following outline-sdk's configurl parser. Unknown query parameters such as
// outline=1 are ignored. It does no network I/O.
//
// ssconf:// (dynamic) keys are resolved separately (phase 2).
func ParseAccessKey(key string) (*AccessKey, error) {
	key = strings.TrimSpace(key)
	u, err := url.Parse(key)
	if err != nil {
		return nil, errors.New("access key is not a valid URL")
	}
	switch strings.ToLower(u.Scheme) {
	case "ss":
	case "ssconf":
		return nil, errors.New("dynamic ssconf:// access keys are not supported yet")
	default:
		return nil, fmt.Errorf("access key must start with ss://, got %q", u.Scheme+"://")
	}
	if u.Host == "" {
		return nil, errors.New("access key has no server")
	}

	ak := &AccessKey{Name: u.Fragment}
	var userinfo, hostport string
	var query url.Values
	if u.User != nil {
		// SIP002: userinfo is base64url (or std base64, or plain) "method:password".
		userinfo = decodeBase64Lenient(u.User.String())
		hostport, query = u.Host, u.Query()
	} else {
		// Legacy: the whole host part is base64("method:password@host:port").
		decoded, err := base64.URLEncoding.WithPadding(base64.NoPadding).DecodeString(strings.TrimRight(u.Host, "="))
		if err != nil {
			return nil, errors.New("access key has no credentials")
		}
		at := strings.LastIndex(string(decoded), "@")
		if at < 0 {
			return nil, errors.New("access key has no credentials")
		}
		userinfo, hostport = string(decoded[:at]), string(decoded[at+1:])
		query = u.Query()
	}

	var found bool
	ak.Method, ak.Password, found = strings.Cut(userinfo, ":")
	if !found || ak.Method == "" {
		return nil, errors.New("access key credentials must be method:password")
	}
	ak.Method = strings.ToLower(ak.Method)
	if _, err := shadowsocks.NewEncryptionKey(ak.Method, ak.Password); err != nil {
		return nil, fmt.Errorf("access key uses an unsupported cipher %q", ak.Method)
	}

	host, port, err := net.SplitHostPort(hostport)
	if err != nil {
		return nil, fmt.Errorf("access key server %q: %w", hostport, err)
	}
	p, err := strconv.ParseUint(port, 10, 16)
	if err != nil || p == 0 {
		return nil, fmt.Errorf("access key has invalid port %q", port)
	}
	if host == "" {
		return nil, errors.New("access key has no server host")
	}
	ak.Host, ak.Port = host, uint16(p)
	ak.Prefix = query.Get("prefix")
	return ak, nil
}

// decodeBase64Lenient decodes base64url or standard base64 (padded or not),
// returning s unchanged if it is not base64 or does not decode to method:password.
func decodeBase64Lenient(s string) string {
	for _, enc := range []*base64.Encoding{base64.RawURLEncoding, base64.URLEncoding, base64.RawStdEncoding, base64.StdEncoding} {
		if b, err := enc.DecodeString(s); err == nil && strings.Contains(string(b), ":") {
			return string(b)
		}
	}
	if unescaped, err := url.PathUnescape(s); err == nil {
		return unescaped
	}
	return s
}

// ssURL matches ss:// URLs inside error text.
var ssURL = regexp.MustCompile(`(?i)ss://[^\s\[\]]+`)

// redactSSURL hides the credentials of an ss:// URL: the userinfo in SIP002
// form, or the whole base64 blob in legacy form.
func redactSSURL(u string) string {
	if at := strings.LastIndex(u, "@"); at >= 0 {
		return "ss://REDACTED" + u[at:]
	}
	return "ss://REDACTED"
}

// Redact removes the key's credentials from s (typically an error message
// from outline-sdk, which may quote the transport config).
func (k *AccessKey) Redact(s string) string {
	s = ssURL.ReplaceAllStringFunc(s, redactSSURL)
	if k.Password == "" {
		return s
	}
	creds := k.Method + ":" + k.Password
	for _, secret := range []string{
		base64.RawURLEncoding.EncodeToString([]byte(creds)),
		base64.URLEncoding.EncodeToString([]byte(creds)),
		base64.RawStdEncoding.EncodeToString([]byte(creds)),
		base64.StdEncoding.EncodeToString([]byte(creds)),
		url.QueryEscape(k.Password),
		k.Password,
	} {
		s = strings.ReplaceAll(s, secret, "REDACTED")
	}
	return s
}

// Server returns "host:port" for logging (no credentials).
func (k *AccessKey) Server() string {
	return net.JoinHostPort(k.Host, strconv.Itoa(int(k.Port)))
}

// Transport is a pair of outline-sdk configurl strings.
type Transport struct {
	// Stream builds the TCP dialer: the optional prefix (e.g. "split:2",
	// "tlsfrag:1") is applied below Shadowsocks.
	Stream string
	// Packet builds the UDP listener. Prefixes are not applied: most of them
	// (split, tlsfrag) only exist for streams.
	Packet string
	// Server is the Shadowsocks server address, for the reachability check.
	Server netip.AddrPort
}

// TransportConfig returns the configurl strings for this key, dialing
// serverIP instead of the hostname.
//
// Using the IP matters: once the tunnel is up, DNS goes through the tunnel,
// and NetworkManager only routes the resolved gateway IP around it.
func (k *AccessKey) TransportConfig(serverIP netip.Addr, prefix string) Transport {
	ss := url.URL{
		Scheme: "ss",
		// base64url userinfo cannot contain '|' (the configurl separator).
		User: url.User(base64.RawURLEncoding.EncodeToString([]byte(k.Method + ":" + k.Password))),
		Host: netip.AddrPortFrom(serverIP.Unmap(), k.Port).String(),
	}
	if k.Prefix != "" {
		ss.RawQuery = url.Values{"prefix": {k.Prefix}}.Encode()
	}
	t := Transport{Stream: ss.String(), Packet: ss.String(), Server: netip.AddrPortFrom(serverIP.Unmap(), k.Port)}
	if prefix = strings.Trim(strings.TrimSpace(prefix), "|"); prefix != "" {
		t.Stream = prefix + "|" + t.Stream
	}
	return t
}

// ResolveServer resolves the key's host to one address, preferring IPv4.
func (k *AccessKey) ResolveServer(ctx context.Context, r *net.Resolver) (netip.Addr, error) {
	if ip, err := netip.ParseAddr(k.Host); err == nil {
		return ip.Unmap(), nil
	}
	if r == nil {
		r = net.DefaultResolver
	}
	ips, err := r.LookupNetIP(ctx, "ip", k.Host)
	if err != nil {
		return netip.Addr{}, fmt.Errorf("resolve server %s: %w", k.Host, err)
	}
	var v6 netip.Addr
	for _, ip := range ips {
		ip = ip.Unmap()
		if ip.Is4() {
			return ip, nil
		}
		if !v6.IsValid() {
			v6 = ip
		}
	}
	if v6.IsValid() {
		return v6, nil
	}
	return netip.Addr{}, fmt.Errorf("resolve server %s: no addresses", k.Host)
}

// Settings are the Outline options of an NM connection profile.
type Settings struct {
	AccessKey       *AccessKey
	DNS             []netip.Addr
	IPv6            string // IPv6Tunnel or IPv6Off
	NeverDefault    bool
	TransportPrefix string
}

const (
	IPv6Tunnel = "tunnel" // route IPv6 into the tunnel (no leaks)
	IPv6Off    = "off"    // leave IPv6 alone (traffic bypasses the VPN)
)

var defaultDNS = []netip.Addr{netip.MustParseAddr("1.1.1.1")}

// ParseSettings reads and validates the connection profile. Errors are
// nmvpn.ErrBadArguments and never contain the access key.
func ParseSettings(s nmvpn.ConnectionSettings) (*Settings, error) {
	data, secrets := s.VPNData(), s.VPNSecrets()
	bad := func(format string, args ...any) error { return nmvpn.Errorf(nmvpn.ErrBadArguments, format, args...) }

	key := secrets[SecretAccessKey]
	if strings.TrimSpace(key) == "" {
		return nil, bad("missing %q secret", SecretAccessKey)
	}
	ak, err := ParseAccessKey(key)
	if err != nil {
		return nil, bad("%v", err)
	}
	st := &Settings{AccessKey: ak, IPv6: IPv6Tunnel, TransportPrefix: data[KeyTransportPrefix]}

	for _, f := range strings.FieldsFunc(data[KeyDNS], func(r rune) bool { return r == ',' || r == ' ' || r == ';' }) {
		ip, err := netip.ParseAddr(f)
		if err != nil {
			return nil, bad("invalid DNS server %q", f)
		}
		st.DNS = append(st.DNS, ip.Unmap())
	}
	if len(st.DNS) == 0 {
		st.DNS = defaultDNS
	}

	switch v := strings.ToLower(strings.TrimSpace(data[KeyIPv6])); v {
	case "", IPv6Tunnel:
	case IPv6Off:
		st.IPv6 = IPv6Off
	default:
		return nil, bad("invalid %s value %q (want %s or %s)", KeyIPv6, v, IPv6Tunnel, IPv6Off)
	}

	switch v := strings.ToLower(strings.TrimSpace(data[KeyNeverDefault])); v {
	case "", "no", "false", "0":
	case "yes", "true", "1":
		st.NeverDefault = true
	default:
		return nil, bad("invalid %s value %q", KeyNeverDefault, v)
	}
	return st, nil
}

// checkResolver is the DNS server used for connectivity checks through the
// tunnel; IPv4 is preferred because the server may lack IPv6 egress.
func (s *Settings) checkResolver() netip.Addr {
	if v4 := s.DNS4(); len(v4) > 0 {
		return v4[0]
	}
	return s.DNS[0]
}

// DNS4 and DNS6 split the DNS servers by family.
func (s *Settings) DNS4() []netip.Addr { return filterAddrs(s.DNS, true) }
func (s *Settings) DNS6() []netip.Addr { return filterAddrs(s.DNS, false) }

func filterAddrs(in []netip.Addr, v4 bool) []netip.Addr {
	var out []netip.Addr
	for _, a := range in {
		if a.Is4() == v4 {
			out = append(out, a)
		}
	}
	return out
}
