package outline

import (
	"context"
	"log/slog"
	"sync"

	"network-manager-outline/internal/nmvpn"
)

// Handler implements nmvpn.Handler for Outline.
type Handler struct {
	log *slog.Logger

	mu  sync.Mutex
	dev *Device
}

var _ nmvpn.Handler = (*Handler)(nil)

func NewHandler(log *slog.Logger) *Handler { return &Handler{log: log} }

func (h *Handler) NeedSecrets(s nmvpn.ConnectionSettings) (string, error) {
	if s.VPNSecrets()[SecretAccessKey] != "" {
		return "", nil
	}
	return "vpn", nil
}

// Connect flow (TODO(phase1)):
//  1. ResolveAccessKey + resolve server IP (before routes change)
//  2. tun.Open
//  3. NewDevice, CheckUDP, go Pump (ctx cancels it)
//  4. e.SetConfig {TunDev, Gateway=server IP, MTU, HasIP4, HasIP6}
//  5. e.SetIP4Config / e.SetIP6Config {Address, DNS, NeverDefault}
//     -> the plugin reports STARTED once all announced configs are sent
//  6. on Pump error: e.Fail(nmvpn.FailureConnectFailed)
func (h *Handler) Connect(ctx context.Context, s nmvpn.ConnectionSettings, e nmvpn.Emitter) error {
	if s.VPNSecrets()[SecretAccessKey] == "" {
		return nmvpn.Errorf(nmvpn.ErrBadArguments, "missing %q secret", SecretAccessKey)
	}
	return nmvpn.Errorf(nmvpn.ErrFailed, "not implemented")
}

func (h *Handler) Disconnect() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.dev != nil {
		err := h.dev.Close()
		h.dev = nil
		return err
	}
	return nil
}
