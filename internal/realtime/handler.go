package realtime

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/gorilla/websocket"

	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/platform/httpx"
)

// Handler upgrades GET /ws to a websocket and streams the connection's filtered feed.
type Handler struct {
	Hub *Hub
	// Actor returns the authenticated actor (api.ActorFrom); the router's Authenticate
	// middleware has already validated the session cookie or bearer token.
	Actor func(ctx context.Context) (authz.Actor, bool)
	// AllowedOrigins are extra origins accepted for upgrades, as in media.Gateway.
	AllowedOrigins []string
	Log            *slog.Logger

	// Session returns, for the request that opened the socket, a check that re-validates the
	// credential it was authenticated with (session cookie or bearer token) using the same
	// lookup as the Authenticate middleware. The check reports valid=false once the credential
	// no longer authenticates (logout, revocation, expiry, disabled user) and an error when it
	// could not tell. Nil disables revalidation.
	Session func(ctx context.Context) func(ctx context.Context) (valid bool, err error)
	// RevalidateInterval is how often the credential is re-checked; zero means 30s.
	RevalidateInterval time.Duration

	// Keepalive tuning; zero values use the defaults below.
	PingInterval time.Duration
	PongWait     time.Duration
	WriteTimeout time.Duration
}

const (
	defaultPingInterval = 25 * time.Second
	defaultPongWait     = 60 * time.Second
	defaultWriteTimeout = 10 * time.Second

	defaultRevalidateInterval = 30 * time.Second
	// maxCheckFailures is how many consecutive undecidable checks are tolerated before the
	// connection is closed.
	maxCheckFailures = 2
)

// Close reasons of a revalidated connection, part of the client contract.
const (
	reasonSessionEnded     = "session ended"
	reasonCheckUnavailable = "session check unavailable"
)

// errSessionEnded and errCheckUnavailable end serve when revalidation fails.
var (
	errSessionEnded     = errors.New("session ended")
	errCheckUnavailable = errors.New("session check unavailable")
)

func orDefault(d, def time.Duration) time.Duration {
	if d > 0 {
		return d
	}
	return def
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.Actor(r.Context())
	if !ok {
		http.Error(w, `{"code":"unauthorized","message":"authentication required"}`, http.StatusUnauthorized)
		return
	}
	up := websocket.Upgrader{
		ReadBufferSize: 1 << 10, WriteBufferSize: 4 << 10,
		CheckOrigin: func(r *http.Request) bool { return httpx.OriginAllowed(r, h.AllowedOrigins) },
	}
	// Reserve the slot before upgrading so a refused connection gets a plain HTTP status.
	// The origin check runs inside Upgrade, so a cross-origin request would briefly hold a
	// slot; it is released immediately below.
	sub, err := h.Hub.Subscribe(actor)
	switch {
	case errors.Is(err, ErrTooManyConnections):
		http.Error(w, `{"code":"too_many_connections","message":"too many realtime connections"}`, http.StatusTooManyRequests)
		return
	case err != nil:
		http.Error(w, `{"code":"unavailable","message":"realtime feed is shutting down"}`, http.StatusServiceUnavailable)
		return
	}
	defer sub.Close()
	conn, err := up.Upgrade(w, r, nil)
	if err != nil {
		return // Upgrade already answered the client
	}
	defer conn.Close()
	var check func(context.Context) (bool, error)
	if h.Session != nil {
		check = h.Session(r.Context())
	}
	h.serve(r.Context(), conn, sub, check)
}

// revalidate re-checks the connection's credential every interval and returns errSessionEnded
// when it no longer authenticates. The check is a read-only lookup: it deliberately does not
// slide the session's idle timeout, so an open but unattended socket cannot keep a session
// alive forever; user activity through the REST API (which the UI does when it refetches on
// pushed events) still does. One undecidable check (a transient store error) is tolerated and
// retried on the next tick; two in a row fail closed with errCheckUnavailable so the client
// reconnects instead of streaming on an unverifiable credential.
func revalidate(ctx context.Context, check func(context.Context) (bool, error), every time.Duration) error {
	t := time.NewTicker(every)
	defer t.Stop()
	failures := 0
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
		}
		valid, err := check(ctx)
		switch {
		case ctx.Err() != nil:
			return nil
		case err != nil:
			if failures++; failures >= maxCheckFailures {
				return errCheckUnavailable
			}
		case !valid:
			return errSessionEnded
		default:
			failures = 0
		}
	}
}

func (h *Handler) serve(ctx context.Context, conn *websocket.Conn, sub *Subscription, check func(context.Context) (bool, error)) {
	pingEvery := orDefault(h.PingInterval, defaultPingInterval)
	pongWait := orDefault(h.PongWait, defaultPongWait)
	writeTimeout := orDefault(h.WriteTimeout, defaultWriteTimeout)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Reader: clients send nothing meaningful; reading is what processes pongs and detects a
	// closed peer. Anything larger than a control frame is a protocol violation.
	conn.SetReadLimit(512)
	_ = conn.SetReadDeadline(time.Now().Add(pongWait))
	conn.SetPongHandler(func(string) error { return conn.SetReadDeadline(time.Now().Add(pongWait)) })
	go func() {
		defer cancel()
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()

	// Revalidation ends the connection through the same writer loop as any other close.
	ended := make(chan error, 1)
	if check != nil {
		go func() {
			if err := revalidate(ctx, check, orDefault(h.RevalidateInterval, defaultRevalidateInterval)); err != nil {
				ended <- err
			}
		}()
	}

	// Messages are pumped by a helper so the writer loop can also service pings.
	type result struct {
		env Envelope
		err error
	}
	msgs := make(chan result)
	go func() {
		for {
			env, err := sub.Recv(ctx)
			select {
			case msgs <- result{env, err}:
			case <-ctx.Done():
				return
			}
			if err != nil {
				return
			}
		}
	}()

	ping := time.NewTicker(pingEvery)
	defer ping.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case err := <-ended:
			h.closeWith(conn, err, writeTimeout)
			return
		case <-ping.C:
			_ = conn.SetWriteDeadline(time.Now().Add(writeTimeout))
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		case res := <-msgs:
			if res.err != nil {
				h.closeWith(conn, res.err, writeTimeout)
				return
			}
			_ = conn.SetWriteDeadline(time.Now().Add(writeTimeout))
			if err := conn.WriteJSON(res.env); err != nil {
				return
			}
		}
	}
}

// closeWith sends the close frame matching why the subscription ended.
func (h *Handler) closeWith(conn *websocket.Conn, err error, timeout time.Duration) {
	code, text := websocket.CloseGoingAway, "server shutting down"
	if errors.Is(err, ErrSlowConsumer) {
		code, text = websocket.ClosePolicyViolation, "slow consumer"
	} else if errors.Is(err, errSessionEnded) {
		code, text = websocket.ClosePolicyViolation, reasonSessionEnded
	} else if errors.Is(err, errCheckUnavailable) {
		code, text = websocket.CloseTryAgainLater, reasonCheckUnavailable
	} else if !errors.Is(err, ErrClosed) {
		return
	}
	_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(code, text), time.Now().Add(timeout))
}
