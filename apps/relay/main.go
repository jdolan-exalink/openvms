// Command relay runs the OpenVMS Media Relay service for NAT/firewall traversal.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jdolan-exalink/openvms/internal/relay"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	addr := os.Getenv("OPENVMS_RELAY_ADDR")
	if addr == "" {
		addr = ":8554"
	}

	server := relay.NewServer(relay.Config{
		ListenAddr: addr,
		Log:        log,
		TokenTTL:   1 * time.Hour,
	})

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		<-ctx.Done()
		_ = server.Close()
	}()

	log.Info("starting openvms media relay", "addr", addr)
	if err := server.Start(addr); err != nil {
		log.Error("relay server exited", "error", err)
	}
}
