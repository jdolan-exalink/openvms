package realtime_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"

	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/realtime"
)

type wsFixture struct {
	srv    *httptest.Server
	hub    *realtime.Hub
	tenant uuid.UUID
}

func newWS(t *testing.T, mutate func(*realtime.Handler), cfg realtime.HubConfig) *wsFixture {
	t.Helper()
	tenant := uuid.New()
	actor := tenantActor(tenant)
	cfg.Authorizer, cfg.Log = allowAll(), discard()
	hub := realtime.NewHub(cfg)
	h := &realtime.Handler{
		Hub: hub, Log: discard(),
		Actor: func(context.Context) (authz.Actor, bool) { return actor, true },
	}
	if mutate != nil {
		mutate(h)
	}
	srv := httptest.NewServer(h)
	t.Cleanup(func() { srv.Close(); hub.Close() })
	return &wsFixture{srv: srv, hub: hub, tenant: tenant}
}

func (f *wsFixture) dial(t *testing.T, origin string) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	hdr := http.Header{}
	if origin != "" {
		hdr.Set("Origin", origin)
	}
	return websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(f.srv.URL, "http")+"/ws", hdr)
}

func waitConnections(t *testing.T, h *realtime.Hub, n int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for h.Connections() != n {
		if time.Now().After(deadline) {
			t.Fatalf("connections = %d, want %d", h.Connections(), n)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestHandlerRequiresAuthentication(t *testing.T) {
	f := newWS(t, func(h *realtime.Handler) {
		h.Actor = func(context.Context) (authz.Actor, bool) { return authz.Actor{}, false }
	}, realtime.HubConfig{})
	_, resp, err := f.dial(t, "")
	if err == nil || resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("resp=%v err=%v, want 401", resp, err)
	}
}

func TestHandlerOriginPolicy(t *testing.T) {
	f := newWS(t, nil, realtime.HubConfig{})
	if _, resp, err := f.dial(t, "https://evil.example"); err == nil || resp == nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-origin: resp=%v err=%v, want 403", resp, err)
	}
	c, _, err := f.dial(t, f.srv.URL) // same host as the request
	if err != nil {
		t.Fatalf("same-origin upgrade refused: %v", err)
	}
	c.Close()
}

func TestHandlerStreamsEnvelopes(t *testing.T) {
	f := newWS(t, nil, realtime.HubConfig{})
	c, _, err := f.dial(t, "")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	waitConnections(t, f.hub, 1)

	cam := uuid.New()
	f.hub.Publish(eventMsg(f.tenant, cam))
	f.hub.Publish(eventMsg(uuid.New(), uuid.New())) // other tenant: never on this socket

	_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
	var env realtime.Envelope
	if err := c.ReadJSON(&env); err != nil {
		t.Fatal(err)
	}
	if env.Type != realtime.TypeEventCreated || env.TenantID != f.tenant {
		t.Fatalf("env = %+v", env)
	}
	var data map[string]any
	_ = json.Unmarshal(env.Data, &data)
	if data["camera_id"] != cam.String() {
		t.Fatalf("data = %v", data)
	}
	_ = c.SetReadDeadline(time.Now().Add(150 * time.Millisecond))
	if _, _, err := c.ReadMessage(); err == nil {
		t.Fatal("received a second frame; the other tenant's message leaked")
	}
}

func TestHandlerLimitsConnectionsPerUser(t *testing.T) {
	f := newWS(t, nil, realtime.HubConfig{MaxPerUser: 1})
	c, _, err := f.dial(t, "")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	waitConnections(t, f.hub, 1)
	_, resp, err := f.dial(t, "")
	if err == nil || resp == nil || resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("resp=%v err=%v, want 429", resp, err)
	}
}

func TestHandlerDisconnectReleasesSubscription(t *testing.T) {
	f := newWS(t, nil, realtime.HubConfig{})
	c, _, err := f.dial(t, "")
	if err != nil {
		t.Fatal(err)
	}
	waitConnections(t, f.hub, 1)
	c.Close()
	waitConnections(t, f.hub, 0)
}

func TestHandlerShutdownSendsGoingAway(t *testing.T) {
	f := newWS(t, nil, realtime.HubConfig{})
	c, _, err := f.dial(t, "")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	waitConnections(t, f.hub, 1)
	f.hub.Close()
	_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, _, err = c.ReadMessage()
	if !websocket.IsCloseError(err, websocket.CloseGoingAway) {
		t.Fatalf("err = %v, want close 1001", err)
	}
}

func TestHandlerPingsIdleConnections(t *testing.T) {
	f := newWS(t, func(h *realtime.Handler) { h.PingInterval = 30 * time.Millisecond }, realtime.HubConfig{})
	c, _, err := f.dial(t, "")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	pings := make(chan struct{}, 4)
	c.SetPingHandler(func(string) error {
		select {
		case pings <- struct{}{}:
		default:
		}
		return nil
	})
	go func() { _, _, _ = c.ReadMessage() }()
	select {
	case <-pings:
	case <-time.After(2 * time.Second):
		t.Fatal("no ping received")
	}
}

func TestHandlerDropsConnectionWithoutPong(t *testing.T) {
	f := newWS(t, func(h *realtime.Handler) {
		h.PingInterval = 20 * time.Millisecond
		h.PongWait = 60 * time.Millisecond
	}, realtime.HubConfig{})
	c, _, err := f.dial(t, "")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	waitConnections(t, f.hub, 1)
	// The client never reads, so it never answers pings: the server must give up.
	waitConnections(t, f.hub, 0)
}
