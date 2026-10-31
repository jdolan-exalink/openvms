// Command edge-agent reports host metrics and accepts a bearer-authenticated update.
package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/jdolan-exalink/openvms/internal/agent"
	"github.com/jdolan-exalink/openvms/internal/agent/backup"
	"github.com/jdolan-exalink/openvms/internal/agent/camproxy"
	"github.com/jdolan-exalink/openvms/internal/agent/mtls"
	"github.com/jdolan-exalink/openvms/internal/agent/onvifdiscover"
	"github.com/jdolan-exalink/openvms/internal/agent/provisionlpr"
	"github.com/jdolan-exalink/openvms/internal/agent/tokens"
	"github.com/jdolan-exalink/openvms/internal/onvif"
	"github.com/jdolan-exalink/openvms/internal/platform/buildinfo"
	"github.com/jdolan-exalink/openvms/internal/platform/grpctls"
)

// runVersionFlag prints the build version and reports true when the arguments ask for it.
// It runs before anything reads the token or opens a listener, so the install preflight can
// query an agent binary without starting it.
func runVersionFlag(args []string, out io.Writer) bool {
	if len(args) != 1 || (args[0] != "--version" && args[0] != "-version" && args[0] != "version") {
		return false
	}
	_, _ = fmt.Fprintln(out, buildinfo.Version)
	return true
}

