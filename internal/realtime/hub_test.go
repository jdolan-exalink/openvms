package realtime_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/realtime"
)

// allowFunc adapts a function to realtime.Authorizer.
type allowFunc func(actor authz.Actor, s realtime.Scope) (bool, error)

func (f allowFunc) Allow(_ context.Context, a authz.Actor, s realtime.Scope) (bool, error) {
	return f(a, s)
}

func allowAll() realtime.Authorizer {
	return allowFunc(func(authz.Actor, realtime.Scope) (bool, error) { return true, nil })
}

func discard() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func tenantActor(tenant uuid.UUID) authz.Actor {
	return authz.Actor{UserID: uuid.New(), Username: "u", TenantID: &tenant}
}

func eventMsg(tenant, camera uuid.UUID) realtime.Message {
	data, _ := json.Marshal(map[string]any{"camera_id": camera})
	return realtime.Message{
		Envelope: realtime.Envelope{Type: realtime.TypeEventCreated, TenantID: tenant, Data: data},
		Scope:    realtime.Scope{Kind: authz.ScopeCamera, ID: camera, Permission: authz.EventsView},
	}
}

func recv(t *testing.T, s *realtime.Subscription) (realtime.Envelope, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return s.Recv(ctx)
}

// expectNothing asserts that no message is delivered within a short window.
func expectNothing(t *testing.T, s *realtime.Subscription) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	if env, err := s.Recv(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected no delivery, got %+v err=%v", env, err)
	}
}

func TestHubDeliversToOwnTenant(t *testing.T) {
	tenant, cam := uuid.New(), uuid.New()
	h := realtime.NewHub(realtime.HubConfig{Authorizer: allowAll(), Log: discard()})
	defer h.Close()
	sub, err := h.Subscribe(tenantActor(tenant))
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Close()

	h.Publish(eventMsg(tenant, cam))
	env, err := recv(t, sub)
	if err != nil {
		t.Fatal(err)
	}
	if env.Type != realtime.TypeEventCreated || env.TenantID != tenant {
		t.Fatalf("envelope = %+v", env)
	}
}

func TestHubNeverLeaksAcrossTenants(t *testing.T) {
	tenantA, tenantB := uuid.New(), uuid.New()
	// The authorizer says yes to everything: tenant isolation must not depend on it.
	h := realtime.NewHub(realtime.HubConfig{Authorizer: allowAll(), Log: discard()})
	defer h.Close()
	subA, _ := h.Subscribe(tenantActor(tenantA))
	subB, _ := h.Subscribe(tenantActor(tenantB))
	defer subA.Close()
	defer subB.Close()

	h.Publish(eventMsg(tenantB, uuid.New()))
	expectNothing(t, subA)
	if _, err := recv(t, subB); err != nil {
		t.Fatalf("owner tenant did not receive its message: %v", err)
	}
}

func TestHubFiltersByPermission(t *testing.T) {
	tenant := uuid.New()
	visible, hidden := uuid.New(), uuid.New()
	auth := allowFunc(func(_ authz.Actor, s realtime.Scope) (bool, error) {
		return s.Permission == authz.EventsView && s.ID == visible, nil
	})
	h := realtime.NewHub(realtime.HubConfig{Authorizer: auth, Log: discard()})
	defer h.Close()
	sub, _ := h.Subscribe(tenantActor(tenant))
	defer sub.Close()

	h.Publish(eventMsg(tenant, hidden))
	h.Publish(eventMsg(tenant, visible))
	env, err := recv(t, sub)
	if err != nil {
		t.Fatal(err)
	}
	var data struct {
		CameraID uuid.UUID `json:"camera_id"`
	}
	_ = json.Unmarshal(env.Data, &data)
	if data.CameraID != visible {
		t.Fatalf("received camera %s, want only the visible one %s", data.CameraID, visible)
	}
	expectNothing(t, sub)
}

func TestHubFailsClosedOnAuthorizerError(t *testing.T) {
	tenant := uuid.New()
	auth := allowFunc(func(authz.Actor, realtime.Scope) (bool, error) { return true, errors.New("db down") })
	h := realtime.NewHub(realtime.HubConfig{Authorizer: auth, Log: discard()})
	defer h.Close()
	sub, _ := h.Subscribe(tenantActor(tenant))
	defer sub.Close()

	h.Publish(eventMsg(tenant, uuid.New()))
	expectNothing(t, sub)
}

