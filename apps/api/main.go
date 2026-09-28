// Command api is the OpenVMS control plane HTTP API.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jdolan-exalink/openvms/internal/api"
	"github.com/jdolan-exalink/openvms/internal/branding"
	"github.com/jdolan-exalink/openvms/internal/clipwatermark"
	"github.com/jdolan-exalink/openvms/internal/events"
	"github.com/jdolan-exalink/openvms/internal/health"
	"github.com/jdolan-exalink/openvms/internal/identity"
	"github.com/jdolan-exalink/openvms/internal/inventory"
	"github.com/jdolan-exalink/openvms/internal/media"
	"github.com/jdolan-exalink/openvms/internal/platform/buildinfo"
	"github.com/jdolan-exalink/openvms/internal/platform/config"
	"github.com/jdolan-exalink/openvms/internal/platform/logging"
	"github.com/jdolan-exalink/openvms/internal/platform/natsx"
	"github.com/jdolan-exalink/openvms/internal/platform/objectstore"
	"github.com/jdolan-exalink/openvms/internal/platform/postgres"
	"github.com/jdolan-exalink/openvms/internal/platform/telemetry"
	"github.com/jdolan-exalink/openvms/internal/platform/valkeyx"
	"github.com/jdolan-exalink/openvms/internal/secrets"
	dbstore "github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
	"github.com/jdolan-exalink/openvms/migrations"
)

const service = "openvms-api"

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

	// Migrations run as the login role; the pool then drops to openvms_app so RLS applies.
	if cfg.MigrateOnStart {
		v, err := postgres.Migrate(ctx, cfg.DatabaseURL, migrations.FS, log)
		if err != nil {
			return err
		}
		log.Info("database schema ready", "version", v)
	}
	pool, err := postgres.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	sealer, err := secrets.FromEnv()
	if err != nil {
		return err
	}

	vk, err := valkeyx.Connect(cfg.ValkeyAddr)
	if err != nil {
		return err
	}
	defer vk.Close()

	nc, err := natsx.Connect(cfg.NATSURL, service, log)
	if err != nil {
		return err
	}
	defer nc.Close()
	if err := natsx.EnsureStreams(ctx, nc); err != nil {
		return err
	}

	store, err := objectstore.New(ctx, cfg.S3)
	if err != nil {
		return err
	}
	if err := store.EnsureBucket(ctx); err != nil {
		return err
	}

	st := &dbstore.Store{Pool: pool}
	inv := inventory.New(st, sealer, log)
	adapters := inventory.NewAdapters(inv)
	mediaSvc := &media.Service{Store: st, Adapters: adapters, Log: log}
	brandingSvc := &branding.Service{Store: st, Blobs: store, Log: log}
	clipWatermarkSvc := &clipwatermark.Service{Store: st, Media: mediaSvc, Branding: brandingSvc, Blobs: store, Log: log}
	handlers := &api.Handlers{
		Inv:           inv,
		Events:        &events.Service{Store: st, Blobs: store, Adapters: adapters, Log: log},
		Media:         mediaSvc,
		Branding:      brandingSvc,
		ClipWatermark: clipWatermarkSvc,
		Identity: &identity.Service{
			Store: st, Sealer: sealer, Log: log, Issuer: "OpenVMS",
			SessionTTL: cfg.SessionTTL, IdleTimeout: cfg.SessionIdle,
			MaxFailures: cfg.LoginMaxFailures, LockFor: cfg.LoginLockout,
		},
		Log:          log,
		CheckTimeout: 2 * time.Second,
		Checks: []health.Check{
			{Name: "postgres", Probe: pool.Ping},
			{Name: "valkey", Probe: func(ctx context.Context) error { return valkeyx.Ping(ctx, vk) }},
			{Name: "nats", Probe: func(ctx context.Context) error { return natsx.Ping(ctx, nc) }},
			{Name: "object_storage", Probe: store.Ping},
		},
		SchemaVersion: func(ctx context.Context) (int64, error) {
			return postgres.SchemaVersion(ctx, pool)
		},
	}
	router, err := api.NewRouter(handlers, log, api.Options{
		Queries:           db.New(pool),
		TrustForwardedFor: cfg.TrustForwardedFor,
		SessionIdle:       cfg.SessionIdle,
		Media:             (&media.Gateway{Svc: mediaSvc, Actor: api.ActorFrom, Branding: brandingSvc}).Routes(),
	})
	if err != nil {
		return err
	}

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", cfg.HTTPAddr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}
	log.Info("shutting down")
	sctx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	return srv.Shutdown(sctx)
}
