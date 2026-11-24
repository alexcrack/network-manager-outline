package outline

import (
	"context"
	"errors"
)

// AccessKey is a resolved Outline access key.
type AccessKey struct {
	// Transport is a configurl string accepted by outline-sdk, e.g. "ss://...".
	Transport string
	// Host is the server hostname/IP used for the external-gateway host route.
	Host string
}

// ResolveAccessKey turns a user-provided key into a static transport config.
//
// Supported:
//   - ss://...       static Outline/Shadowsocks key (SIP002 or legacy base64)
//   - ssconf://...   dynamic key: fetched over HTTPS, response is either an
//     ss:// URL or JSON {server, server_port, password, method, prefix}
//
// TODO(phase1): ss:// parsing and host extraction.
// TODO(phase2): ssconf:// fetching.
func ResolveAccessKey(ctx context.Context, key string) (*AccessKey, error) {
	return nil, errors.New("not implemented")
}
