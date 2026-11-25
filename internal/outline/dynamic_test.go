package outline

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"

	"network-manager-outline/internal/nmvpn"
)

func TestParseDynamicKey(t *testing.T) {
	tests := []struct{ key, url, name string }{
		{"ssconf://keys.example.com/a1b2c3d4.yml", "https://keys.example.com/a1b2c3d4.yml", ""},
		{"  ssconf://keys.example.com:8443/k/u?id=7#My%20Server\n", "https://keys.example.com:8443/k/u?id=7", "My Server"},
		{"ssconf://keys.example.com/k#prefix=abc&Home&x=1", "https://keys.example.com/k", "Home"},
		{"https://keys.example.com/k", "https://keys.example.com/k", ""},
		{"SSCONF://keys.example.com/k", "https://keys.example.com/k", ""},
	}
	for _, tt := range tests {
		dk, err := ParseDynamicKey(tt.key)
		if err != nil {
			t.Fatalf("%q: %v", tt.key, err)
		}
		if dk.URL.String() != tt.url || dk.Name != tt.name {
			t.Errorf("%q: url=%q name=%q", tt.key, dk.URL, dk.Name)
		}
	}
	for _, key := range []string{"ssconf://", "http://keys.example.com/k", "ss://x@1.2.3.4:1"} {
		if _, err := ParseDynamicKey(key); err == nil {
			t.Errorf("ParseDynamicKey(%q) succeeded", key)
		}
	}
}

func TestParseKeyConfig(t *testing.T) {
	ssLink := "ss://" + b64url(testMethod+":s3cret") + "@203.0.113.7:8388/?outline=1"
	tests := []struct {
		name, body, host, pass, prefix string
		port                           uint16
	}{
		{"ss link", ssLink, "203.0.113.7", "s3cret", "", 8388},
		{"ss link with BOM and whitespace", "\ufeff\n  " + ssLink + "  \r\n", "203.0.113.7", "s3cret", "", 8388},
		{"quoted ss link", `"` + ssLink + `"`, "203.0.113.7", "s3cret", "", 8388},
		{"legacy JSON (docs example)",
			`{"server": "outline-server.example.com", "server_port": 8388, "password": "example", "method": "chacha20-ietf-poly1305"}`,
			"outline-server.example.com", "example", "", 8388},
		{"legacy JSON with prefix and string port",
			`{"server": "203.0.113.9", "server_port": "443", "password": "p@ss", "method": "aes-256-gcm", "prefix": "\u0016\u0003\u0001"}`,
			"203.0.113.9", "p@ss", "\x16\x03\x01", 443},
		{"YAML tcpudp (docs example)", `transport:
  $type: tcpudp
  tcp:
    $type: shadowsocks
    endpoint: outline-server.example.com:8388
    cipher: chacha20-ietf-poly1305
    secret: example
  udp:
    $type: shadowsocks
    endpoint: outline-server.example.com:8388
    cipher: chacha20-ietf-poly1305
    secret: example
`, "outline-server.example.com", "example", "", 8388},
		{"transport shadowsocks with dial endpoint", `transport:
  $type: shadowsocks
  endpoint: {$type: dial, address: "[2001:db8::1]:8388"}
  cipher: chacha20-ietf-poly1305
  secret: "x:y"
  prefix: "POST "
`, "2001:db8::1", "x:y", "POST ", 8388},
		{"transport as ss link", "transport: " + ssLink, "203.0.113.7", "s3cret", "", 8388},
		{"tcpudp with ss links", "transport: {$type: tcpudp, tcp: \"" + ssLink + "\", udp: \"" + ssLink + "\"}", "203.0.113.7", "s3cret", "", 8388},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k, err := ParseKeyConfig(tt.body)
			if err != nil {
				t.Fatal(err)
			}
			if k.Host != tt.host || k.Port != tt.port || k.Password != tt.pass || k.Prefix != tt.prefix {
				t.Fatalf("got %+v", *k)
			}
		})
	}
}

func TestParseKeyConfigErrors(t *testing.T) {
	tests := []struct{ name, body, want string }{
		{"empty", "  \n", "empty"},
		{"garbage", "<html>oops</html>", ""},
		{"websocket", "transport: {$type: tcpudp, tcp: {$type: shadowsocks, endpoint: {$type: websocket, url: wss://x}, cipher: chacha20-ietf-poly1305, secret: s}, udp: x}", "not supported"},
		{"first-supported", "transport: {$type: first-supported, options: []}", "not supported"},
		{"different tcp and udp servers", `transport:
  $type: tcpudp
  tcp: {$type: shadowsocks, endpoint: "a.example.com:1", cipher: chacha20-ietf-poly1305, secret: s}
  udp: {$type: shadowsocks, endpoint: "b.example.com:1", cipher: chacha20-ietf-poly1305, secret: s}
`, "different"},
		{"unknown cipher", `{"server": "1.2.3.4", "server_port": 1, "password": "p", "method": "rot13"}`, "unsupported cipher"},
		{"bad port", `{"server": "1.2.3.4", "server_port": 70000, "password": "p", "method": "aes-256-gcm"}`, "server_port"},
		{"no endpoint", "transport: {$type: shadowsocks, cipher: aes-256-gcm}", "no endpoint"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseKeyConfig(tt.body)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want it to contain %q", err, tt.want)
			}
		})
	}

	_, err := ParseKeyConfig(`{"error": {"message": "Subscription expired", "details": "renew at example.com"}}`)
	var pe *ProviderError
	if !errors.As(err, &pe) || pe.Message != "Subscription expired" || pe.Details != "renew at example.com" {
		t.Fatalf("provider error: %v", err)
	}
}

