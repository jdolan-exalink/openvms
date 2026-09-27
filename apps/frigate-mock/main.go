// Command frigate-mock simulates one Frigate server for development and CI.
//
// Environment:
//
//	MOCK_NAME           server name, used in logs (default frigate-mock)
//	MOCK_CAMERAS        comma list; suffix "+lpr" marks an LPR camera, ":zone|zone" adds zones
//	                    e.g. "acceso_norte+lpr:entrada|salida,plaza:centro,cementerio"
//	MOCK_TOPIC_PREFIX   MQTT topic prefix (default frigate)
//	MOCK_MQTT_URL       broker, e.g. tcp://mosquitto:1883; empty disables MQTT
//	MOCK_INTERVAL       time between new review items (default 20s)
//	MOCK_SEED           finished reviews to create over the past 24h (default 200)
//	MOCK_USER / MOCK_PASSWORD   credentials for POST /api/login (default admin / admin)
//	MOCK_REQUIRE_AUTH   require login like port 8971 (default true)
//	HTTP_ADDR           listen address (default :8971)
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/jdolan-exalink/openvms/internal/frigatemock"
	"github.com/jdolan-exalink/openvms/internal/platform/logging"
)

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "error", err)
		os.Exit(1)
	}
}

func run() error {
	name := env("MOCK_NAME", "frigate-mock")
	log := logging.New("frigate-mock", env("LOG_LEVEL", "info")).With("mock", name)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cameras, err := frigatemock.ParseCameras(env("MOCK_CAMERAS", "acceso_norte+lpr:entrada|salida,plaza:centro,cementerio"))
	if err != nil {
		return err
	}
	prefix := env("MOCK_TOPIC_PREFIX", "frigate")
	interval, err := time.ParseDuration(env("MOCK_INTERVAL", "20s"))
	if err != nil {
		return fmt.Errorf("MOCK_INTERVAL: %w", err)
	}
	seed, _ := strconv.Atoi(env("MOCK_SEED", "200"))
	requireAuth, _ := strconv.ParseBool(env("MOCK_REQUIRE_AUTH", "true"))

	store := frigatemock.NewStore(5000)
	gen := &frigatemock.Generator{Cameras: cameras, Store: store, TopicPrefix: prefix, Interval: interval, Log: log}
	gen.Seed(seed, 24*time.Hour, time.Now())

	if url := os.Getenv("MOCK_MQTT_URL"); url != "" {
		pub, err := frigatemock.ConnectMQTT(url, name, prefix, log)
		if err != nil {
			return err
		}
		defer pub.Close()
		gen.Publisher = pub
	}
	go gen.Run(ctx)

	srv := &frigatemock.Server{
		Name: name, Version: "0.17.2-mock", TopicPrefix: prefix, Cameras: cameras, Store: store,
		User: env("MOCK_USER", "admin"), Password: env("MOCK_PASSWORD", "admin"),
		RequireAuth: requireAuth, StartedAt: time.Now(),
	}
	addr := env("HTTP_ADDR", ":8971")
	httpSrv := &http.Server{Addr: addr, Handler: srv.Handler(), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		log.Info("listening", "addr", addr, "cameras", len(cameras), "seeded_reviews", seed)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("http server", "error", err)
			stop()
		}
	}()
	<-ctx.Done()
	sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return httpSrv.Shutdown(sctx)
}

func env(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}