func main() {
	if runVersionFlag(os.Args[1:], os.Stdout) {
		return
	}
	if len(os.Args) > 1 && (os.Args[1] == "token" || os.Args[1] == "connection") {
		os.Exit(runConsoleCommand(os.Args[1:], os.Stdout, os.Stderr, time.Now))
	}
	if len(os.Args) > 1 && os.Args[1] == "rollback-apply" {
		os.Exit(runRollbackApply(os.Args[2:], os.Stdout, os.Stderr))
	}
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
	var opsHandler *onvifdiscover.OpsHandler
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
		opsHandler, handlerErr = onvifdiscover.NewOpsHandler(secret, discoveryConfig)
		if handlerErr != nil {
			log.Error("invalid ONVIF ops configuration", "error", handlerErr)
			os.Exit(1)
		}
	}
	tlsConfig, tlsEnabled, err := loadAgentTLSConfig(os.Getenv("OPENVMS_AGENT_ONVIF_TLS_LISTEN"), os.Getenv("TLS_CERT_FILE"), os.Getenv("TLS_KEY_FILE"))
	if err != nil {
		log.Error("invalid ONVIF TLS configuration", "error", err)
		os.Exit(1)
	}
	auth := &authenticator{secret: secret, tokens: tokens.Open(tokensFile())}
	healthHandler := newAgentHealthHandler(secret, currentAgentHealthInfo(variant))
	lprReceiver := provisionlpr.NewReceiver(provisionlpr.DefaultConfig(), log)
	stateDir := env("OPENVMS_AGENT_STATE_DIR", "/var/lib/openvms-agent")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		log.Warn("agent state directory unavailable: Provision-ISR registrations and captures will not be saved", "dir", stateDir, "error", err)
	}
	registry, err := provisionlpr.LoadRegistry(filepath.Join(stateDir, "provision-registry.json"))
	if err != nil {
		// The registry then refuses every camera until it is repaired on the host.
		log.Error("Provision-ISR camera registry is unreadable: every camera is refused until it is repaired", "error", err)
	}
	lprReceiver.SetRegistry(registry)
	queue, err := provisionlpr.OpenEventQueue(stateDir, 0, 0)
	if err != nil {
		// Without the queue the readings still reach the live subscribers, but central cannot collect them.
		log.Error("Provision-ISR event queue unavailable: readings will not reach central", "error", err)
	} else {
		lprReceiver.SetEventQueue(queue)
	}
	capture, err := parseCaptureMode(os.Getenv("OPENVMS_AGENT_PROVISION_CAPTURE"))
	if err != nil {
		log.Error("invalid Provision-ISR capture configuration", "error", err)
		os.Exit(1)
	}
	if capture != captureOff {
		store, err := provisionlpr.NewCaptureStore(captureDir(stateDir), 0, 0)
		if err != nil {
			// Captures are a diagnostic aid: failing to keep them must not stop the receiver.
			log.Warn("Provision-ISR captures disabled", "error", err)
		} else {
			lprReceiver.SetCaptures(store, capture == captureEverything)
		}
	}
	tcpPortStr := env("OPENVMS_AGENT_PROVISION_TCP_PORT", "8082")
	if tcpPortStr != "" && tcpPortStr != "0" && tcpPortStr != "off" {
		tcpServer, err := provisionlpr.NewTCPServer("0.0.0.0:"+tcpPortStr, lprReceiver, log)
		if err != nil {
			log.Warn("provision-isr tcp server initialization failed", "error", err)
		} else if err := tcpServer.Start(); err != nil {
			log.Warn("provision-isr tcp server failed to bind", "port", tcpPortStr, "error", err)
		} else {
			log.Info("provision-isr tcp alarm server listening", "port", tcpPortStr)
			defer func() { _ = tcpServer.Close() }()
		}
	}
	updater, err := newUpdater(stateDir)
	if err != nil {
		log.Error("agent updater unavailable: updates over the API are disabled", "error", err)
	}
	replace := func(*http.Request) error { return errors.New("agent updates are unavailable on this host") }
	if updater != nil {
		if rolled, err := updater.NoteStart(context.Background()); err != nil {
			log.Error("pending agent update record unreadable", "error", err)
		} else if rolled {
			log.Error("the new agent binary kept failing before it was confirmed; the previous binary is back")
			restart()
			time.Sleep(5 * time.Second)
			os.Exit(1)
		}
		replace = newUpdateReplacer(updater)
	}
	mux := buildMuxWithAuth(auth, variant, sampler, discoveryHandler, probeHandler, opsHandler, lprReceiver, replace, func() { go restart() })
	// The original Frigate config.yml, kept on the host so it travels with it (see internal/agent/backup).
	mountBackup(mux, auth, backup.New(stateDir))
	addr := env("OPENVMS_AGENT_LISTEN", "0.0.0.0:7419")
	// Camera web view: the control plane reaches a camera's own web interface through this route.
	mountCameraWeb(mux, auth, camproxy.New(camproxy.Config{BlockedPorts: ownListenPorts(addr, os.Getenv("OPENVMS_AGENT_ONVIF_TLS_LISTEN"))}))
	if lprReceiver != nil {
		if capture == captureEverything {
			log.Warn("Provision-ISR capture-all is on: POSTs on any path are accepted and kept; turn it off once the camera protocol is known")
		}
		mux.Handle("/", postsAnywhere(lprReceiver))
	}
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	log.Info("listening", "addr", addr, "version", agent.Version)
	if updater != nil {
		go confirmUpdateWhenHealthy(updater, addr, secret, log)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// Mutual TLS is opt-in (OPENVMS_AGENT_GRPC_ADDR). When on, the worker dials the agent
	// listener with the enrolled certificate instead of the control address below.
	// The first attempt is short: the API is served only after this point, and an agent that cannot
	// reach OpenVMS must not wait a full enrollment timeout before answering OpenVMS.
	firstTry, cancelFirstTry := context.WithTimeout(ctx, mtlsFirstTry)
	mtlsCfg, err := loadMTLS(firstTry, os.Getenv, log, time.Now)
	cancelFirstTry()
	mtlsPending := errors.Is(err, mtls.ErrEnrollUnavailable)
	if err != nil && !mtlsPending {
		log.Error("invalid agent mTLS configuration", "error", err)
		os.Exit(1)
	}
	serverAddr := env("OPENVMS_SERVER_ADDR", env("OPENVMS_CONTROL_ADDR", ""))
	if mtlsPending {
		// An agent that cannot reach OpenVMS (behind NAT) must still serve its API: OpenVMS is the one
		// that connects. The enrollment is retried in the background.
		log.Warn("cannot enroll with the OpenVMS API yet; serving the agent API and retrying", "error", err)
		go func() {
			cfg, err := retryMTLS(ctx, func() (*mtlsSettings, error) { return loadMTLS(ctx, os.Getenv, log, time.Now) }, log, sleepContext)
			switch {
			case err == nil:
				startMTLSWorker(ctx, cfg, sampler, log)
			case !errors.Is(err, context.Canceled):
				log.Error("mTLS enrollment stopped; fix the configuration and restart the agent", "error", err)
			}
		}()
	} else if mtlsCfg != nil {
		if serverAddr != "" {
			log.Warn("OPENVMS_AGENT_GRPC_ADDR is set: ignoring OPENVMS_SERVER_ADDR / OPENVMS_CONTROL_ADDR", "ignored", serverAddr)
		}
		startMTLSWorker(ctx, mtlsCfg, sampler, log)
	} else if serverAddr != "" {
		nodeID := env("OPENVMS_NODE_ID", "node-auto")
		controlTLS, controlCA, controlServerName, err := controlTLSFromEnv(os.Getenv)
		if err != nil {
			log.Error("invalid control channel TLS configuration", "error", err)
			os.Exit(1)
		}
		worker := agent.NewWorker(agent.WorkerConfig{
			NodeID:          nodeID,
			ControlGRPCAddr: serverAddr,
			Sampler:         sampler,
			Log:             log,
			TLS:             controlTLS,
			TLSCAFile:       controlCA,
			TLSServerName:   controlServerName,
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
		tlsServer := newAgentTLSServer(tlsConfig, auth, healthHandler, discoveryHandler, probeHandler, lprReceiver)
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

func buildMux(secret, variant string, sampler *agent.Sampler, discoveryHandler, probeHandler http.Handler, opsHandler *onvifdiscover.OpsHandler, lprHandler http.Handler, replace func(*http.Request) error, restartUpdate func()) *http.ServeMux {
	return buildMuxWithAuth(&authenticator{secret: secret}, variant, sampler, discoveryHandler, probeHandler, opsHandler, lprHandler, replace, restartUpdate)
}

// buildMuxWithAuth wires the agent's routes. Scopes: read (metrics, health), events (the reading queue
// and the receiver status), control (ONVIF operations, discovery, the camera registry, captures,
// configuration) and update (replacing the binary). The legacy token has every scope.
func buildMuxWithAuth(auth *authenticator, variant string, sampler *agent.Sampler, discoveryHandler, probeHandler http.Handler, opsHandler *onvifdiscover.OpsHandler, lprHandler http.Handler, replace func(*http.Request) error, restartUpdate func()) *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle("GET /v1/metrics", auth.scoped(tokens.ScopeRead, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, sampler.Current(variant))
	})))
	mux.Handle("POST /v1/update", auth.scoped(tokens.ScopeUpdate, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := replace(r); err != nil {
			http.Error(w, err.Error(), updateStatus(err))
			return
		}
		w.WriteHeader(http.StatusAccepted)
		restartUpdate()
	})))
	if discoveryHandler != nil {
		mux.Handle("POST /v1/onvif/discover", auth.scoped(tokens.ScopeControl, discoveryHandler))
	}
	if probeHandler != nil {
		mux.Handle("POST /v1/onvif/probe", auth.scoped(tokens.ScopeControl, probeHandler))
	}
	if opsHandler != nil {
		control := func(h http.HandlerFunc) http.Handler { return auth.scoped(tokens.ScopeControl, h) }
		mux.Handle("POST /v1/onvif/sync-time", control(opsHandler.HandleSyncTime))
		mux.Handle("POST /v1/onvif/ptz", control(opsHandler.HandlePTZ))
		mux.Handle("POST /v1/onvif/video-config", control(opsHandler.HandleVideoConfig))
		mux.Handle("POST /v1/onvif/video-encoding", control(opsHandler.HandleVideoEncoding))
		mux.Handle("POST /v1/onvif/video-device", control(opsHandler.HandleVideoDevice))
		mux.Handle("POST /v1/onvif/reboot", control(opsHandler.HandleReboot))
		mux.Handle("POST /v1/onvif/imaging", control(opsHandler.HandleImaging))
		mux.Handle("POST /v1/onvif/snapshot", control(opsHandler.HandleSnapshot))
		mux.Handle("POST /v1/onvif/pull-events", control(opsHandler.HandlePullEvents))
	}
	if lprHandler != nil {
		mountProvisionLPR(mux, auth, lprHandler)
	}
	configurator := provisionlpr.NewConfigurator()
	mux.Handle("POST /v1/provision-lpr/configure", auth.scoped(tokens.ScopeControl, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var p provisionlpr.CameraConfigParams
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		res := configurator.Configure(r.Context(), p)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(res)
	})))
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

