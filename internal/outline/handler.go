package outline

import (
	"context"
	"errors"
	"log/slog"
	"sync"

	"network-manager-outline/internal/nmvpn"
)

// Handler implements nmvpn.Handler for Outline.
type Handler struct {
	log *slog.Logger

	mu     sync.Mutex
	cancel context.CancelFunc
	dev    *Device
}

var _ nmvpn.Handler = (*Handler)(nil)

func NewHandler(log *slog.Logger) *Handler { return &Handler{log: log} }

func (h *Handler) NeedSecrets(s nmvpn.ConnectionSettings) (string, error) {
	if _, ok := vpnSecrets(s)[SecretAccessKey]; ok {
		return "", nil
	}
	return "vpn", nil
}

// Connect flow (TODO(phase1)):
//  1. ResolveAccessKey + resolve server IP (before routes change)
//  2. tun.Open
//  3. NewDevice, CheckUDP, go Pump
//  4. emit Config {tundev, gateway=server IP, mtu, has-ip4, has-ip6}
//  5. emit Ip4Config / Ip6Config {address, prefix, dns, never-default}
//  6. SetState(StateStarted); on Pump error -> Failure + StateStopped
func (h *Handler) Connect(ctx context.Context, s nmvpn.ConnectionSettings, e nmvpn.Emitter) error {
	return errors.New("not implemented")
}

func (h *Handler) Disconnect() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.cancel != nil {
		h.cancel()
		h.cancel = nil
	}
	if h.dev != nil {
		err := h.dev.Close()
		h.dev = nil
		return err
	}
	return nil
}

// vpnData returns vpn.data as map[string]string.
func vpnData(s nmvpn.ConnectionSettings) map[string]string { return stringDict(s, "data") }

// vpnSecrets returns vpn.secrets as map[string]string.
func vpnSecrets(s nmvpn.ConnectionSettings) map[string]string { return stringDict(s, "secrets") }

func stringDict(s nmvpn.ConnectionSettings, key string) map[string]string {
	out := map[string]string{}
	if v, ok := s["vpn"][key]; ok {
		if m, ok := v.Value().(map[string]string); ok {
			out = m
		}
	}
	return out
}
