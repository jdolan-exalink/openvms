package provisionlpr

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
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
	camID := req.URL.Query().Get("camera_id")
	if camID == "" {
		camID = req.URL.Query().Get("dev_id")
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

	// Always reply HTTP 200 quickly to Provision camera to prevent retry storms
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))

	// Parse in background queue or worker
	event, err := r.parser.Parse(req, body, now)
	if err != nil {
		r.log.Warn("provision-isr payload unparseable", "error", err, "path", req.URL.Path, "size", len(body))
		// Still record keepalive from the camera IP
		r.heartbeat.RecordKeepalive("", extractClientIP(req), now)
		return
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

func (r *Receiver) handleStatus(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	statuses := r.heartbeat.AllStatuses(time.Now())
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"cameras": statuses,
	})
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
