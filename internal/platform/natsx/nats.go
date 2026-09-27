// Package natsx connects to NATS JetStream, the internal event bus (PRD §12).
package natsx

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// Streams owned by the control plane. Subjects follow the naming in PRD §12.
var Streams = []jetstream.StreamConfig{
	{
		Name:        "FRIGATE",
		Description: "Review and health events ingested from Frigate servers",
		Subjects:    []string{"frigate.>"},
		Retention:   jetstream.LimitsPolicy,
		MaxAge:      7 * 24 * time.Hour,
		Storage:     jetstream.FileStorage,
	},
	{
		Name:        "PLATFORM",
		Description: "Server, camera, user and export lifecycle events",
		Subjects:    []string{"server.>", "camera.>", "lpr.>", "user.>", "export.>"},
		Retention:   jetstream.LimitsPolicy,
		MaxAge:      7 * 24 * time.Hour,
		Storage:     jetstream.FileStorage,
	},
}

func Connect(url, name string, log *slog.Logger) (*nats.Conn, error) {
	nc, err := nats.Connect(url,
		nats.Name(name),
		nats.MaxReconnects(-1),
		nats.DisconnectErrHandler(func(_ *nats.Conn, err error) {
			if err != nil {
				log.Warn("nats disconnected", "error", err)
			}
		}),
		nats.ReconnectHandler(func(*nats.Conn) { log.Info("nats reconnected") }),
	)
	if err != nil {
		return nil, fmt.Errorf("connect nats %s: %w", url, err)
	}
	return nc, nil
}

// EnsureStreams creates or updates every stream in Streams.
func EnsureStreams(ctx context.Context, nc *nats.Conn) error {
	js, err := jetstream.New(nc)
	if err != nil {
		return err
	}
	for _, cfg := range Streams {
		if _, err := js.CreateOrUpdateStream(ctx, cfg); err != nil {
			return fmt.Errorf("ensure stream %s: %w", cfg.Name, err)
		}
	}
	return nil
}

func Ping(ctx context.Context, nc *nats.Conn) error {
	if nc.Status() != nats.CONNECTED {
		return errors.New("nats status: " + nc.Status().String())
	}
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(2 * time.Second)
	}
	return nc.FlushTimeout(time.Until(deadline))
}