// keyServer serves body with status at /secret-token and records requests.
func keyServer(t *testing.T, status int, body string) (*httptest.Server, *[]string) {
	t.Helper()
	var seen []string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.RequestURI())
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, &seen
}

func ssconfFor(srv *httptest.Server, rest string) string {
	return strings.Replace(srv.URL, "https://", "ssconf://", 1) + rest
}

func TestFetch(t *testing.T) {
	srv, seen := keyServer(t, 200, `{"server": "203.0.113.7", "server_port": 8388, "password": "s3cret", "method": "chacha20-ietf-poly1305"}`)
	dk, err := ParseDynamicKey(ssconfFor(srv, "/secret-token?u=1#Work"))
	if err != nil {
		t.Fatal(err)
	}
	k, err := dk.Fetch(context.Background(), srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	if k.Server() != "203.0.113.7:8388" || k.Name != "Work" {
		t.Fatalf("got %+v", *k)
	}
	if len(*seen) != 1 || (*seen)[0] != "GET /secret-token?u=1" {
		t.Fatalf("requests = %v", *seen)
	}
}

func TestFetchErrors(t *testing.T) {
	tests := []struct {
		name     string
		status   int
		body     string
		rejected bool
	}{
		{"not found", 404, "gone", true},
		{"forbidden", 403, "", true},
		{"server error", 500, "", false},
		{"provider error", 200, `{"error": {"message": "Key revoked"}}`, true},
		{"empty", 200, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, _ := keyServer(t, tt.status, tt.body)
			dk, _ := ParseDynamicKey(ssconfFor(srv, "/secret-token"))
			_, err := dk.Fetch(context.Background(), srv.Client())
			var fe *FetchError
			if !errors.As(err, &fe) || fe.Rejected != tt.rejected {
				t.Fatalf("err = %v (rejected=%v), want rejected=%v", err, fe != nil && fe.Rejected, tt.rejected)
			}
		})
	}
}

func TestFetchRefusesHTTPRedirect(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://keys.example.com/plain", http.StatusFound)
	}))
	defer srv.Close()
	dk, _ := ParseDynamicKey(srv.URL + "/k")
	_, err := dk.Fetch(context.Background(), srv.Client())
	if err == nil || !strings.Contains(err.Error(), "https") {
		t.Fatalf("err = %v", err)
	}
}

func TestDynamicRedact(t *testing.T) {
	dk, _ := ParseDynamicKey("ssconf://keys.example.com/u/0123456789abcdef?sig=XYZ#Name")
	for _, msg := range []string{
		`fetch dynamic key: Get "https://keys.example.com/u/0123456789abcdef?sig=XYZ": dial tcp: timeout`,
		"key ssconf://keys.example.com/u/0123456789abcdef?sig=XYZ failed",
		"path /u/0123456789abcdef",
	} {
		got := dk.Redact(msg)
		if strings.Contains(got, "0123456789abcdef") || strings.Contains(got, "XYZ") {
			t.Errorf("not redacted: %q", got)
		}
	}
	if got := dk.Redact("keys.example.com unreachable"); got != "keys.example.com unreachable" {
		t.Errorf("host should stay: %q", got)
	}
}

func TestHandlerDynamicKey(t *testing.T) {
	srv, seen := keyServer(t, 200, "transport: "+"ss://"+b64url(testMethod+":s3cret")+"@203.0.113.7:8388")
	f := newFixture(&fakeStack{})
	f.h.httpClient = srv.Client()
	key := ssconfFor(srv, "/secret-token#Home")
	if err := f.h.Connect(f.ctx, settings(nil, map[string]string{SecretAccessKey: key}), f.em); err != nil {
		t.Fatal(err)
	}
	if ev := f.em.next(t); ev != "config" {
		t.Fatalf("event %q", ev)
	}
	if f.em.generic.Gateway != netip.MustParseAddr("203.0.113.7") {
		t.Fatalf("gateway = %s", f.em.generic.Gateway)
	}
	if !strings.Contains(f.stack.transport.Stream, "@203.0.113.7:8388") {
		t.Fatalf("transport = %q", f.stack.transport.Stream)
	}
	f.disconnect(t)
	if len(*seen) != 1 {
		t.Fatalf("requests = %v", *seen)
	}
}

func TestHandlerDynamicKeyFailures(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		want   nmvpn.Failure
	}{
		{"revoked", 404, "", nmvpn.FailureLoginFailed},
		{"provider error", 200, `{"error": {"message": "Subscription expired"}}`, nmvpn.FailureLoginFailed},
		{"server down", 502, "", nmvpn.FailureConnectFailed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, _ := keyServer(t, tt.status, tt.body)
			f := newFixture(&fakeStack{})
			f.h.httpClient = srv.Client()
			if err := f.h.Connect(f.ctx, settings(nil, map[string]string{SecretAccessKey: ssconfFor(srv, "/secret-token")}), f.em); err != nil {
				t.Fatal(err)
			}
			if ev := f.em.next(t); ev != "fail" {
				t.Fatalf("event %q", ev)
			}
			f.disconnect(t)
			if f.em.reason != tt.want {
				t.Fatalf("reason = %d, want %d", f.em.reason, tt.want)
			}
		})
	}
}

func TestParseSettingsDynamic(t *testing.T) {
	st, err := ParseSettings(settings(nil, map[string]string{SecretAccessKey: "ssconf://keys.example.com/k"}))
	if err != nil {
		t.Fatal(err)
	}
	if st.DynamicKey == nil || st.AccessKey != nil {
		t.Fatalf("settings = %+v", st)
	}
	if _, err := ParseSettings(settings(nil, map[string]string{SecretAccessKey: "ssconf://"})); err == nil {
		t.Fatal("ssconf:// without host accepted")
	}
}
