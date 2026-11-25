package outline

// Dynamic access keys (ssconf://), following Outline's documentation
// (developer.getoutline.org/vpn/management/dynamic-access-keys) and the
// official client (outline-apps client/go/outline/fetch.go and parse.go):
//
//   - ssconf://host[:port]/path?query is fetched from https://host[:port]/path?query;
//     plain https:// keys are accepted too. The #fragment is only a name.
//   - One GET with a 10 s timeout on every connect; non-2xx is an error.
//   - The body is trimmed and may be an ss:// link, the legacy JSON object
//     (server, server_port, password, method, prefix), the YAML/JSON
//     "transport" config, or a provider error {"error": {"message", "details"}}.
//
// Unlike the official client, redirects must stay on HTTPS, and only
// Shadowsocks transports (optionally as a tcpudp pair) are supported.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/goccy/go-yaml"
)

const (
	fetchTimeout = 10 * time.Second
	maxKeyConfig = 64 * 1024
)

// DynamicKey is an ssconf:// (or https://) access key: the location of the
// actual key, fetched on every connect.
type DynamicKey struct {
	URL  *url.URL // https URL to fetch, without fragment
	Name string   // display name from the #fragment, if any
}

// ParseDynamicKey parses an ssconf:// or https:// key. It does no network I/O.
func ParseDynamicKey(key string) (*DynamicKey, error) {
	u, err := url.Parse(strings.TrimSpace(key))
	if err != nil {
		return nil, errors.New("access key is not a valid URL")
	}
	switch strings.ToLower(u.Scheme) {
	case "ssconf", "https":
	default:
		return nil, fmt.Errorf("dynamic access key must start with ssconf:// or https://, got %q", u.Scheme+"://")
	}
	if u.Hostname() == "" {
		return nil, errors.New("dynamic access key has no host")
	}
	name := keyName(u.Fragment)
	u.Scheme, u.Fragment, u.RawFragment = "https", "", ""
	return &DynamicKey{URL: u, Name: name}, nil
}

// keyName returns the display name from a key's fragment: like the official
// client, the first &-separated entry that is not a key=value pair.
func keyName(fragment string) string {
	for _, part := range strings.Split(fragment, "&") {
		if part != "" && !strings.Contains(part, "=") {
			return part
		}
	}
	return ""
}

// Host returns the key server's host, safe to log (the path is often the secret).
func (d *DynamicKey) Host() string { return d.URL.Host }

// Redact hides the key's path and query (often a per-user token) in s.
func (d *DynamicKey) Redact(s string) string {
	if d == nil {
		return s
	}
	safe := "https://" + d.URL.Host + "/REDACTED"
	for _, secret := range []string{d.URL.String(), strings.Replace(d.URL.String(), "https://", "ssconf://", 1)} {
		s = strings.ReplaceAll(s, secret, safe)
	}
	if p := d.URL.EscapedPath(); len(p) > 1 {
		s = strings.ReplaceAll(s, p, "/REDACTED")
	}
	if d.URL.RawQuery != "" {
		s = strings.ReplaceAll(s, d.URL.RawQuery, "REDACTED")
	}
	return s
}

// FetchError is a failed fetch. Rejected means the provider refused the key
// (an error response, or HTTP 401/403/404/410), as opposed to a network or
// server problem.
type FetchError struct {
	Rejected bool
	Err      error
}

func (e *FetchError) Error() string { return e.Err.Error() }
func (e *FetchError) Unwrap() error { return e.Err }

// ProviderError is an error message the key provider returned instead of a key.
type ProviderError struct {
	Message string
	Details string
}

func (e *ProviderError) Error() string {
	if e.Details != "" {
		return fmt.Sprintf("key provider: %s (%s)", e.Message, e.Details)
	}
	return "key provider: " + e.Message
}

// Fetch downloads and parses the key. client may be nil.
func (d *DynamicKey) Fetch(ctx context.Context, client *http.Client) (*AccessKey, error) {
	if client == nil {
		client = &http.Client{}
	}
	c := *client
	c.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if req.URL.Scheme != "https" {
			return errors.New("refusing redirect away from https")
		}
		if len(via) >= 10 {
			return errors.New("too many redirects")
		}
		return nil
	}

	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, d.URL.String(), nil)
	if err != nil {
		return nil, &FetchError{Err: err}
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, &FetchError{Err: fmt.Errorf("fetch dynamic key: %w", err)}
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxKeyConfig+1))
	if err != nil {
		return nil, &FetchError{Err: fmt.Errorf("read dynamic key: %w", err)}
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		switch resp.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusGone:
			return nil, &FetchError{Rejected: true, Err: fmt.Errorf("key server refused the key: HTTP %s", resp.Status)}
		}
		return nil, &FetchError{Err: fmt.Errorf("key server error: HTTP %s", resp.Status)}
	}
	if len(body) > maxKeyConfig {
		return nil, &FetchError{Err: fmt.Errorf("dynamic key is larger than %d bytes", maxKeyConfig)}
	}

	key, err := ParseKeyConfig(string(body))
	if err != nil {
		var pe *ProviderError
		return nil, &FetchError{Rejected: errors.As(err, &pe), Err: err}
	}
	if key.Name == "" {
		key.Name = d.Name
	}
	return key, nil
}

