package provisionlpr

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
)

func TestConfiguratorPreparesTheStepsAndDoesNotClaimSuccess(t *testing.T) {
	c := NewConfigurator()
	res := c.Configure(context.Background(), CameraConfigParams{
		TargetHost:     "10.0.15.2",
		TargetPort:     7419,
		IntervalSecond: 20,
	})

	if res.Success {
		t.Fatal("the configurator reported success although nothing was applied to the camera")
	}
	if res.Method != "manual_instructions" {
		t.Fatalf("expected manual_instructions, got %q", res.Method)
	}
	if res.Parameters["Server IP / Domain"] != "10.0.15.2" {
		t.Fatalf("unexpected target host: %v", res.Parameters)
	}
	if res.Parameters["Server Port"] != "7419" {
		t.Fatalf("unexpected target port: %v", res.Parameters)
	}
	if res.Parameters["Heartbeat Interval"] != "20 s" || res.WebUIPath == "" {
		t.Fatalf("the steps are incomplete: %+v", res)
	}
}

func TestConfiguratorAppliesDefaultsToMissingValues(t *testing.T) {
	res := NewConfigurator().Configure(context.Background(), CameraConfigParams{TargetHost: "10.0.15.2"})
	if res.Parameters["Server Port"] != "7419" || res.Parameters["Keepalive Endpoint"] != "/SendKeepalive" ||
		res.Parameters["LPR Event Endpoint"] != "/v1/provision-lpr/event" || res.Parameters["Heartbeat Interval"] != "20 s" {
		t.Fatalf("defaults missing: %v", res.Parameters)
	}
}

// The manufacturer documents no way to set the push remotely, so the configurator must not guess an
// endpoint on the camera or hand it credentials.
func TestConfiguratorNeverContactsTheCameraOrUsesItsCredentials(t *testing.T) {
	var requests atomic.Int32
	camera := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer camera.Close()
	u, _ := url.Parse(camera.URL)
	port, _ := strconv.Atoi(u.Port())

	res := NewConfigurator().Configure(context.Background(), CameraConfigParams{
		CameraIP: u.Hostname(), CameraPort: port, Username: "admin", Password: "pass123",
		TargetHost: "10.0.15.2", TargetPort: 7419, IntervalSecond: 20,
	})
	if got := requests.Load(); got != 0 {
		t.Fatalf("the camera received %d requests; the configurator must not guess endpoints on it", got)
	}
	if res.Success || res.Method != "manual_instructions" {
		t.Fatalf("result = %+v", res)
	}
	for k, v := range res.Parameters {
		if strings.Contains(k+v, "pass123") || strings.Contains(k+v, "admin") {
			t.Fatalf("a credential leaked into the result: %s=%s", k, v)
		}
	}
}
