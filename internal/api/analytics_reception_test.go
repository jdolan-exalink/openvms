package api

import (
	"testing"
	"time"

	"github.com/jdolan-exalink/openvms/internal/api/gen"
	"github.com/jdolan-exalink/openvms/internal/inventory"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

func TestCameraAnalyticsReception(t *testing.T) {
	recent := time.Now().Add(-time.Minute)
	old := time.Now().Add(-48 * time.Hour)
	cases := []struct {
		name      string
		row       db.GetCameraRow
		wantState gen.AnalyticsReceptionState
		wantNone  bool
	}{
		{"an analytic arrived a minute ago", db.GetCameraRow{LastProvisionEventAt: &recent}, gen.AnalyticsReceptionStateReceiving, false},
		{"the last analytic is two days old", db.GetCameraRow{LastProvisionEventAt: &old}, gen.AnalyticsReceptionStateIdle, false},
		{"a camera tagged as Provision that never reported", db.GetCameraRow{Tags: []string{"provision"}}, gen.AnalyticsReceptionStateIdle, false},
		{"a camera Provision has nothing to do with", db.GetCameraRow{}, "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out := toCamera(c.row).AnalyticsReception
			if c.wantNone {
				if out != nil {
					t.Fatalf("got %+v, want no reception state", out)
				}
				return
			}
			if out == nil || out.State != c.wantState {
				t.Fatalf("got %+v, want state %q", out, c.wantState)
			}
		})
	}
}

func TestServerAnalyticsReceptionOnlyForRecorders(t *testing.T) {
	recent := time.Now().Add(-time.Minute)
	nvr := db.GetServerRow{Kind: inventory.KindProvisionNVR, LastProvisionEventAt: recent}
	if got := toServer(inventory.ServerView{GetServerRow: nvr}).AnalyticsReception; got == nil || got.State != gen.AnalyticsReceptionStateReceiving {
		t.Fatalf("recorder with a recent analytic: got %+v, want receiving", got)
	}
	nvr.ProvisionIngestError = "agent unreachable"
	if got := toServer(inventory.ServerView{GetServerRow: nvr}).AnalyticsReception; got == nil || got.State != gen.AnalyticsReceptionStateDown {
		t.Fatalf("recorder whose last pull failed: got %+v, want down", got)
	}
	never := db.GetServerRow{Kind: inventory.KindProvisionNVR, LastProvisionEventAt: time.Unix(0, 0)}
	got := toServer(inventory.ServerView{GetServerRow: never}).AnalyticsReception
	if got == nil || got.State != gen.AnalyticsReceptionStateIdle || got.LastEventAt != nil {
		t.Fatalf("recorder that never reported: got %+v, want idle with no time", got)
	}
	frigate := db.GetServerRow{Kind: "frigate", LastProvisionEventAt: recent}
	if got := toServer(inventory.ServerView{GetServerRow: frigate}).AnalyticsReception; got != nil {
		t.Fatalf("a Frigate server must carry no reception state, got %+v", got)
	}
}
