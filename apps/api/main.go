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

	// PDW-7: tenant branding lets an operator pick any IANA time zone for the watermark
	// (internal/branding.ResolveLocation, time.LoadLocation). This binary runs distroless
	// (gcr.io/distroless/static-debian12, see deploy/docker/go.Dockerfile's "runtime" target).
	// Verified directly (docker create + docker cp) that this particular base image currently
	// does ship /usr/share/zoneinfo, so time.LoadLocation already works there without this
	// import — but that is an incidental property of gcr.io/distroless/static-debian12 today,
	// not something deploy/docker/go.Dockerfile installs or pins, and it is not true of every
	// minimal base image (e.g. gcr.io/distroless/static, without "-debian12", ships none).
	// Embedding the full IANA tzdata into the binary removes that dependency on the base
	// image's own filesystem entirely, so a future base image change can never silently break
	// every non-UTC watermark.
	_ "time/tzdata"

	"github.com/nats-io/nats.go/jetstream"
	"google.golang.org/grpc/credentials"

	"github.com/jdolan-exalink/openvms/internal/alarms"
	"github.com/jdolan-exalink/openvms/internal/api"
	"github.com/jdolan-exalink/openvms/internal/branding"
	"github.com/jdolan-exalink/openvms/internal/clipwatermark"
	"github.com/jdolan-exalink/openvms/internal/control"
	"github.com/jdolan-exalink/openvms/internal/events"
	"github.com/jdolan-exalink/openvms/internal/health"
	"github.com/jdolan-exalink/openvms/internal/identity"
	"github.com/jdolan-exalink/openvms/internal/inventory"
	"github.com/jdolan-exalink/openvms/internal/maps"
	"github.com/jdolan-exalink/openvms/internal/media"
	"github.com/jdolan-exalink/openvms/internal/mediasession"
	"github.com/jdolan-exalink/openvms/internal/notify"
	"github.com/jdolan-exalink/openvms/internal/platform/buildinfo"
	"github.com/jdolan-exalink/openvms/internal/platform/config"
	"github.com/jdolan-exalink/openvms/internal/platform/grpctls"
	"github.com/jdolan-exalink/openvms/internal/platform/logging"
	"github.com/jdolan-exalink/openvms/internal/platform/natsx"
	"github.com/jdolan-exalink/openvms/internal/platform/objectstore"
	"github.com/jdolan-exalink/openvms/internal/platform/postgres"
	"github.com/jdolan-exalink/openvms/internal/platform/telemetry"
	"github.com/jdolan-exalink/openvms/internal/platform/valkeyx"
	"github.com/jdolan-exalink/openvms/internal/provision"
	"github.com/jdolan-exalink/openvms/internal/realtime"
	"github.com/jdolan-exalink/openvms/internal/rules"
	"github.com/jdolan-exalink/openvms/internal/search"
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
	js, err := jetstream.New(nc)
	if err != nil {
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
	inv.Blobs = store
	adapters := inventory.NewAdapters(inv)
	mediaSvc := &media.Service{Store: st, Adapters: adapters, Log: log}
	brandingSvc := &branding.Service{Store: st, Blobs: store, Log: log}
	clipWatermarkSvc := &clipwatermark.Service{Store: st, Media: mediaSvc, Branding: brandingSvc, Blobs: store, Log: log}
	alarmsSvc := &alarms.Service{Store: st, Pub: &jetstreamPublisher{js: js}, Log: log}
	mapsSvc := &maps.Service{Store: st, Blobs: store, Config: maps.DefaultConfig(), Log: log}
	// Land the default view where the server actually is (public-IP geolocation, with an
	// OPENVMS_MAPS_CENTER override); the lookup is best-effort and never blocks startup.
	mapsSvc.Config.DefaultCenter = maps.DetectServerCenter(ctx, log)
	handlers := &api.Handlers{
		Inv:           inv,
		Provision:     provision.New(inv, st, sealer, log),
		Events:        &events.Service{Store: st, Blobs: store, Adapters: adapters, Log: log},
		Alarms:        alarmsSvc,
		Media:         mediaSvc,
		Maps:          mapsSvc,
		Features:      cfg.Features,
		Branding:      brandingSvc,
		ClipWatermark: clipWatermarkSvc,
		Search:        &search.Service{Store: st},
		Rules:         rules.NewService(st, &jetstreamPublisher{js: js}, log),
		Notify: notify.NewService(st, sealer, notify.Deps{
			WahaBaseURL: cfg.Waha.BaseURL, WahaAPIKey: cfg.Waha.APIKey,
		}, log),
		Identity: &identity.Service{
			Store: st, Sealer: sealer, Log: log, Issuer: "OpenVMS",
			SessionTTL: cfg.SessionTTL, IdleTimeout: cfg.SessionIdle,
			MaxFailures: cfg.LoginMaxFailures, LockFor: cfg.LoginLockout,
		},
		Log:          log,
		WorkerURL:    cfg.WorkerURL,
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
	// The push feed: one ordered consumer per stream feeds the hub; the handler filters per
	// connection by tenant and by the same authorized-ID queries the list endpoints use.
	rtRoutes := realtime.DefaultRoutes()
	rtHub := realtime.NewHub(realtime.HubConfig{
		Authorizer: realtime.NewCachedAuthorizer(realtime.StoreLoader(st), 30*time.Second),
		Log:        log,
		Routes:     rtRoutes,
	})
	defer rtHub.Close()
	go (&realtime.Feed{Source: realtime.JetStreamSource{JS: js}, Hub: rtHub, Routes: rtRoutes, Log: log}).Run(ctx)
	sessionsMgr := mediasession.NewManager(15 * time.Minute)
	defer sessionsMgr.Close()

	var grpcCreds credentials.TransportCredentials
	if cfg.GRPCTLSEnabled() {
		grpcCreds, err = grpctls.ServerCredentials(cfg.GRPCTLSCertFile, cfg.GRPCTLSKeyFile)
		if err != nil {
			return fmt.Errorf("grpc tls: %w", err)
		}
		log.Info("gRPC control channel TLS enabled")
	} else {
		log.Warn("gRPC control channel is plaintext; set GRPC_TLS_CERT_FILE and GRPC_TLS_KEY_FILE to enable TLS")
	}
	grpcCtrl := control.NewServer(control.Config{
		Addr:        cfg.GRPCAddr,
		Identity:    handlers.Identity,
		Inventory:   inv,
		Realtime:    rtHub,
		Sessions:    sessionsMgr,
		Log:         log,
		Features:    cfg.Features.EnabledList(),
		Credentials: grpcCreds,
	})
	go func() {
		if err := grpcCtrl.Start(cfg.GRPCAddr); err != nil {
			log.Warn("grpc control server stopped", "error", err)
		}
	}()

	handlers.RealtimeTracker = rtHub
	handlers.GRPCTracker = grpcCtrl
	handlers.SessionsTracker = sessionsMgr

	router, err := api.NewRouter(handlers, log, api.Options{
		Queries:                     db.New(pool),
		TrustForwardedFor:           cfg.TrustForwardedFor,
		CredentialTrustedProxyCIDRs: cfg.CredentialTrustedProxyCIDRs,
		SessionIdle:                 cfg.SessionIdle,
		Media: (&media.Gateway{
			Svc: mediaSvc, Actor: api.ActorFrom, Branding: brandingSvc, Session: api.RevalidatorFrom,
			Live: media.LiveConfig{
				AuditWindow: cfg.LiveAuditWindow, RevalidateInterval: cfg.LiveRevalidateInterval,
				PingInterval: cfg.LivePingInterval, PongWait: cfg.LivePongWait,
			},
		}).Routes(),
		Realtime: &realtime.Handler{Hub: rtHub, Actor: api.ActorFrom, Session: api.RevalidatorFrom, Log: log},
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
		grpcCtrl.GracefulStop()
		return err
	case <-ctx.Done():
	}
	log.Info("shutting down")
	grpcCtrl.GracefulStop()
	sctx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	return srv.Shutdown(sctx)
}

type jetstreamPublisher struct {
	js jetstream.JetStream
}

func (p *jetstreamPublisher) Publish(ctx context.Context, subject string, data []byte) error {
	_, err := p.js.Publish(ctx, subject, data)
	return err
}
