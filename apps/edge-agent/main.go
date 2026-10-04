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
		if err := replaceBinary(r); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusAccepted)
		go restart()
	})
	addr := env("OPENVMS_AGENT_LISTEN", "0.0.0.0:7419")
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	log.Info("listening", "addr", addr, "version", agent.Version)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Error("listen", "error", err)
		os.Exit(1)
	}
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