func restart() {
	time.Sleep(400 * time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	cmd := exec.CommandContext(ctx, "systemctl", "restart", "openvms-agent")
	if err := cmd.Start(); err != nil {
		cancel()
		slog.Error("failed to start agent restart", "error", err)
		return
	}
	go func() {
		defer cancel()
		if err := cmd.Wait(); err != nil {
			slog.Error("agent restart failed", "error", err)
		}
	}()
}

// controlTLSFromEnv reads the opt-in TLS settings for the gRPC control channel.
// A CA file or server name without OPENVMS_CONTROL_TLS is rejected so a typo
// cannot silently leave the channel in plaintext. The CA bundle is loaded here
// so a missing or broken file stops the agent at startup instead of only
// failing later inside the background heartbeat worker.
func controlTLSFromEnv(getenv func(string) string) (enabled bool, caFile, serverName string, err error) {
	if v := strings.TrimSpace(getenv("OPENVMS_CONTROL_TLS")); v != "" {
		enabled, err = strconv.ParseBool(v)
		if err != nil {
			return false, "", "", fmt.Errorf("OPENVMS_CONTROL_TLS must be a boolean: %w", err)
		}
	}
	caFile = strings.TrimSpace(getenv("OPENVMS_CONTROL_TLS_CA_FILE"))
	serverName = strings.TrimSpace(getenv("OPENVMS_CONTROL_TLS_SERVER_NAME"))
	if _, err := grpctls.TransportCredentials(enabled, grpctls.ClientOptions{CAFile: caFile, ServerName: serverName}); errors.Is(err, grpctls.ErrOptionsWithoutTLS) {
		return false, "", "", errors.New("OPENVMS_CONTROL_TLS_CA_FILE and OPENVMS_CONTROL_TLS_SERVER_NAME require OPENVMS_CONTROL_TLS=true")
	} else if err != nil {
		return false, "", "", fmt.Errorf("control channel TLS: %w", err)
	}
	return enabled, caFile, serverName, nil
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

// mountProvisionLPR registers the Provision-ISR receiver on a mux. The camera-facing routes carry no
// token (cameras cannot send one). Everything else is behind a scope: captured requests can hold
// plates and images and the registry decides which cameras are trusted (control); the receiver status
// lists camera addresses and plates and the queued readings are what central collects (events). The
// receiver does not authenticate by itself, so every listener that serves it has to mount it here.
func mountProvisionLPR(mux *http.ServeMux, auth *authenticator, lprHandler http.Handler) {
	mux.Handle("/SendKeepalive", lprHandler)
	mux.Handle("/v1/provision-lpr/", lprHandler)
	mux.Handle("/v1/provision-lpr/event", lprHandler)
	mux.Handle("/SendLPR", lprHandler)
	mux.Handle("/LPR", lprHandler)
	mux.Handle("/Notification/LPR", lprHandler)
	control := auth.scoped(tokens.ScopeControl, lprHandler)
	mux.Handle("GET /v1/provision-lpr/captures", control)
	mux.Handle("GET /v1/provision-lpr/captures/", control)
	mux.Handle("/v1/provision-lpr/registry", control)
	mux.Handle("/v1/provision-lpr/registry/", control)
	events := auth.scoped(tokens.ScopeEvents, lprHandler)
	mux.Handle("GET /v1/provision-lpr/status", events)
	mux.Handle("/v1/provision-lpr/events", events)
	mux.Handle("/v1/provision-lpr/events/", events)
}

// startMTLSWorker runs the heartbeat worker over the enrolled mutual-TLS channel.
func startMTLSWorker(ctx context.Context, cfg *mtlsSettings, sampler *agent.Sampler, log *slog.Logger) {
	worker := agent.NewWorker(agent.WorkerConfig{
		ControlGRPCAddr: cfg.Addr,
		Sampler:         sampler,
		Log:             log,
		TLSCAFile:       cfg.CAFile,
		TLSServerName:   cfg.ServerName,
		MTLS:            cfg.Manager,
	})
	go func() {
		if err := worker.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			log.Warn("heartbeat worker stopped", "error", err)
		}
	}()
}
