// nm-outline-service is the NetworkManager VPN service for Outline.
// NetworkManager launches it (see data/nm-outline-service.name) as root.
package main

import (
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
	debug := flag.Bool("debug", os.Getenv("NM_VPN_LOG_LEVEL") != "", "enable debug logging")
	flag.Parse()

	level := slog.LevelInfo
	if *debug {
		level = slog.LevelDebug
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))

	p := nmvpn.New(*busName, outline.NewHandler(log), log)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	go func() { <-sig; p.Stop() }()

	if err := p.Run(); err != nil {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
}
