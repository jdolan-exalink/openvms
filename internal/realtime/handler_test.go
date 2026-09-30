package realtime_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
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

func (f *wsFixture) dialResp(t *testing.T, origin string) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	hdr := http.Header{}
	if origin != "" {
		hdr.Set("Origin", origin)
	}
	return websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(f.srv.URL, "http")+"/ws", hdr)
}

func (f *wsFixture) dial(t *testing.T, origin string) (*websocket.Conn, error) {
	t.Helper()
	c, resp, err := f.dialResp(t, origin)
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	return c, err
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
	_, resp, err := f.dialResp(t, "")
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err == nil || resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("resp=%v err=%v, want 401", resp, err)
	}
}

func TestHandlerOriginPolicy(t *testing.T) {
	f := newWS(t, nil, realtime.HubConfig{})
	if _, resp, err := f.dialResp(t, "https://evil.example"); err == nil || resp == nil || resp.StatusCode != http.StatusForbidden {
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		t.Fatalf("cross-origin: resp=%v err=%v, want 403", resp, err)
	} else if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	c, err := f.dial(t, f.srv.URL) // same host as the request
	if err != nil {
		t.Fatalf("same-origin upgrade refused: %v", err)
	}
	c.Close()
}

func TestHandlerStreamsEnvelopes(t *testing.T) {
	f := newWS(t, nil, realtime.HubConfig{})
	c, err := f.dial(t, "")
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
	c, err := f.dial(t, "")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	waitConnections(t, f.hub, 1)
	_, resp, err := f.dialResp(t, "")
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err == nil || resp == nil || resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("resp=%v err=%v, want 429", resp, err)
	}
}

func TestHandlerDisconnectReleasesSubscription(t *testing.T) {
	f := newWS(t, nil, realtime.HubConfig{})
	c, err := f.dial(t, "")
	if err != nil {
		t.Fatal(err)
	}
	waitConnections(t, f.hub, 1)
	c.Close()
	waitConnections(t, f.hub, 0)
}

func TestHandlerShutdownSendsGoingAway(t *testing.T) {
	f := newWS(t, nil, realtime.HubConfig{})
	c, err := f.dial(t, "")
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
	c, err := f.dial(t, "")
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
	c, err := f.dial(t, "")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	waitConnections(t, f.hub, 1)
	// The client never reads, so it never answers pings: the server must give up.
	waitConnections(t, f.hub, 0)
}

// sessionStub is a scriptable per-connection credential check.
type sessionStub struct {
	mu    sync.Mutex
	calls int
	fn    func(call int) (bool, error)
}

func (s *sessionStub) check(context.Context) (bool, error) {
	s.mu.Lock()
	s.calls++
	n := s.calls
	s.mu.Unlock()
	return s.fn(n)
}

func withSession(s *sessionStub) func(*realtime.Handler) {
	return func(h *realtime.Handler) {
		h.RevalidateInterval = 20 * time.Millisecond
		h.Session = func(context.Context) func(context.Context) (bool, error) { return s.check }
	}
}

func readClose(t *testing.T, c *websocket.Conn) *websocket.CloseError {
	t.Helper()
	_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, _, err := c.ReadMessage()
	var ce *websocket.CloseError
	if !errors.As(err, &ce) {
		t.Fatalf("err = %v, want a close frame", err)
	}
	return ce
}

func TestHandlerClosesWhenSessionEnds(t *testing.T) {
	s := &sessionStub{fn: func(n int) (bool, error) { return n < 3, nil }}
	f := newWS(t, withSession(s), realtime.HubConfig{})
	c, err := f.dial(t, "")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	ce := readClose(t, c)
	if ce.Code != websocket.ClosePolicyViolation || ce.Text != "session ended" {
		t.Fatalf("close = %d %q, want 1008 \"session ended\"", ce.Code, ce.Text)
	}
	waitConnections(t, f.hub, 0)
}

func TestHandlerKeepsStreamingWhileSessionIsValid(t *testing.T) {
	s := &sessionStub{fn: func(int) (bool, error) { return true, nil }}
	f := newWS(t, withSession(s), realtime.HubConfig{})
	c, err := f.dial(t, "")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	waitConnections(t, f.hub, 1)
	time.Sleep(120 * time.Millisecond) // several revalidations
	f.hub.Publish(eventMsg(f.tenant, uuid.New()))
	_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
	var env realtime.Envelope
	if err := c.ReadJSON(&env); err != nil {
		t.Fatal(err)
	}
}

