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

	"github.com/jdolan-exalink/openvms/internal/agentstatus"
	"github.com/jdolan-exalink/openvms/internal/alarms"
	"github.com/jdolan-exalink/openvms/internal/clipwatermark"
	"github.com/jdolan-exalink/openvms/internal/events"
	"github.com/jdolan-exalink/openvms/internal/faces"
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
	"github.com/jdolan-exalink/openvms/internal/provision"
	"github.com/jdolan-exalink/openvms/internal/realtime/outbox"
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
	dispatcher := &outbox.Dispatcher{
		Repository: outbox.StoreRepository{Store: st},
		Publisher:  outbox.JetStreamPublisher{JS: js},
		Config:     outbox.Config{PollInterval: time.Second, Lease: 30 * time.Second, RetryBase: time.Second, RetryMax: time.Minute, MaxAttempts: 8, BatchSize: 32},
		Log:        log,
	}
	dispatcherDone := make(chan struct{})
	go func() {
		defer close(dispatcherDone)
		dispatcher.Run(ctx)
	}()
	inv := inventory.New(st, sealer, log)
	adapters := inventory.NewAdapters(inv)
	go runCameraZoneSync(ctx, inv, log)
	go runFaceSync(ctx, &faces.Syncer{Store: st, Blobs: objects, Adapters: adapters, Log: log},
		&faces.Service{Store: st, Blobs: objects, Log: log}, log)
	go runFaceReviewIngestion(ctx, &faces.ReviewIngestor{Store: st, Blobs: objects, Adapters: adapters, Log: log}, log)
	go runFaceEmbedding(ctx, st, objects, log)

	rulesSvc := rules.NewService(st, &jetstreamPublisher{js: js}, log)
	rulesSvc.ZoneResolver = rules.NewStoreZoneResolver(st)
	ruleEvaluationWorker := events.NewRuleEvaluationWorker(st, rulesSvc, log)
	ruleEvaluationDone := make(chan struct{})
	go func() {
		defer close(ruleEvaluationDone)
		ruleEvaluationWorker.Run(ctx)
	}()
	syncer := &events.Syncer{
		Store: st, Adapters: adapters, Blobs: objects, Log: log,
		Interval: cfg.EventSyncInterval, Backfill: cfg.EventBackfill, Concurrency: 8,
	}
	go syncer.Run(ctx)

	// Collects the plate readings Provision-ISR cameras report to their edge agents. An agent that
	// is unreachable or too old only gets its error recorded; it never stops the other jobs.
	agentService := &provision.Service{Store: st, Sealer: sealer, Log: log}
	agentIngest := &events.AgentIngestor{Store: st, Log: log, Interval: 15 * time.Second, Source: agentService}
	go agentIngest.Run(ctx)

	// Asks the agents that send no heartbeat (their host cannot reach OpenVMS) for their state and
	// records it as one, so they show as online like every other agent.
	agentPoller := &provision.AgentPoller{
		Service: agentService, Store: st, Recorder: &agentstatus.Recorder{Store: st}, Log: log,
	}
	go agentPoller.Run(ctx)

	// Closes open/acknowledged alarms that saw no new event for alarm_settings.auto_close_after.
	autoCloser := &alarms.AutoCloser{Store: st, Pub: &jetstreamPublisher{js: js}, Log: log}
	go autoCloser.Run(ctx)

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
	// #nosec G703 -- model path comes from trusted deployment configuration, not an HTTP request.
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
		Svc:          inv,
		Adapters:     adapters,
		Interval:     cfg.HealthInterval,
		NoSignalHold: cfg.NoSignalHold,
		Concurrency:  8,
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
	shutdownErr := srv.Shutdown(sctx)
	waitFor := func(name string, done <-chan struct{}) error {
		select {
		case <-done:
			return nil
		case <-sctx.Done():
			return fmt.Errorf("wait for %s: %w", name, sctx.Err())
		}
	}
	return errors.Join(shutdownErr,
		waitFor("realtime outbox dispatcher", dispatcherDone),
		waitFor("rule evaluation worker", ruleEvaluationDone),
	)
}

func runCameraZoneSync(ctx context.Context, svc *inventory.Service, log *slog.Logger) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := svc.ProcessCameraZoneSyncJobs(ctx, 25); err != nil && ctx.Err() == nil {
				log.WarnContext(ctx, "process camera-zone sync jobs", "error", err)
			}
		}
	}
}

