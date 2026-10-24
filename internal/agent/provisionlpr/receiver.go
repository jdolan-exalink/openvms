package provisionlpr

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
)

// EventSubscriber receives normalized LPR events asynchronously.
type EventSubscriber func(event LPREvent)

// Receiver handles HTTP requests from Provision-ISR cameras.
type Receiver struct {
	cfg         Config
	parser      *Parser
	dedup       *Deduplicator
	heartbeat   *HeartbeatManager
	subscribers []EventSubscriber
	subMu       sync.RWMutex
	log         *slog.Logger
	eventsQueue chan LPREvent
	captures    *CaptureStore
	captureAll  bool
	registry    *Registry
}

// NewReceiver creates a new Receiver with the given config and dependencies.
func NewReceiver(cfg Config, log *slog.Logger) *Receiver {
	if cfg.HeartbeatTimeout <= 0 {
		cfg.HeartbeatTimeout = 60 * time.Second
	}
	if cfg.DeduplicationWindow <= 0 {
		cfg.DeduplicationWindow = 15 * time.Second
	}
	if cfg.MaxRequestBodyBytes <= 0 {
		cfg.MaxRequestBodyBytes = 10 << 20
	}
	if log == nil {
		log = slog.Default()
	}

	r := &Receiver{
		cfg:         cfg,
		parser:      NewParser(),
		dedup:       NewDeduplicator(cfg.DeduplicationWindow),
		heartbeat:   NewHeartbeatManager(cfg.HeartbeatTimeout),
		log:         log,
		eventsQueue: make(chan LPREvent, 256),
	}

	go r.processEvents()
	return r
}

// SetCaptures makes the receiver keep the requests it cannot parse, so an unknown firmware or
// camera model is recorded instead of silently dropped. With all set it keeps every event request,
// parsed or not, which is how the real protocol of a camera is learned. Call it before serving.
func (r *Receiver) SetCaptures(store *CaptureStore, all bool) {
	r.captures = store
	r.captureAll = all && store != nil
}

// SetRegistry makes the receiver identify cameras by the address they connect from and, once the
// registry enforces, refuse the ones that are not registered. Without a registry every request is
// accepted, as before. Call it before serving.
func (r *Receiver) SetRegistry(reg *Registry) { r.registry = reg }

// Subscribe registers a callback to receive normalized LPR events.
func (r *Receiver) Subscribe(sub EventSubscriber) {
	if sub == nil {
		return
	}
	r.subMu.Lock()
	defer r.subMu.Unlock()
	r.subscribers = append(r.subscribers, sub)
}

// Heartbeat returns the underlying HeartbeatManager.
func (r *Receiver) Heartbeat() *HeartbeatManager {
	return r.heartbeat
}

// ServeHTTP routes incoming requests to the keepalive or event handlers.
func (r *Receiver) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	path := req.URL.Path

	switch {
	case path == "/SendKeepalive" || path == "/v1/provision-lpr/keepalive":
		r.handleKeepalive(w, req)
	case path == "/v1/provision-lpr/status":
		r.handleStatus(w, req)
	case path == "/v1/provision-lpr/captures" || strings.HasPrefix(path, "/v1/provision-lpr/captures/"):
		r.handleCaptures(w, req)
	case path == "/v1/provision-lpr/registry" || strings.HasPrefix(path, "/v1/provision-lpr/registry/"):
		r.handleRegistry(w, req)
	case req.Method == http.MethodPost:
		r.handleEvent(w, req)
	default:
		http.NotFound(w, req)
	}
}

