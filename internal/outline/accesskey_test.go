package outline

import (
	"context"
	"encoding/base64"
	"net/netip"
	"strings"
	"testing"

	"github.com/godbus/dbus/v5"
	"golang.getoutline.org/sdk/transport"
	"golang.getoutline.org/sdk/x/configurl"

	"network-manager-outline/internal/nmvpn"
)

const testMethod = "chacha20-ietf-poly1305"

func b64url(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }

func TestParseAccessKey(t *testing.T) {
	tests := []struct {
		name, key                      string
		method, pass, host, prefix, nm string
		port                           uint16
	}{
		{
			name:   "outline SIP002",
			key:    "ss://" + b64url(testMethod+":s3cret") + "@203.0.113.7:8388/?outline=1#My%20Server",
			method: testMethod, pass: "s3cret", host: "203.0.113.7", port: 8388, nm: "My Server",
		},
		{
			name:   "SIP002 padded std base64",
			key:    "ss://" + base64.StdEncoding.EncodeToString([]byte("aes-256-gcm:pw")) + "@vpn.example.com:443",
			method: "aes-256-gcm", pass: "pw", host: "vpn.example.com", port: 443,
		},
		{
			name:   "SIP002 plain userinfo with prefix",
			key:    "ss://" + testMethod + ":p%40ss@vpn.example.com:443?prefix=%16%03%01%00%C2%A8%01%01",
			method: testMethod, pass: "p@ss", host: "vpn.example.com", port: 443, prefix: "\x16\x03\x01\x00¨\x01\x01",
		},
		{
			name:   "legacy base64",
			key:    "ss://" + b64url(testMethod+":legacy@198.51.100.1:8000") + "#old",
			method: testMethod, pass: "legacy", host: "198.51.100.1", port: 8000, nm: "old",
		},
		{
			name:   "IPv6 server",
			key:    "ss://" + b64url(testMethod+":x") + "@[2001:db8::1]:8388",
			method: testMethod, pass: "x", host: "2001:db8::1", port: 8388,
		},
		{
			name:   "whitespace and upper-case method",
			key:    "  ss://" + b64url("AES-256-GCM:x") + "@1.2.3.4:1\n",
			method: "aes-256-gcm", pass: "x", host: "1.2.3.4", port: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k, err := ParseAccessKey(tt.key)
			if err != nil {
				t.Fatal(err)
			}
			if k.Method != tt.method || k.Password != tt.pass || k.Host != tt.host || k.Port != tt.port ||
				k.Prefix != tt.prefix || k.Name != tt.nm {
				t.Fatalf("got %+v", *k)
			}
		})
	}
}

func TestParseAccessKeyErrors(t *testing.T) {
	for _, key := range []string{
		"",
		"https://example.com",
		"ssconf://example.com/key",
		"ss://" + b64url(testMethod+":x") + "@1.2.3.4",       // no port
		"ss://" + b64url(testMethod+":x") + "@1.2.3.4:0",     // port 0
		"ss://" + b64url(testMethod+":x") + "@1.2.3.4:70000", // port range
		"ss://" + b64url("nopassword") + "@1.2.3.4:80",
		"ss://" + b64url("no-at-sign"),
	} {
		if _, err := ParseAccessKey(key); err == nil {
			t.Errorf("ParseAccessKey(%q) succeeded", key)
		}
	}
}

func TestTransportConfigAcceptedBySDK(t *testing.T) {
	k, err := ParseAccessKey("ss://" + b64url(testMethod+":pa|ss:word") + "@vpn.example.com:443/?prefix=%7C%01")
	if err != nil {
		t.Fatal(err)
	}
	for _, ip := range []string{"203.0.113.7", "2001:db8::1"} {
		for _, prefix := range []string{"", "split:2", " split:2| "} {
			tr := k.TransportConfig(netip.MustParseAddr(ip), prefix)
			for _, cfg := range []string{tr.Stream, tr.Packet} {
				if strings.Contains(cfg, "vpn.example.com") {
					t.Fatalf("hostname not replaced: %s", cfg)
				}
			}
			wantPipes := 0
			if strings.TrimSpace(prefix) != "" {
				wantPipes = 1
			}
			if strings.Count(tr.Stream, "|") != wantPipes || strings.Contains(tr.Packet, "|") {
				t.Fatalf("unexpected pipes: %+v", tr)
			}
			if _, err := configurl.New[transport.StreamDialer](context.Background(), registry, tr.Stream); err != nil {
				t.Fatalf("configurl rejected stream %q: %v", tr.Stream, err)
			}
			if _, err := configurl.New[transport.PacketListener](context.Background(), registry, tr.Packet); err != nil {
				t.Fatalf("configurl rejected packet %q: %v", tr.Packet, err)
			}
		}
	}
}

