// nm-outline-service is the NetworkManager VPN service for Outline.
// NetworkManager launches it (see data/nm-outline-service.name) as root.
package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"network-manager-outline/internal/nmvpn"
	"network-manager-outline/internal/outline"
)

// syslogDebug is LOG_DEBUG; NetworkManager passes its VPN log level to
// plugins as a syslog priority in NM_VPN_LOG_LEVEL (always set).
const syslogDebug = 7

func main() {
	busName := flag.String("bus-name", outline.ServiceName, "D-Bus service name to claim")
	debug := flag.Bool("debug", false, "debug logging; keep running after disconnect (for running the service by hand)")
	flag.Parse()

	_, fromNM := os.LookupEnv("NM_VPN_LOG_LEVEL")
	log := newLogger(*debug || nmLogLevel() >= syslogDebug, fromNM)

	p := nmvpn.New(outline.NewHandler(log), nmvpn.Options{
		BusName:   *busName,
		WatchPeer: !*debug, // like nm-openvpn-service: exit with NM unless debugging by hand
		Logger:    log,
	})

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := p.Run(ctx); err != nil {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
}

// nmLogLevel returns NM_VPN_LOG_LEVEL, or -1 if unset or invalid.
func nmLogLevel() int {
	v, err := strconv.Atoi(os.Getenv("NM_VPN_LOG_LEVEL"))
	if err != nil {
		return -1
	}
	return v
}

// newLogger logs to stderr. When started by NetworkManager, stderr ends up in
// the journal, which adds its own timestamps.
func newLogger(debug, dropTime bool) *slog.Logger {
	opts := &slog.HandlerOptions{Level: slog.LevelInfo}
	if debug {
		opts.Level = slog.LevelDebug
	}
	if dropTime {
		opts.ReplaceAttr = func(groups []string, a slog.Attr) slog.Attr {
			if len(groups) == 0 && a.Key == slog.TimeKey {
				return slog.Attr{}
			}
			return a
		}
	}
	return slog.New(slog.NewTextHandler(os.Stderr, opts))
}