func (r *Receiver) handleKeepalive(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost && req.Method != http.MethodGet {
		w.Header().Set("Allow", "POST, GET")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	now := time.Now()
	clientIP := extractClientIP(req)
	registered, ok := r.admit(w, req, nil, now)
	if !ok {
		return
	}
	camID := req.URL.Query().Get("camera_id")
	if camID == "" {
		camID = req.URL.Query().Get("dev_id")
	}
	if registered != nil {
		camID = registered.CameraID
	}

	r.heartbeat.RecordKeepalive(camID, clientIP, now)
	r.log.Debug("provision-isr keepalive received", "camera_id", camID, "camera_ip", clientIP)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

func (r *Receiver) handleEvent(w http.ResponseWriter, req *http.Request) {
	now := time.Now()
	// Read bounded body
	body, err := io.ReadAll(io.LimitReader(req.Body, r.cfg.MaxRequestBodyBytes+1))
	if err != nil || int64(len(body)) > r.cfg.MaxRequestBodyBytes {
		http.Error(w, "payload too large or unreadable", http.StatusRequestEntityTooLarge)
		return
	}

	registered, ok := r.admit(w, req, body, now)
	if !ok {
		return
	}

	// Always reply HTTP 200 quickly to Provision camera to prevent retry storms
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))

	// Parse in background queue or worker
	event, err := r.parser.Parse(req, body, now)
	if err != nil {
		r.log.Warn("provision-isr payload unparseable", "error", err, "path", req.URL.Path, "size", len(body))
		r.capture(req, body, CaptureUnparsed, err.Error(), now)
		// Still record keepalive from the camera IP
		keepaliveID := ""
		if registered != nil {
			keepaliveID = registered.CameraID
		}
		r.heartbeat.RecordKeepalive(keepaliveID, extractClientIP(req), now)
		return
	}
	if registered != nil && event.CameraID != registered.CameraID {
		// The registered ID is the identity; what the payload claimed is kept for audit only.
		reported := event.CameraID
		event.ReportedCameraID = &reported
		event.CameraID = registered.CameraID
	}

	if r.captureAll {
		r.capture(req, body, CaptureAll, "", now)
	}

	// Deduplicate
	if r.dedup.IsDuplicate(event.CameraID, event.Plate, now) {
		r.log.Debug("provision-isr duplicate plate suppressed", "camera_id", event.CameraID, "plate", event.Plate)
		return
	}

	// Update heartbeat with reading
	r.heartbeat.RecordFullEvent(event, now)
	r.log.Info("provision-isr lpr event accepted", "camera_id", event.CameraID, "plate", event.Plate, "ip", event.CameraIP)

	// Enqueue for subscribers
	select {
	case r.eventsQueue <- event:
	default:
		r.log.Warn("provision-isr events queue full; dropping event", "camera_id", event.CameraID, "plate", event.Plate)
	}
}

// admit decides whether the peer of a request may feed the receiver and which registered camera it
// is. Without a registry everyone is admitted and nobody is identified. With one, a registered address
// is identified by its registered ID; an unregistered one is noted so an operator can see it, and is
// turned away with 403 once the registry enforces (the request is kept as evidence when captures
// are on). The peer address is the only identity used: the registry cannot be fooled by headers.
func (r *Receiver) admit(w http.ResponseWriter, req *http.Request, body []byte, now time.Time) (*RegisteredCamera, bool) {
	if r.registry == nil {
		return nil, true
	}
	ip := extractClientIP(req)
	if cam, known := r.registry.Identify(ip); known {
		return &cam, true
	}
	enforcing := r.registry.Enforcing()
	r.registry.NoteUnregistered(ip, req.URL.Path, enforcing, now)
	if !enforcing {
		return nil, true
	}
	if body != nil {
		r.capture(req, body, CaptureUnregistered, "source address is not a registered camera", now)
	}
	r.log.Warn("provision-isr request from an unregistered address refused", "ip", ip, "path", req.URL.Path)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	_, _ = w.Write([]byte(`{"error":"camera not registered"}`))
	return nil, false
}

// capture keeps a request when a store is configured. A failure to store never affects the reply
// the camera already received.
func (r *Receiver) capture(req *http.Request, body []byte, reason, parseErr string, now time.Time) {
	if r.captures == nil {
		return
	}
	if _, err := r.captures.Save(req, body, reason, parseErr, extractClientIP(req), now); err != nil {
		r.log.Warn("provision-isr capture not stored", "error", err, "path", req.URL.Path)
	}
}

// handleCaptures serves the stored captures: the list, one description, or the original body.
// The receiver itself does not authenticate; the agent mux puts these routes behind the bearer token.
func (r *Receiver) handleCaptures(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if r.captures == nil {
		http.Error(w, "captures are not enabled", http.StatusNotFound)
		return
	}
	rest := strings.TrimPrefix(strings.TrimPrefix(req.URL.Path, "/v1/provision-lpr/captures"), "/")
	if rest == "" {
		list, err := r.captures.List(100)
		if err != nil {
			http.Error(w, "captures unavailable", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"captures": list})
		return
	}
	id, wantBody := strings.CutSuffix(rest, "/body")
	meta, body, err := r.captures.Get(id)
	if err != nil {
		http.NotFound(w, req)
		return
	}
	if wantBody {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", `attachment; filename="`+meta.ID+`.bin"`)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		_, _ = w.Write(body) //nolint:gosec // served as an octet-stream attachment with nosniff, behind the agent token.
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(meta)
}

func (r *Receiver) handleStatus(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	statuses := r.heartbeat.AllStatuses(time.Now())
	out := map[string]any{"cameras": statuses}
	if r.registry != nil {
		out["registry"] = r.registry.Snapshot()
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

func (r *Receiver) processEvents() {
	for event := range r.eventsQueue {
		r.subMu.RLock()
		subscribers := append([]EventSubscriber(nil), r.subscribers...)
		r.subMu.RUnlock()

		for _, sub := range subscribers {
			sub(event)
		}
	}
}