func TestHubClosesSlowConsumers(t *testing.T) {
	tenant := uuid.New()
	h := realtime.NewHub(realtime.HubConfig{Authorizer: allowAll(), Log: discard(), Buffer: 2})
	defer h.Close()
	slow, _ := h.Subscribe(tenantActor(tenant))
	fast, _ := h.Subscribe(tenantActor(tenant))
	defer slow.Close()
	defer fast.Close()

	for i := 0; i < 5; i++ {
		h.Publish(eventMsg(tenant, uuid.New()))
		if _, err := recv(t, fast); err != nil { // fast keeps draining
			t.Fatal(err)
		}
	}
	// slow never read: it overflowed its 2-slot buffer and was dropped.
	var err error
	for i := 0; i < 5 && !errors.Is(err, realtime.ErrSlowConsumer); i++ {
		_, err = recv(t, slow)
	}
	if !errors.Is(err, realtime.ErrSlowConsumer) {
		t.Fatalf("slow consumer error = %v, want ErrSlowConsumer", err)
	}
	if h.Connections() != 1 {
		t.Fatalf("connections = %d, want 1 (slow one removed)", h.Connections())
	}
}

func TestHubLimitsConnectionsPerUser(t *testing.T) {
	h := realtime.NewHub(realtime.HubConfig{Authorizer: allowAll(), Log: discard(), MaxPerUser: 2})
	defer h.Close()
	actor := tenantActor(uuid.New())
	a, err := h.Subscribe(actor)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.Subscribe(actor); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Subscribe(actor); !errors.Is(err, realtime.ErrTooManyConnections) {
		t.Fatalf("third subscribe error = %v, want ErrTooManyConnections", err)
	}
	if _, err := h.Subscribe(tenantActor(uuid.New())); err != nil {
		t.Fatalf("another user must not be limited: %v", err)
	}
	a.Close()
	if _, err := h.Subscribe(actor); err != nil {
		t.Fatalf("closing a connection must free a slot: %v", err)
	}
}

func TestHubCloseEndsSubscriptions(t *testing.T) {
	h := realtime.NewHub(realtime.HubConfig{Authorizer: allowAll(), Log: discard()})
	sub, _ := h.Subscribe(tenantActor(uuid.New()))
	h.Close()
	if _, err := recv(t, sub); !errors.Is(err, realtime.ErrClosed) {
		t.Fatalf("recv after Close = %v, want ErrClosed", err)
	}
	if _, err := h.Subscribe(tenantActor(uuid.New())); !errors.Is(err, realtime.ErrClosed) {
		t.Fatalf("subscribe after Close = %v, want ErrClosed", err)
	}
}

func TestHubDispatchDecodesAndPublishes(t *testing.T) {
	tenant, cam := uuid.New(), uuid.New()
	h := realtime.NewHub(realtime.HubConfig{Authorizer: allowAll(), Log: discard(), Routes: realtime.DefaultRoutes()})
	defer h.Close()
	sub, _ := h.Subscribe(tenantActor(tenant))
	defer sub.Close()

	payload, _ := json.Marshal(map[string]any{"id": uuid.New(), "tenant_id": tenant, "camera_id": cam, "severity": "alert"})
	h.Dispatch("frigate.event.new."+tenant.String(), payload)
	h.Dispatch("frigate.event.new."+tenant.String(), []byte("garbage")) // dropped, must not break the hub
	env, err := recv(t, sub)
	if err != nil || env.Type != realtime.TypeEventCreated {
		t.Fatalf("env=%+v err=%v", env, err)
	}
	expectNothing(t, sub)
}

func TestHubSiteFiltering(t *testing.T) {
	tenant := uuid.New()
	siteA, siteB := uuid.New(), uuid.New()
	h := realtime.NewHub(realtime.HubConfig{Authorizer: allowAll(), Log: discard()})
	defer h.Close()

	sub, err := h.Subscribe(tenantActor(tenant))
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Close()

	// Filter to siteA only
	sub.SetSiteIDs([]uuid.UUID{siteA})

	// Message for siteB should be filtered out
	h.Publish(realtime.Message{
		Envelope: realtime.Envelope{Type: realtime.TypeEventCreated, TenantID: tenant, SiteID: &siteB, Data: []byte(`{}`)},
		Scope:    realtime.Scope{Kind: authz.ScopeTenant, ID: tenant, Permission: authz.EventsView},
	})
	expectNothing(t, sub)

	// Message for siteA should be delivered
	h.Publish(realtime.Message{
		Envelope: realtime.Envelope{Type: realtime.TypeEventCreated, TenantID: tenant, SiteID: &siteA, Data: []byte(`{}`)},
		Scope:    realtime.Scope{Kind: authz.ScopeTenant, ID: tenant, Permission: authz.EventsView},
	})
	env, err := recv(t, sub)
	if err != nil {
		t.Fatal(err)
	}
	if env.SiteID == nil || *env.SiteID != siteA {
		t.Fatalf("expected siteA, got %+v", env.SiteID)
	}

	// Message without siteID (global/tenant-level) should still be delivered
	h.Publish(realtime.Message{
		Envelope: realtime.Envelope{Type: realtime.TypeEventCreated, TenantID: tenant, SiteID: nil, Data: []byte(`{}`)},
		Scope:    realtime.Scope{Kind: authz.ScopeTenant, ID: tenant, Permission: authz.EventsView},
	})
	if _, err := recv(t, sub); err != nil {
		t.Fatal(err)
	}
}