// runFaceSync converges the Frigate face libraries with the central registry (FACE-02) and
// deletes queued enrollment image blobs. Both are idempotent and safe to run on every replica.
func runFaceSync(ctx context.Context, syncer *faces.Syncer, svc *faces.Service, log *slog.Logger) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := syncer.RunOnce(ctx, 25); err != nil && ctx.Err() == nil {
				log.WarnContext(ctx, "process face sync jobs", "error", err)
			}
			if err := svc.PurgeBlobs(ctx, 100); err != nil && ctx.Err() == nil {
				log.WarnContext(ctx, "purge face image blobs", "error", err)
			}
		}
	}
}

// runFaceReviewIngestion retains unclassified face attempts before Frigate's train retention removes them.
func runFaceReviewIngestion(ctx context.Context, ingestor *faces.ReviewIngestor, log *slog.Logger) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	run := func() {
		passCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
		if err := ingestor.RunOnce(passCtx, 250); err != nil && ctx.Err() == nil {
			log.WarnContext(ctx, "ingest face review sightings", "error", err)
		}
	}
	run()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			run()
		}
	}
}

// runFaceEmbedding embeds retained face crops centrally (FACE-07.2). It stays off until a model
// artifact is configured, because no weights ship with the image. The profile is the first usable
// one of Hailo-8, then CPU; a device that is visible but fails initialization or smoke falls back.
func runFaceEmbedding(ctx context.Context, st *store.Store, blobs faces.Blobs, log *slog.Logger) {
	modelPath := os.Getenv("FACE_EMBEDDING_MODEL_PATH")
	hefPath := os.Getenv("FACE_EMBEDDING_HEF_PATH")
	modelID := os.Getenv("FACE_EMBEDDING_MODEL_ID")
	if (modelPath == "" && hefPath == "") || modelID == "" {
		log.Info("face embedding disabled: set FACE_EMBEDDING_MODEL_PATH (or FACE_EMBEDDING_HEF_PATH) and FACE_EMBEDDING_MODEL_ID")
		return
	}
	libraryPath := os.Getenv("ONNXRUNTIME_SHARED_LIBRARY")
	if libraryPath == "" {
		libraryPath = "/opt/openvms/libonnxruntime.so.1.29.0"
	}
	var factories []faces.EmbeddingRuntimeFactory
	if hefPath != "" {
		factories = append(factories, faces.Hailo8EmbeddingFactory{Config: faces.Hailo8EmbeddingConfig{HEFPath: hefPath}})
	}
	if modelPath != "" {
		factories = append(factories, faces.CPUEmbeddingFactory{Config: faces.CPUEmbeddingConfig{ModelPath: modelPath, LibraryPath: libraryPath}})
	}
	selection, err := faces.SelectEmbeddingRuntime(ctx, factories)
	if err != nil {
		log.Warn("face embedding disabled: no usable runtime", "error", err)
		return
	}
	defer func() { _ = selection.Runtime.Close() }()
	var detector faces.FaceDetector
	if detectorPath := os.Getenv("FACE_DETECTOR_MODEL_PATH"); detectorPath == "" {
		log.Info("face detector gate disabled: set FACE_DETECTOR_MODEL_PATH to keep crops that are not faces out of the suggestions")
	} else if _, statErr := os.Stat(detectorPath); os.IsNotExist(statErr) { //nolint:gosec // operator-controlled deployment config, not request input.
		// An upgrade that has not fetched the detector yet keeps embedding, without the gate.
		log.Warn("face detector gate disabled: model file not found; run deploy/faces/prepare-arcface-model.sh and restart the worker", "path", detectorPath)
	} else {
		// A detector that exists but cannot start pauses embedding: running without it would quietly
		// embed crops that are not faces again.
		detector, err = faces.OpenYuNet(faces.YuNetConfig{ModelPath: detectorPath, LibraryPath: libraryPath})
		if err != nil {
			log.Error("face embedding paused: face detector unavailable", "error", err)
			return
		}
		defer func() { _ = detector.Close() }()
	}
	log.Info("face embedding ready", "profile", selection.Profile, "model_id", modelID, "detector", detector != nil)
	embedder := &faces.Embedder{Store: st, Blobs: blobs, Runtime: selection.Runtime, ModelID: modelID, Detector: detector, Log: log}
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		passCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		n, err := embedder.RunOnce(passCtx, 100)
		cancel()
		if err != nil && ctx.Err() == nil {
			log.WarnContext(ctx, "embed face sightings", "error", err)
		}
		if n > 0 {
			log.InfoContext(ctx, "face sightings embedded", "count", n)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
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
