// Command edge-agent reports host metrics and accepts a bearer-authenticated update.
package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jdolan-exalink/openvms/internal/agent"
	"github.com/jdolan-exalink/openvms/internal/agent/onvifdiscover"
	"github.com/jdolan-exalink/openvms/internal/onvif"
	"github.com/jdolan-exalink/openvms/internal/platform/buildinfo"
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
	var probeHandler http.Handler
	if enabled {
		var handlerErr error
		discoveryHandler, handlerErr = onvifdiscover.NewHandler(secret, onvif.NewDiscovery(onvif.NewUDPTransport(), onvif.DiscoveryConfig{}), discoveryConfig)
		if handlerErr != nil {
			log.Error("invalid ONVIF discovery configuration", "error", handlerErr)
			os.Exit(1)
		}
		probeHandler, handlerErr = onvifdiscover.NewProbeHandler(secret, discoveryConfig, onvifdiscover.NewDeviceProbe(nil))
		if handlerErr != nil {
			log.Error("invalid ONVIF probe configuration", "error", handlerErr)
			os.Exit(1)
		}
	}
	tlsConfig, tlsEnabled, err := loadAgentTLSConfig(os.Getenv("OPENVMS_AGENT_ONVIF_TLS_LISTEN"), os.Getenv("TLS_CERT_FILE"), os.Getenv("TLS_KEY_FILE"))
	if err != nil {
		log.Error("invalid ONVIF TLS configuration", "error", err)
		os.Exit(1)
	}
	healthHandler := newAgentHealthHandler(secret, currentAgentHealthInfo(variant))
	mux := buildMux(secret, variant, sampler, discoveryHandler, replaceBinary, func() { go restart() })
	addr := env("OPENVMS_AGENT_LISTEN", "0.0.0.0:7419")
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	log.Info("listening", "addr", addr, "version", agent.Version)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	serverAddr := env("OPENVMS_SERVER_ADDR", env("OPENVMS_CONTROL_ADDR", ""))
	if serverAddr != "" {
		nodeID := env("OPENVMS_NODE_ID", "node-auto")
		worker := agent.NewWorker(agent.WorkerConfig{
			NodeID:          nodeID,
			ControlGRPCAddr: serverAddr,
			Sampler:         sampler,
			Log:             log,
		})
		go func() {
			if err := worker.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
				log.Warn("heartbeat worker stopped", "error", err)
			}
		}()
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, sCancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer sCancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	if tlsEnabled {
		tlsServer := newAgentTLSServer(tlsConfig, healthHandler, discoveryHandler, probeHandler)
		log.Info("ONVIF TLS listener enabled", "addr", tlsServer.Addr)
		if err := serveAgentServers(log, srv, tlsServer); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("agent listener stopped", "error", err)
			os.Exit(1)
		}
		return
	}
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Error("listen", "error", err)
		os.Exit(1)
	}
}

func currentAgentHealthInfo(variant string) agentHealthInfo {
	info := agentHealthInfo{
		Status:  "ok",
		Version: agent.Version,
		Build: agentBuildMetadata{
			Version:   buildinfo.Version,
			Commit:    buildinfo.Commit,
			BuildTime: buildinfo.BuildTime,
			GoVersion: buildinfo.GoVersion(),
		},
		Variant: variant,
	}
	if identity, err := agent.RunningBinaryIdentity(); err == nil {
		info.BinaryIdentity = identity
	}
	return info
}

type agentBuildMetadata struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuildTime string `json:"build_time"`
	GoVersion string `json:"go_version"`
}

type agentHealthInfo struct {
	Status         string               `json:"status"`
	Version        string               `json:"version"`
	Build          agentBuildMetadata   `json:"build"`
	Variant        string               `json:"variant"`
	BinaryIdentity agent.BinaryIdentity `json:"binary_identity,omitempty"`
}

const agentHealthMaxResponseBytes = agent.MaxHealthResponseBytes

func newAgentHealthHandler(secret string, info agentHealthInfo) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !bearer(r, secret) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		payload, err := json.Marshal(info)
		if err != nil {
			http.Error(w, "health metadata unavailable", http.StatusInternalServerError)
			return
		}
		payload = append(payload, '\n')
		if len(payload) > agentHealthMaxResponseBytes {
			http.Error(w, "health metadata unavailable", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(payload)
	})
}

func serveAgentServers(log *slog.Logger, servers ...*http.Server) error {
	return serveAgentServersWith(log, func(s *http.Server) error {
		if s.TLSConfig != nil {
			return s.ListenAndServeTLS("", "")
		}
		return s.ListenAndServe()
	}, func(ctx context.Context, s *http.Server) error {
		return s.Shutdown(ctx)
	}, servers...)
}

func serveAgentServersWith(log *slog.Logger, serve func(*http.Server) error, shutdown func(context.Context, *http.Server) error, servers ...*http.Server) error {
	type result struct {
		server *http.Server
		err    error
	}
	results := make(chan result, len(servers))
	for _, server := range servers {
		go func(s *http.Server) {
			results <- result{server: s, err: serve(s)}
		}(server)
	}
	first := <-results
	if first.err != nil && !errors.Is(first.err, http.ErrServerClosed) {
		log.Error("listener stopped", "addr", first.server.Addr, "error", first.err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, server := range servers {
		if err := shutdown(ctx, server); err != nil {
			log.Error("listener shutdown", "addr", server.Addr, "error", err)
		}
	}
	listenerErr := first.err
	if errors.Is(listenerErr, http.ErrServerClosed) || listenerErr == nil {
		listenerErr = nil
	}
	for range servers[1:] {
		result := <-results
		if listenerErr == nil && result.err != nil && !errors.Is(result.err, http.ErrServerClosed) {
			listenerErr = result.err
		}
	}
	return listenerErr
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
