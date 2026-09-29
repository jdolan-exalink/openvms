package realtime_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/realtime"
)

func TestDecodeEventCreated(t *testing.T) {
	tenant, cam, site, srv := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	payload, _ := json.Marshal(map[string]any{
		"id": uuid.New(), "tenant_id": tenant, "site_id": site, "server_id": srv, "camera_id": cam,
		"severity": "alert", "labels": []string{"car"}, "start_time": time.Unix(100, 0).UTC(),
		"plates": []string{"ABC123"}, // never part of the published shape, must not be forwarded
	})
	m, err := realtime.Decode(realtime.DefaultRoutes(), "frigate.event.new."+tenant.String(), payload)
	if err != nil {
		t.Fatal(err)
	}
	if m.Envelope.Type != realtime.TypeEventCreated || m.Envelope.TenantID != tenant {
		t.Fatalf("envelope = %+v", m.Envelope)
	}
	if m.Scope.Kind != authz.ScopeCamera || m.Scope.ID != cam || m.Scope.Permission != authz.EventsView {
		t.Fatalf("scope = %+v", m.Scope)
	}
	var data map[string]any
	if err := json.Unmarshal(m.Envelope.Data, &data); err != nil {
		t.Fatal(err)
	}
	if data["camera_id"] != cam.String() || data["severity"] != "alert" {
		t.Fatalf("data = %v", data)
	}
	if _, leaked := data["plates"]; leaked {
		t.Fatal("event.created must not forward plates")
	}
}

func TestDecodeServerStatus(t *testing.T) {
	tenant, site, srv := uuid.New(), uuid.New(), uuid.New()
	payload, _ := json.Marshal(map[string]any{
		"server_id": srv, "tenant_id": tenant, "site_id": site, "from": "online", "to": "offline",
		"error": "dial tcp 10.0.0.5:5000: refused", "at": time.Unix(100, 0).UTC(),
	})
	m, err := realtime.Decode(realtime.DefaultRoutes(), "server.offline", payload)
	if err != nil {
		t.Fatal(err)
	}
	if m.Envelope.Type != realtime.TypeServerStatus || m.Envelope.TenantID != tenant {
		t.Fatalf("envelope = %+v", m.Envelope)
	}
	if m.Scope.Kind != authz.ScopeServer || m.Scope.ID != srv || m.Scope.Permission != authz.ServersView {
		t.Fatalf("scope = %+v", m.Scope)
	}
	var data map[string]any
	_ = json.Unmarshal(m.Envelope.Data, &data)
	if data["to"] != "offline" || data["server_id"] != srv.String() {
		t.Fatalf("data = %v", data)
	}
	if _, leaked := data["error"]; leaked {
		t.Fatal("server.status must not forward internal error text")
	}
}

func TestDecodeSkipsUnknownAndMalformed(t *testing.T) {
	routes := realtime.DefaultRoutes()
	if _, err := realtime.Decode(routes, "user.created", []byte(`{}`)); err == nil {
		t.Fatal("unrouted subject must be skipped")
	}
	if _, err := realtime.Decode(routes, "frigate.event.new."+uuid.NewString(), []byte(`not json`)); err == nil {
		t.Fatal("malformed payload must be rejected")
	}
	// A payload without a tenant can never be filtered safely.
	if _, err := realtime.Decode(routes, "server.online", []byte(`{"server_id":"`+uuid.NewString()+`"}`)); err == nil {
		t.Fatal("payload without tenant must be rejected")
	}
}

func TestSubjectMatch(t *testing.T) {
	cases := []struct {
		pattern, subject string
		want             bool
	}{
		{"server.*", "server.online", true},
		{"server.*", "server.online.x", false},
		{"frigate.event.new.*", "frigate.event.new.abc", true},
		{"frigate.>", "frigate.event.new.abc", true},
		{"frigate.>", "frigate", false},
		{"server.*", "camera.online", false},
	}
	for _, tc := range cases {
		if got := realtime.SubjectMatch(tc.pattern, tc.subject); got != tc.want {
			t.Errorf("SubjectMatch(%q,%q) = %v, want %v", tc.pattern, tc.subject, got, tc.want)
		}
	}
}
