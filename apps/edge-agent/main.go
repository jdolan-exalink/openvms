// Command edge-agent reports host metrics and accepts a bearer-authenticated update.
package main

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/jdolan-exalink/openvms/internal/agent"
	"github.com/jdolan-exalink/openvms/internal/agent/onvifdiscover"
	"github.com/jdolan-exalink/openvms/internal/onvif"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	token, err := os.ReadFile(env("OPENVMS_AGENT_TOKEN_FILE", "/etc/openvms/agent.token"))
	if err != nil {
		log.Error("read token file", "error", err)
		os.Exit(1)
	}
	secret := strings.TrimSpace(string(token))
	if secret == "" {
		log.Error("token file is empty")
		os.Exit(1)
	}
	sampler := agent.NewSampler(agent.Paths{
		CCTV: env("OPENVMS_CCTV_PATH", "/mnt/cctv"),
		DB:   env("OPENVMS_DB_PATH", "/opt/frigate/config"),
	})
	variant := env("OPENVMS_AGENT_VARIANT", "")
	discoveryConfig, enabled, err := onvifdiscover.LoadConfig(os.Getenv("ONVIF_DISCOVERY_INTERFACES"), os.Getenv("ONVIF_ALLOWED_CIDRS"))
	if err != nil {
		log.Error("invalid ONVIF discovery configuration", "error", err)
		os.Exit(1)
	}
	var discoveryHandler http.Handler
	if enabled {
		var handlerErr error
		discoveryHandler, handlerErr = onvifdiscover.NewHandler(secret, onvif.NewDiscovery(onvif.NewUDPTransport(), onvif.DiscoveryConfig{}), discoveryConfig)
		if handlerErr != nil {
			log.Error("invalid ONVIF discovery configuration", "error", handlerErr)
			os.Exit(1)
		}
	}
	mux := buildMux(secret, variant, sampler, discoveryHandler, replaceBinary, func() { go restart() })
	addr := env("OPENVMS_AGENT_LISTEN", "0.0.0.0:7419")
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	log.Info("listening", "addr", addr, "version", agent.Version)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Error("listen", "error", err)
		os.Exit(1)
	}
}

func buildMux(secret, variant string, sampler *agent.Sampler, discoveryHandler http.Handler, replace func(*http.Request) error, restartUpdate func()) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/metrics", func(w http.ResponseWriter, r *http.Request) {
		if !bearer(r, secret) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		writeJSON(w, sampler.Current(variant))
	})
	mux.HandleFunc("POST /v1/update", func(w http.ResponseWriter, r *http.Request) {
		if !bearer(r, secret) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if err := replace(r); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusAccepted)
		restartUpdate()
	})
	if discoveryHandler != nil {
		mux.Handle("POST /v1/onvif/discover", discoveryHandler)
	}
	return mux
}

func bearer(r *http.Request, secret string) bool {
	const prefix = "Bearer "
	got := r.Header.Get("Authorization")
	if !strings.HasPrefix(got, prefix) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(strings.TrimPrefix(got, prefix)), []byte(secret)) == 1
}

func replaceBinary(r *http.Request) error {
	data, err := io.ReadAll(io.LimitReader(r.Body, 80<<20))
	if err != nil {
		return err
	}
	if len(data) < 4 || string(data[:4]) != "\x7fELF" {
		return errors.New("update payload is not an executable")
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	next := exe + ".new"
	if err := os.WriteFile(next, data, 0o755); err != nil {
		return err
	}
	return os.Rename(next, exe)
}

func restart() {
	time.Sleep(400 * time.Millisecond)
	cmd := exec.Command("systemctl", "restart", "openvms-agent")
	_ = cmd.Start()
}

func env(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func writeJSON(w http.ResponseWriter, snap agent.Snapshot) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(snap)
}
