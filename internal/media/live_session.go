package media

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// LiveConfig tunes the live websocket gateway. Zero values mean the defaults below, so a
// Gateway built without it (tests, older wiring) behaves sensibly.
type LiveConfig struct {
	// AuditWindow collapses LIVE_VIEWED: a user reconnecting to the same camera within this
	// window of their previous connection (sliding) is the same viewing session and writes
	// no new audit row. Default 5m.
	AuditWindow time.Duration
	// RevalidateInterval is how often a long-lived socket re-checks the credential and the
	// camera permission. Default 30s.
	RevalidateInterval time.Duration
	// PingInterval and PongWait are the keepalive of both socket legs: a ping is sent every
	// PingInterval and a leg that shows no sign of life (message or pong) for PongWait is
	// closed. Defaults 20s and 60s.
	PingInterval time.Duration
	PongWait     time.Duration
	// WriteTimeout bounds a single write, so a slow browser cannot pin memory. Default 10s.
	WriteTimeout time.Duration
}

const (
	defaultAuditWindow        = 5 * time.Minute
	defaultRevalidateInterval = 30 * time.Second
	defaultPingInterval       = 20 * time.Second
	defaultPongWait           = 60 * time.Second
	defaultWriteTimeout       = 10 * time.Second

	// clientReadLimit bounds what a browser may send (the go2rtc handshake is tiny);
	// upstreamReadLimit bounds one media message from Frigate (an MSE fragment).
	clientReadLimit   = 64 << 10
	upstreamReadLimit = 8 << 20
	// maxRevalidateFailures undecidable checks in a row fail closed.
	maxRevalidateFailures = 2
)

func orDur(v, def time.Duration) time.Duration {
	if v > 0 {
		return v
	}
	return def
}

func (c LiveConfig) auditWindow() time.Duration { return orDur(c.AuditWindow, defaultAuditWindow) }
func (c LiveConfig) revalidateEvery() time.Duration {
	return orDur(c.RevalidateInterval, defaultRevalidateInterval)
}
func (c LiveConfig) pingEvery() time.Duration { return orDur(c.PingInterval, defaultPingInterval) }
func (c LiveConfig) pongWait() time.Duration  { return orDur(c.PongWait, defaultPongWait) }
func (c LiveConfig) writeTimeout() time.Duration {
	return orDur(c.WriteTimeout, defaultWriteTimeout)
}

// Error codes of the live error frame, part of the contract with the web player.
const (
	CodeCameraOffline       = "camera_offline"
	CodeUnauthorized        = "unauthorized"
	CodeForbidden           = "forbidden"
	CodeUpstreamUnreachable = "upstream_unreachable"
	CodeCodecUnsupported    = "codec_unsupported"
	CodeServerError         = "server_error"
)

// errorFrame is the JSON text frame the gateway sends right before closing a live socket:
//
//	{"type":"error","code":"camera_offline","message":"...","value":"..."}
//
// value repeats message on purpose: go2rtc's own error frame is {"type":"error","value":..},
// which is what the current player renders, so the new frame is backward compatible.
type errorFrame struct {
	Type    string `json:"type"`
	Code    string `json:"code"`
	Message string `json:"message"`
	Value   string `json:"value"`
}

func newErrorFrame(code, msg string) []byte {
	b, _ := json.Marshal(errorFrame{Type: "error", Code: code, Message: msg, Value: msg})
	return b
}

// viewTracker decides whether a live connection starts a new viewing session. Check and
// record are one critical section, so concurrent connects of the same user+camera produce
// exactly one audit row. State is per process (a multi-instance deployment may audit once
// per instance).
type viewTracker struct {
	mu   sync.Mutex
	seen map[viewKey]time.Time
}

type viewKey struct{ user, camera uuid.UUID }

// begin reports whether this is a new viewing session (true) or a reconnect within window
// of the previous connection (false), and slides the window either way.
func (t *viewTracker) begin(k viewKey, now time.Time, window time.Duration) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.seen == nil {
		t.seen = map[viewKey]time.Time{}
	}
	last, ok := t.seen[k]
	t.seen[k] = now
	if len(t.seen) > 1024 { // opportunistic prune keeps the map bounded
		for key, ts := range t.seen {
			if now.Sub(ts) > window {
				delete(t.seen, key)
			}
		}
	}
	return !ok || now.Sub(last) > window
}

// forget undoes a begin whose audit write failed, so the next connect audits again.
func (t *viewTracker) forget(k viewKey) {
	t.mu.Lock()
	delete(t.seen, k)
	t.mu.Unlock()
}

// wsConn serialises data writes to one websocket leg (gorilla allows a single writer);
// control frames (ping, close) are safe to send concurrently.
type wsConn struct {
	c       *websocket.Conn
	mu      sync.Mutex
	timeout time.Duration
}

func (w *wsConn) write(mt int, data []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	_ = w.c.SetWriteDeadline(time.Now().Add(w.timeout))
	return w.c.WriteMessage(mt, data)
}

// closeWith sends a close frame (best effort) and drops the connection.
func (w *wsConn) closeWith(code int, reason string) {
	if len(reason) > 100 { // control frames carry at most 125 bytes
		reason = reason[:100]
	}
	_ = w.c.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(code, reason), time.Now().Add(time.Second))
	_ = w.c.Close()
}

// --- metrics -----------------------------------------------------------------------

var (
	liveActive = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "openvms_live_sessions_active", Help: "Live websocket sessions currently proxied, by quality.",
	}, []string{"quality"})
	liveOpened = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "openvms_live_sessions_opened_total", Help: "Live websocket sessions opened, by quality.",
	}, []string{"quality"})
	liveReconnects = promauto.NewCounter(prometheus.CounterOpts{
		Name: "openvms_live_reconnects_total", Help: "Live connections by the same user to the same camera within the audit window.",
	})
	liveErrors = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "openvms_live_errors_total", Help: "Live error frames sent, by error code.",
	}, []string{"code"})
	liveBytes = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "openvms_live_proxied_bytes_total", Help: "Live payload bytes proxied, by direction (up = browser to Frigate, down = Frigate to browser).",
	}, []string{"direction"})
)

func qualityLabel(q string) string {
	if q == "main" {
		return "main"
	}
	return "sub"
}