func TestResolveServer(t *testing.T) {
	k := &AccessKey{Host: "192.0.2.1"}
	ip, err := k.ResolveServer(context.Background(), nil)
	if err != nil || ip != netip.MustParseAddr("192.0.2.1") {
		t.Fatal(ip, err)
	}
	k.Host = "localhost"
	ip, err = k.ResolveServer(context.Background(), nil)
	if err != nil || !ip.IsLoopback() {
		t.Fatal(ip, err)
	}
}

func settings(data, secrets map[string]string) nmvpn.ConnectionSettings {
	return nmvpn.ConnectionSettings{"vpn": {
		"data":    dbus.MakeVariant(data),
		"secrets": dbus.MakeVariant(secrets),
	}}
}

var testKey = "ss://" + b64url(testMethod+":s3cret") + "@203.0.113.7:8388"

func TestParseSettings(t *testing.T) {
	st, err := ParseSettings(settings(nil, map[string]string{SecretAccessKey: testKey}))
	if err != nil {
		t.Fatal(err)
	}
	if st.IPv6 != IPv6Tunnel || st.NeverDefault || len(st.DNS) != 1 || st.DNS[0] != netip.MustParseAddr("1.1.1.1") {
		t.Fatalf("defaults: %+v", st)
	}

	st, err = ParseSettings(settings(map[string]string{
		KeyDNS: "9.9.9.9, 2620:fe::fe;8.8.8.8", KeyIPv6: "off", KeyNeverDefault: "yes", KeyTransportPrefix: "split:2",
	}, map[string]string{SecretAccessKey: testKey}))
	if err != nil {
		t.Fatal(err)
	}
	if len(st.DNS4()) != 2 || len(st.DNS6()) != 1 || st.IPv6 != IPv6Off || !st.NeverDefault || st.TransportPrefix != "split:2" {
		t.Fatalf("parsed: %+v", st)
	}
	if st.checkResolver() != netip.MustParseAddr("9.9.9.9") {
		t.Fatal("check resolver should prefer IPv4")
	}
}

func TestParseSettingsErrors(t *testing.T) {
	cases := []nmvpn.ConnectionSettings{
		settings(nil, nil),
		settings(nil, map[string]string{SecretAccessKey: "ss://garbage"}),
		settings(map[string]string{KeyDNS: "not-an-ip"}, map[string]string{SecretAccessKey: testKey}),
		settings(map[string]string{KeyIPv6: "maybe"}, map[string]string{SecretAccessKey: testKey}),
		settings(map[string]string{KeyNeverDefault: "perhaps"}, map[string]string{SecretAccessKey: testKey}),
	}
	for i, c := range cases {
		_, err := ParseSettings(c)
		pe, ok := err.(*nmvpn.Error)
		if !ok || pe.Code != nmvpn.ErrBadArguments {
			t.Errorf("case %d: err = %v", i, err)
		}
		if err != nil && strings.Contains(err.Error(), "s3cret") {
			t.Errorf("case %d: error leaks the password", i)
		}
	}
}

func TestParseAccessKeyRejectsUnknownCipher(t *testing.T) {
	_, err := ParseAccessKey("ss://" + b64url("whacha20-ietf-poly1305:s3cret") + "@203.0.113.7:8388/?outline=1")
	if err == nil || !strings.Contains(err.Error(), "unsupported cipher") {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(err.Error(), "s3cret") {
		t.Fatal("error leaks the password")
	}
}

func TestRedact(t *testing.T) {
	k, err := ParseAccessKey("ss://" + b64url(testMethod+":s3cret+/=") + "@203.0.113.7:8388")
	if err != nil {
		t.Fatal(err)
	}
	tr := k.TransportConfig(netip.MustParseAddr("203.0.113.7"), "split:2")
	msgs := []string{
		// Shape of the outline-sdk error seen in the journal.
		"create stream dialer: failed to decode host string [" + tr.Stream + "]: illegal base64 data at input byte 2",
		"dial " + tr.Packet + ": connection refused",
		"password s3cret+/= rejected; escaped s3cret%2B%2F%3D",
		"legacy ss://" + b64url(testMethod+":s3cret+/=@203.0.113.7:8388") + "#x",
		"std " + base64.StdEncoding.EncodeToString([]byte(testMethod+":s3cret+/=")),
	}
	for _, m := range msgs {
		got := k.Redact(m)
		if strings.Contains(got, "s3cret") || strings.Contains(got, b64url(testMethod+":s3cret")) ||
			strings.Contains(got, base64.StdEncoding.EncodeToString([]byte(testMethod+":s3cret+/="))) {
			t.Errorf("not redacted: %q -> %q", m, got)
		}
		if !strings.Contains(got, "REDACTED") {
			t.Errorf("no marker: %q", got)
		}
	}
	if got := k.Redact("203.0.113.7:8388 timeout"); got != "203.0.113.7:8388 timeout" {
		t.Errorf("changed unrelated text: %q", got)
	}
}