// ParseKeyConfig parses the body of a dynamic key: an ss:// link, legacy
// Shadowsocks JSON, a YAML/JSON transport config, or a provider error.
func ParseKeyConfig(text string) (*AccessKey, error) {
	text = strings.TrimSpace(strings.TrimPrefix(text, "\ufeff"))
	if text == "" {
		return nil, errors.New("dynamic key is empty")
	}
	if strings.HasPrefix(strings.ToLower(text), "ss://") {
		return ParseAccessKey(text)
	}

	var doc any
	if err := yaml.Unmarshal([]byte(text), &doc); err != nil {
		return nil, errors.New("dynamic key is not an ss:// link, JSON or YAML")
	}
	switch v := doc.(type) {
	case string: // a quoted ss:// link
		return ParseAccessKey(v)
	case map[string]any:
		if e, ok := v["error"]; ok {
			return nil, providerError(e)
		}
		if t, ok := v["transport"]; ok {
			return parseTransport(t)
		}
		return parseTransport(v) // legacy JSON: the object is the transport
	}
	return nil, fmt.Errorf("dynamic key has unexpected type %T", doc)
}

func providerError(v any) error {
	m, _ := v.(map[string]any)
	pe := &ProviderError{Message: str(m["message"]), Details: str(m["details"])}
	if pe.Message == "" {
		pe.Message = "the key provider returned an error"
	}
	return pe
}

// parseTransport handles a transport node: an ss:// string, a shadowsocks
// object (new or legacy fields), or a tcpudp pair of the same server.
func parseTransport(node any) (*AccessKey, error) {
	switch v := node.(type) {
	case string:
		return ParseAccessKey(v)
	case map[string]any:
		typ := str(v["$type"])
		switch typ {
		case "tcpudp":
			tcp, err := parseTransport(v["tcp"])
			if err != nil {
				return nil, fmt.Errorf("tcp: %w", err)
			}
			udp, err := parseTransport(v["udp"])
			if err != nil {
				return nil, fmt.Errorf("udp: %w", err)
			}
			if tcp.Method != udp.Method || tcp.Password != udp.Password || tcp.Server() != udp.Server() {
				return nil, errors.New("TCP and UDP through different Shadowsocks servers are not supported yet")
			}
			return tcp, nil
		case "", "shadowsocks":
			return parseShadowsocks(v)
		default:
			return nil, fmt.Errorf("transport type %q is not supported yet", typ)
		}
	case nil:
		return nil, errors.New("transport is missing")
	}
	return nil, fmt.Errorf("transport has unexpected type %T", node)
}

func parseShadowsocks(m map[string]any) (*AccessKey, error) {
	var method, password, hostport, prefix string
	switch {
	case m["endpoint"] != nil:
		ep, err := parseEndpoint(m["endpoint"])
		if err != nil {
			return nil, err
		}
		method, password, hostport, prefix = str(m["cipher"]), str(m["secret"]), ep, str(m["prefix"])
	case m["server"] != nil: // legacy JSON
		port, err := portNumber(m["server_port"])
		if err != nil {
			return nil, err
		}
		method, password, prefix = str(m["method"]), str(m["password"]), str(m["prefix"])
		hostport = net.JoinHostPort(str(m["server"]), strconv.Itoa(int(port)))
	default:
		return nil, errors.New("shadowsocks config has no endpoint or server")
	}
	if method == "" {
		return nil, errors.New("shadowsocks config has no cipher")
	}
	// Reuse ss:// validation (host, port, cipher) on the equivalent link.
	u := url.URL{Scheme: "ss", User: url.UserPassword(method, password), Host: hostport}
	if prefix != "" {
		u.RawQuery = url.Values{"prefix": {prefix}}.Encode()
	}
	return ParseAccessKey(u.String())
}

// parseEndpoint accepts "host:port" or {$type: dial, address: "host:port"}.
func parseEndpoint(node any) (string, error) {
	switch v := node.(type) {
	case string:
		return v, nil
	case map[string]any:
		if t := str(v["$type"]); t != "" && t != "dial" {
			return "", fmt.Errorf("endpoint type %q is not supported yet", t)
		}
		if v["dialer"] != nil {
			return "", errors.New("endpoints with a custom dialer are not supported yet")
		}
		if a := str(v["address"]); a != "" {
			return a, nil
		}
		return "", errors.New("endpoint has no address")
	}
	return "", fmt.Errorf("endpoint has unexpected type %T", node)
}

func portNumber(v any) (uint16, error) {
	var n uint64
	switch p := v.(type) {
	case uint64:
		n = p
	case int64:
		if p < 0 {
			return 0, errors.New("server_port is negative")
		}
		n = uint64(p)
	case float64:
		n = uint64(p)
	case string:
		var err error
		if n, err = strconv.ParseUint(p, 10, 16); err != nil {
			return 0, fmt.Errorf("invalid server_port %q", p)
		}
	default:
		return 0, errors.New("server_port is missing")
	}
	if n == 0 || n > 65535 {
		return 0, fmt.Errorf("invalid server_port %d", n)
	}
	return uint16(n), nil
}

func str(v any) string {
	switch s := v.(type) {
	case string:
		return s
	case nil:
		return ""
	}
	return fmt.Sprint(v)
}
