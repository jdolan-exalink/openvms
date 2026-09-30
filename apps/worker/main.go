// Command worker runs background consumers of the NATS event bus.
// It polls the health of every registered Frigate server (publishing
// server.online/offline/degraded on changes) and pulls review items and plate reads
// into the central event index (publishing frigate.event.new.<tenant>).
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	// PDW-7: the clip watermark job (internal/clipwatermark) burns the tenant's configured
	// IANA time zone into the watermark text (internal/branding.ResolveLocation,
	// time.LoadLocation). This binary's runtime image (deploy/docker/go.Dockerfile's
	// runtime-ffmpeg target, debian:bookworm-slim + ffmpeg) currently does ship
	// /usr/share/zoneinfo (verified: `docker compose exec worker dpkg -l | grep tzdata` shows
	// the tzdata package installed, pulled in transitively by ffmpeg/ca-certificates — this
	// image's own Dockerfile stage never installs it explicitly). Embedding tzdata here too
	// removes the dependency on that transitive package staying pulled in, matching apps/api's
	// same reasoning: the watermark's correctness should not depend on an incidental property
	// of another package's dependency tree.
	_ "time/tzdata"

	"github.com/jdolan-exalink/openvms/internal/clipwatermark"
	"github.com/jdolan-exalink/openvms/internal/events"
	"github.com/jdolan-exalink/openvms/internal/inventory"
	"github.com/jdolan-exalink/openvms/internal/media"
	"github.com/jdolan-exalink/openvms/internal/notify"
	"github.com/jdolan-exalink/openvms/internal/platform/buildinfo"
	"github.com/jdolan-exalink/openvms/internal/platform/config"
	"github.com/jdolan-exalink/openvms/internal/platform/logging"
	"github.com/jdolan-exalink/openvms/internal/platform/natsx"
	"github.com/jdolan-exalink/openvms/internal/platform/objectstore"
	"github.com/jdolan-exalink/openvms/internal/platform/postgres"
	"github.com/jdolan-exalink/openvms/internal/platform/telemetry"
	"github.com/jdolan-exalink/openvms/internal/rules"
	"github.com/jdolan-exalink/openvms/internal/secrets"
	"github.com/jdolan-exalink/openvms/internal/store"
)

const service = "openvms-worker"

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load(service)
	if err != nil {
		return err
	}
	log := logging.New(service, cfg.LogLevel)
	log.Info("starting", "version", buildinfo.Version, "commit", buildinfo.Commit)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	shutdownTracing, err := telemetry.Setup(ctx, service)
	if err != nil {
		return fmt.Errorf("telemetry: %w", err)
	}
	defer func() { _ = shutdownTracing(context.Background()) }()

	pool, err := postgres.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	sealer, err := secrets.FromEnv()
	if err != nil {
		return err
	}

	nc, err := natsx.Connect(cfg.NATSURL, service, log)
	if err != nil {
		return err
	}
	defer nc.Close()
	if err := natsx.EnsureStreams(ctx, nc); err != nil {
		return err
	}

	js, err := jetstream.New(nc)
	if err != nil {
		return err
	}
	objects, err := objectstore.New(ctx, cfg.S3)
	if err != nil {
		return err
	}
	if err := objects.EnsureBucket(ctx); err != nil {
		return err
	}

	st := &store.Store{Pool: pool}
	inv := inventory.New(st, sealer, log)
	adapters := inventory.NewAdapters(inv)

	rulesSvc := rules.NewService(st, &jetstreamPublisher{js: js}, log)
	syncer := &events.Syncer{
		Store: st, Adapters: adapters, Blobs: objects, Log: log,
		Rules:    rulesSvc,
		Interval: cfg.EventSyncInterval, Backfill: cfg.EventBackfill, Concurrency: 8,
		OnNew: func(ctx context.Context, e events.NewEvent) {
			data, _ := json.Marshal(e)
			if _, err := js.Publish(ctx, "frigate.event.new."+e.TenantID.String(), data); err != nil {
				log.WarnContext(ctx, "publish new event", "error", err)
			}
		},
		OnAlarm: func(ctx context.Context, alarms []events.OpenedAlarm) {
			for _, a := range alarms {
				data, _ := json.Marshal(a)
				if _, err := js.Publish(ctx, "alarm.opened."+a.TenantID.String(), data); err != nil {
					log.WarnContext(ctx, "publish opened alarm", "error", err, "alarm_id", a.ID)
				}
			}
		},
	}
	go syncer.Run(ctx)

	// Delivers the outbox rows that rule firings enqueue for external channels.
	deliveries := &notify.Worker{
		Store: st, Sealer: sealer, Log: log,
		Deps:                  notify.Deps{WahaBaseURL: cfg.Waha.BaseURL, WahaAPIKey: cfg.Waha.APIKey},
		DeliveryRetention:     cfg.NotifyDeliveryRetention,
		NotificationRetention: cfg.NotifyReadRetention,
	}
	go deliveries.Run(ctx)

	offline := &rules.OfflineDetector{Store: st, Rules: rulesSvc, Log: log, Interval: cfg.HealthInterval}
	go offline.Run(ctx)

	tracker := &media.ExportTracker{Store: st, Adapters: adapters, Interval: 3 * time.Second, Log: log}
	go tracker.Run(ctx)

	// clipWorker (PDW-4) burns the plate detail watermark into requested clips via ffmpeg,
	// installed only in this image's runtime stage (deploy/docker/go.Dockerfile
	// runtime-ffmpeg target) — the api/other images stay distroless without it.
	clipWorker := &clipwatermark.Worker{Store: st, Adapters: adapters, Blobs: objects, Log: log}
	go clipWorker.Run(ctx)

	poller := &inventory.HealthPoller{
		Svc:         inv,
		Adapters:    adapters,
		Interval:    cfg.HealthInterval,
		Concurrency: 8,
		OnChange: func(ctx context.Context, c inventory.StatusChange) {
			log.InfoContext(ctx, "server status changed", "server_id", c.ServerID, "from", c.From, "to", c.To, "error", c.Error)
			data, _ := json.Marshal(c)
			if _, err := js.Publish(ctx, "server."+c.To, data); err != nil {
				log.ErrorContext(ctx, "publish server status", "error", err)
			}
		},
	}
	go poller.Run(ctx)

	srv := &http.Server{Addr: cfg.HTTPAddr, Handler: probes(pool.Ping, func(ctx context.Context) error { return natsx.Ping(ctx, nc) }), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("probe server", "error", err)
		}
	}()

	<-ctx.Done()
	log.Info("shutting down")
	sctx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	return srv.Shutdown(sctx)
}

// probes serves /health/live and /health/ready for the container orchestrator.
func probes(checks ...func(context.Context) error) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health/live", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	mux.HandleFunc("GET /health/ready", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		for _, check := range checks {
			if err := check(ctx); err != nil {
				http.Error(w, `{"status":"degraded"}`, http.StatusServiceUnavailable)
				return
			}
		}
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	return mux
}

// jetstreamPublisher adapts JetStream to the rules service's Publisher.
type jetstreamPublisher struct {
	js jetstream.JetStream
}

func (p *jetstreamPublisher) Publish(ctx context.Context, subject string, data []byte) error {
	_, err := p.js.Publish(ctx, subject, data)
	return err
}