func TestHandlerToleratesOneTransientCheckError(t *testing.T) {
	s := &sessionStub{fn: func(n int) (bool, error) {
		if n == 1 {
			return false, errors.New("db blip")
		}
		return true, nil
	}}
	f := newWS(t, withSession(s), realtime.HubConfig{})
	c, err := f.dial(t, "")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	waitConnections(t, f.hub, 1)
	time.Sleep(120 * time.Millisecond)
	f.hub.Publish(eventMsg(f.tenant, uuid.New()))
	_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
	var env realtime.Envelope
	if err := c.ReadJSON(&env); err != nil {
		t.Fatalf("connection dropped after a single transient error: %v", err)
	}
}

func TestHandlerClosesAfterConsecutiveCheckErrors(t *testing.T) {
	s := &sessionStub{fn: func(int) (bool, error) { return false, errors.New("db down") }}
	f := newWS(t, withSession(s), realtime.HubConfig{})
	c, err := f.dial(t, "")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	ce := readClose(t, c)
	if ce.Code != websocket.CloseTryAgainLater || ce.Text != "session check unavailable" {
		t.Fatalf("close = %d %q, want 1013 \"session check unavailable\"", ce.Code, ce.Text)
	}
}

func TestHandlerHelloAndFilter(t *testing.T) {
	siteA := uuid.New()
	siteB := uuid.New()
	camA := uuid.New()
	f := newWS(t, nil, realtime.HubConfig{})
	c, err := f.dial(t, "")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	waitConnections(t, f.hub, 1)

	// Send hello with topics: ["camera"] and site_ids: [siteA]
	hello := realtime.ClientFrame{
		Op:      realtime.OpHello,
		Topics:  []string{"camera"},
		SiteIDs: []uuid.UUID{siteA},
	}
	if err := c.WriteJSON(hello); err != nil {
		t.Fatal(err)
	}

	time.Sleep(50 * time.Millisecond)

	// Publish non-camera event for siteA -> should be ignored because topic is camera
	f.hub.Publish(realtime.Message{
		Envelope: realtime.Envelope{
			Type:     realtime.TypeEventCreated,
			TenantID: f.tenant,
			SiteID:   &siteA,
			Data:     []byte(`{}`),
		},
		Scope: realtime.Scope{Kind: authz.ScopeTenant, ID: f.tenant, Permission: authz.EventsView},
	})

	// Publish camera event for siteB -> should be ignored because site is siteB
	f.hub.Publish(realtime.Message{
		Envelope: realtime.Envelope{
			Type:     realtime.TypeCameraStatusChanged,
			TenantID: f.tenant,
			SiteID:   &siteB,
			CameraID: &camA,
			Data:     []byte(`{"to":"online"}`),
		},
		Scope: realtime.Scope{Kind: authz.ScopeCamera, ID: camA, Permission: authz.CamerasView},
	})

	// Publish camera event for siteA -> should be delivered!
	f.hub.Publish(realtime.Message{
		Envelope: realtime.Envelope{
			Type:     realtime.TypeCameraStatusChanged,
			TenantID: f.tenant,
			SiteID:   &siteA,
			CameraID: &camA,
			Data:     []byte(`{"to":"online"}`),
		},
		Scope: realtime.Scope{Kind: authz.ScopeCamera, ID: camA, Permission: authz.CamerasView},
	})

	_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
	var env realtime.Envelope
	if err := c.ReadJSON(&env); err != nil {
		t.Fatalf("failed to read delivered envelope: %v", err)
	}
	if env.Type != realtime.TypeCameraStatusChanged || env.SiteID == nil || *env.SiteID != siteA {
		t.Fatalf("unexpected envelope: %+v", env)
	}
}