func TestHubTopicFiltering(t *testing.T) {
	tenant := uuid.New()
	h := realtime.NewHub(realtime.HubConfig{Authorizer: allowAll(), Log: discard()})
	defer h.Close()

	sub, err := h.Subscribe(tenantActor(tenant))
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Close()

	// Filter to camera topic only
	sub.SetTopics([]string{"camera"})

	// Event message should be filtered out
	h.Publish(realtime.Message{
		Envelope: realtime.Envelope{Type: realtime.TypeEventCreated, TenantID: tenant, Data: []byte(`{}`)},
		Scope:    realtime.Scope{Kind: authz.ScopeTenant, ID: tenant, Permission: authz.EventsView},
	})
	expectNothing(t, sub)

	// Camera status message should be delivered
	camID := uuid.New()
	h.Publish(realtime.Message{
		Envelope: realtime.Envelope{Type: realtime.TypeCameraStatusChanged, TenantID: tenant, CameraID: &camID, Data: []byte(`{}`)},
		Scope:    realtime.Scope{Kind: authz.ScopeCamera, ID: camID, Permission: authz.CamerasView},
	})
	env, err := recv(t, sub)
	if err != nil {
		t.Fatal(err)
	}
	if env.Type != realtime.TypeCameraStatusChanged {
		t.Fatalf("expected camera.status_changed, got %s", env.Type)
	}
}

func TestHubCameraStatusCoalescing(t *testing.T) {
	tenant := uuid.New()
	camID := uuid.New()
	h := realtime.NewHub(realtime.HubConfig{Authorizer: allowAll(), Log: discard()})
	defer h.Close()

	sub, err := h.Subscribe(tenantActor(tenant))
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Close()

	makeMsg := func(status string) realtime.Message {
		return realtime.Message{
			Envelope: realtime.Envelope{
				Type:     realtime.TypeCameraStatusChanged,
				TenantID: tenant,
				CameraID: &camID,
				Data:     []byte(`{"to":"` + status + `"}`),
			},
			Scope: realtime.Scope{Kind: authz.ScopeCamera, ID: camID, Permission: authz.CamerasView},
		}
	}

	// First message delivered immediately
	h.Publish(makeMsg("streaming"))
	env1, err := recv(t, sub)
	if err != nil {
		t.Fatal(err)
	}
	if string(env1.Data) != `{"to":"streaming"}` {
		t.Fatalf("first msg = %s, want streaming", string(env1.Data))
	}

	// Two rapid messages within 500ms
	h.Publish(makeMsg("reconnecting"))
	time.Sleep(10 * time.Millisecond)
	h.Publish(makeMsg("offline"))

	// The coalesced message delivered after the 500ms window expires should be "offline"
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	env2, err := sub.Recv(ctx)
	if err != nil {
		t.Fatalf("expected coalesced message, got err: %v", err)
	}
	if string(env2.Data) != `{"to":"offline"}` {
		t.Fatalf("expected offline, got %s", string(env2.Data))
	}

	// Nothing else should be delivered
	expectNothing(t, sub)
}

func TestHubServerOfflineSuppression(t *testing.T) {
	tenant := uuid.New()
	serverID := uuid.New()
	camID := uuid.New()
	h := realtime.NewHub(realtime.HubConfig{Authorizer: allowAll(), Log: discard()})
	defer h.Close()

	sub, err := h.Subscribe(tenantActor(tenant))
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Close()

	// Server goes offline
	serverData, _ := json.Marshal(map[string]any{
		"id":   serverID,
		"from": "online",
		"to":   "offline",
	})
	h.Publish(realtime.Message{
		Envelope: realtime.Envelope{
			Type:     realtime.TypeServerStatus,
			TenantID: tenant,
			ServerID: &serverID,
			Data:     serverData,
		},
		Scope: realtime.Scope{Kind: authz.ScopeServer, ID: serverID, Permission: authz.ServersView},
	})

	serverEnv, err := recv(t, sub)
	if err != nil {
		t.Fatal(err)
	}
	if serverEnv.Type != realtime.TypeServerStatus {
		t.Fatalf("expected server.status, got %s", serverEnv.Type)
	}

	// Camera status for that server published during the 30s window must be suppressed
	h.Publish(realtime.Message{
		Envelope: realtime.Envelope{
			Type:     realtime.TypeCameraStatusChanged,
			TenantID: tenant,
			ServerID: &serverID,
			CameraID: &camID,
			Data:     []byte(`{"to":"unknown"}`),
		},
		Scope: realtime.Scope{Kind: authz.ScopeCamera, ID: camID, Permission: authz.CamerasView},
	})
	expectNothing(t, sub)
}
