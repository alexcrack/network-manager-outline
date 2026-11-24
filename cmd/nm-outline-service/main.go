// nm-outline-service is the NetworkManager VPN service for Outline.
// NetworkManager launches it (see data/nm-outline-service.name) as root.
package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"network-manager-outline/internal/nmvpn"
	"network-manager-outline/internal/outline"
)

func main() {
	busName := flag.String("bus-name", outline.ServiceName, "D-Bus service name to claim")
	debug := flag.Bool("debug", os.Getenv("NM_VPN_LOG_LEVEL") != "", "enable debug logging; keep running after disconnect")
	flag.Parse()

	level := slog.LevelInfo
	if *debug {
		level = slog.LevelDebug
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))

	p := nmvpn.New(outline.NewHandler(log), nmvpn.Options{
		BusName:   *busName,
		WatchPeer: !*debug, // like nm-openvpn-service: quit with NM unless debugging
		Logger:    log,
	})

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := p.Run(ctx); err != nil {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
}