func TestHandlerResumeReplay(t *testing.T) {
	ring := realtime.NewRingBuffer()
	camA := uuid.New()
	tenant := uuid.New()
	// Put messages in ring
	ring.Add(realtime.Message{
		Stream: "events",
		Seq:    1,
		Envelope: realtime.Envelope{
			ID:       "events:1",
			Type:     realtime.TypeEventCreated,
			TenantID: tenant,
			Data:     []byte(`{"seq":1}`),
		},
		Scope: realtime.Scope{Kind: authz.ScopeCamera, ID: camA, Permission: authz.EventsView},
	})
	ring.Add(realtime.Message{
		Stream: "events",
		Seq:    2,
		Envelope: realtime.Envelope{
			ID:       "events:2",
			Type:     realtime.TypeEventCreated,
			TenantID: tenant,
			Data:     []byte(`{"seq":2}`),
		},
		Scope: realtime.Scope{Kind: authz.ScopeCamera, ID: camA, Permission: authz.EventsView},
	})

	actor := tenantActor(tenant)
	cfg := realtime.HubConfig{Authorizer: allowAll(), Log: discard(), Ring: ring}
	hub := realtime.NewHub(cfg)
	h := &realtime.Handler{
		Hub: hub, Log: discard(),
		Actor: func(context.Context) (authz.Actor, bool) { return actor, true },
	}
	srv := httptest.NewServer(h)
	defer srv.Close()
	defer hub.Close()

	c, resp, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http")+"/ws", nil)
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	waitConnections(t, hub, 1)

	// Send hello with last_event_id: {"events": 1}
	hello := realtime.ClientFrame{
		Op:          realtime.OpHello,
		LastEventID: map[string]uint64{"events": 1},
	}
	if err := c.WriteJSON(hello); err != nil {
		t.Fatal(err)
	}

	// Should replay event 2
	_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
	var env realtime.Envelope
	if err := c.ReadJSON(&env); err != nil {
		t.Fatalf("failed to read replayed envelope: %v", err)
	}
	if env.ID != "events:2" {
		t.Fatalf("replayed ID = %s, want events:2", env.ID)
	}
}

func TestHandlerResyncOnRingGap(t *testing.T) {
	ring := realtime.NewRingBuffer()
	// Add message at seq 10
	ring.Add(realtime.Message{
		Stream: "events",
		Seq:    10,
		Envelope: realtime.Envelope{
			ID:       "events:10",
			Type:     realtime.TypeEventCreated,
			TenantID: uuid.New(),
		},
	})

	f := newWS(t, nil, realtime.HubConfig{Ring: ring})
	c, err := f.dial(t, "")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	waitConnections(t, f.hub, 1)

	// Ask to resume from seq 1, which fell out
	hello := realtime.ClientFrame{
		Op:          realtime.OpHello,
		LastEventID: map[string]uint64{"events": 1},
	}
	if err := c.WriteJSON(hello); err != nil {
		t.Fatal(err)
	}

	_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
	var resync realtime.ResyncFrame
	if err := c.ReadJSON(&resync); err != nil {
		t.Fatalf("failed to read resync frame: %v", err)
	}
	if resync.Op != realtime.OpResync || len(resync.Streams) == 0 || resync.Streams[0] != "events" {
		t.Fatalf("unexpected resync frame: %+v", resync)
	}
}

func TestHandlerBatchFrameDelivery(t *testing.T) {
	f := newWS(t, nil, realtime.HubConfig{Buffer: 200})
	c, err := f.dial(t, "")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	waitConnections(t, f.hub, 1)

	// Publish 60 messages in rapid succession
	for i := 1; i <= 60; i++ {
		f.hub.Publish(realtime.Message{
			Envelope: realtime.Envelope{
				ID:       "events:" + string(rune('0'+i)),
				Type:     realtime.TypeEventCreated,
				TenantID: f.tenant,
			},
			Scope: realtime.Scope{Kind: authz.ScopeTenant, ID: f.tenant, Permission: authz.EventsView},
		})
	}

	_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, msgBytes, err := c.ReadMessage()
	if err != nil {
		t.Fatalf("failed to read batch message: %v", err)
	}
	t.Logf("received message: %s", string(msgBytes))
	var batch realtime.BatchFrame
	if err := json.Unmarshal(msgBytes, &batch); err != nil {
		t.Fatalf("failed to parse batch frame: %v (raw: %s)", err, string(msgBytes))
	}
	if batch.Op != realtime.OpBatch || len(batch.Frames) <= 50 {
		t.Fatalf("expected batch frame with >50 frames, got op=%s, frames=%d", batch.Op, len(batch.Frames))
	}
}
