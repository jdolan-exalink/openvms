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
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/google/uuid"
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
	"github.com/jdolan-exalink/openvms/internal/store/db"
	"github.com/jdolan-exalink/openvms/internal/vehicle"
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

	exportLimiter := media.NewBandwidthLimiter(
		int64(cfg.ExportGlobalBandwidthMbps)*125000,
		int64(cfg.ExportServerBandwidthMbps)*125000,
	)
	jobManager := &media.ExportJobManager{
		Store:      st,
		Adapters:   adapters,
		StorageDir: cfg.ExportStoragePath,
		Limiter:    exportLimiter,
		Interval:   2 * time.Second,
		Log:        log,
	}
	go jobManager.Run(ctx)

	// clipWorker (PDW-4) burns the plate detail watermark into requested clips via ffmpeg,
	// installed only in this image's runtime stage (deploy/docker/go.Dockerfile
	// runtime-ffmpeg target) — the api/other images stay distroless without it.
	clipWorker := &clipwatermark.Worker{Store: st, Adapters: adapters, Blobs: objects, Log: log}
	go clipWorker.Run(ctx)

	vehicles := &vehicle.Worker{
		Store: st, Blobs: objects, Log: log, Rules: rulesSvc,
		Crops: func(ctx context.Context, serverID uuid.UUID, detectionID string) ([]byte, error) {
			return objectCrop(ctx, st, adapters, serverID, detectionID)
		},
		Rects: func(ctx context.Context, serverID uuid.UUID, detectionID string) (string, [4]float64, [4]float64, bool) {
			return objectRect(ctx, st, adapters, serverID, detectionID)
		},
		Publish: func(ctx context.Context, tenantID, eventID uuid.UUID) {
			data, _ := json.Marshal(map[string]string{"event_id": eventID.String(), "tenant_id": tenantID.String()})
			if err := nc.Publish("event.attributes."+tenantID.String(), data); err != nil {
				log.WarnContext(ctx, "publish vehicle attributes", "error", err)
			}
		},
	}
	modelPath := os.Getenv("VEHICLE_BODY_MODEL")
	if modelPath == "" {
		modelPath = "/opt/openvms/vehicle-body.onnx"
	}
	capacity := &atomic.Value{}
	capacity.Store(bodyReport{Measuring: false, Ready: false})
	if _, err := os.Stat(modelPath); err == nil {
		libraryPath := os.Getenv("ONNXRUNTIME_SHARED_LIBRARY")
		if libraryPath == "" {
			libraryPath = "/opt/openvms/libonnxruntime.so.1.29.0"
		}
		if body, err := vehicle.OpenBodyClassifier(modelPath, libraryPath); err != nil {
			log.Warn("vehicle body classifier disabled", "error", err)
		} else {
			vehicles.Body = body
			capacity.Store(bodyReport{Measuring: true, Ready: true, Model: "autolens-efficientnet-b2", Threads: 2})
			go func() {
				ms, err := body.Benchmark()
				if err != nil || ms <= 0 {
					log.Warn("vehicle body benchmark", "error", err)
					capacity.Store(bodyReport{Ready: true, Model: "autolens-efficientnet-b2", Threads: 2})
					return
				}
				capacity.Store(bodyReport{
					Ready: true, Model: "autolens-efficientnet-b2", Threads: 2,
					LatencyMs: ms, CropsPerSecond: 1000 / ms, CropsPerMinute: 60000 / ms,
				})
				log.Info("vehicle body benchmark", "latency_ms", ms, "crops_per_second", 1000/ms)
			}()
			log.Info("vehicle body classifier ready", "model", modelPath)
		}
	}
	for range 4 {
		go vehicles.Run(ctx)
	}

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
		OnCameraChange: func(ctx context.Context, c inventory.CameraStatusChange) {
			log.InfoContext(ctx, "camera status changed", "camera_id", c.CameraID, "server_id", c.ServerID, "from", c.From, "to", c.To)
			data, _ := json.Marshal(c)
			if _, err := js.Publish(ctx, fmt.Sprintf("camera.status.%s", c.TenantID), data); err != nil {
				log.ErrorContext(ctx, "publish camera status", "error", err)
			}
		},
	}
	go poller.Run(ctx)

	srv := &http.Server{Addr: cfg.HTTPAddr, Handler: probes(func() bodyReport {
		v, _ := capacity.Load().(bodyReport)
		return v
	}, pool.Ping, func(ctx context.Context) error { return natsx.Ping(ctx, nc) }), ReadHeaderTimeout: 5 * time.Second}
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

type bodyReport struct {
	Ready          bool    `json:"ready"`
	Measuring      bool    `json:"measuring"`
	Model          string  `json:"model"`
	LatencyMs      float64 `json:"latency_ms"`
	CropsPerSecond float64 `json:"crops_per_second"`
	CropsPerMinute float64 `json:"crops_per_minute"`
	Threads        int     `json:"threads"`
}

// probes serves /health/live and /health/ready for the container orchestrator.
func probes(capacity func() bodyReport, checks ...func(context.Context) error) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /capacity", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(capacity())
	})
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

func objectCrop(ctx context.Context, st *store.Store, adapters *inventory.Adapters, serverID uuid.UUID, detectionID string) ([]byte, error) {
	var srv db.FrigateServer
	err := st.Tx(ctx, store.AllTenants, func(q *db.Queries) error {
		var err error
		srv, err = q.GetServerRow(ctx, serverID)
		return err
	})
	if err != nil {
		return nil, err
	}
	ad, err := adapters.Get(ctx, srv)
	if err != nil {
		return nil, err
	}
	resp, err := ad.Media().Open(ctx, "/api/events/"+url.PathEscape(detectionID)+"/snapshot.jpg", url.Values{"crop": {"1"}}, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("snapshot status %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 8<<20))
}

func objectRect(ctx context.Context, st *store.Store, adapters *inventory.Adapters, serverID uuid.UUID, detectionID string) (string, [4]float64, [4]float64, bool) {
	var srv db.FrigateServer
	err := st.Tx(ctx, store.AllTenants, func(q *db.Queries) error {
		var err error
		srv, err = q.GetServerRow(ctx, serverID)
		return err
	})
	if err != nil {
		return "", [4]float64{}, [4]float64{}, false
	}
	ad, err := adapters.Get(ctx, srv)
	if err != nil {
		return "", [4]float64{}, [4]float64{}, false
	}
	resp, err := ad.Media().Open(ctx, "/api/events/"+url.PathEscape(detectionID), nil, nil)
	if err != nil {
		return "", [4]float64{}, [4]float64{}, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", [4]float64{}, [4]float64{}, false
	}
	var payload struct {
		Label string `json:"label"`
		Data  struct {
			Box    []float64 `json:"box"`
			Region []float64 `json:"region"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&payload); err != nil {
		return "", [4]float64{}, [4]float64{}, false
	}
	box, okBox := four(payload.Data.Box)
	region, okRegion := four(payload.Data.Region)
	if payload.Label == "" || !okBox || !okRegion {
		return "", [4]float64{}, [4]float64{}, false
	}
	return payload.Label, box, region, true
}

func four(v []float64) ([4]float64, bool) {
	if len(v) != 4 {
		return [4]float64{}, false
	}
	return [4]float64{v[0], v[1], v[2], v[3]}, true
}

// jetstreamPublisher adapts JetStream to the rules service's Publisher.
type jetstreamPublisher struct {
	js jetstream.JetStream
}

func (p *jetstreamPublisher) Publish(ctx context.Context, subject string, data []byte) error {
	_, err := p.js.Publish(ctx, subject, data)
	return err
}
