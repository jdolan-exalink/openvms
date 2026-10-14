package provisionlpr

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
)

func TestConfiguratorManualInstructions(t *testing.T) {
	c := NewConfigurator()
	res := c.Configure(context.Background(), CameraConfigParams{
		TargetHost:     "10.0.15.2",
		TargetPort:     7419,
		IntervalSecond: 20,
	})

	if !res.Success {
		t.Fatal("expected success")
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
}

func TestConfiguratorAutomatedCGI(t *testing.T) {
	var receivedForm url.Values
	var receivedUser, receivedPass string
	var hasAuth bool

	fakeCameraServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/http/post.cgi" {
			_ = r.ParseForm()
			receivedForm = r.PostForm
			receivedUser, receivedPass, hasAuth = r.BasicAuth()
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("OK"))
			return
		}
		http.NotFound(w, r)
	}))
	defer fakeCameraServer.Close()

	u, _ := url.Parse(fakeCameraServer.URL)
	port, _ := strconv.Atoi(u.Port())

	c := NewConfigurator()
	res := c.Configure(context.Background(), CameraConfigParams{
		CameraIP:       u.Hostname(),
		CameraPort:     port,
		Username:       "admin",
		Password:       "pass123",
		TargetHost:     "10.0.15.2",
		TargetPort:     7419,
		IntervalSecond: 20,
	})

	if !res.Success || res.Method != "cgi_api" {
		t.Fatalf("expected cgi_api success, got: %+v", res)
	}
	if !hasAuth || receivedUser != "admin" || receivedPass != "pass123" {
		t.Fatalf("expected basic auth credentials, got user=%q pass=%q auth=%v", receivedUser, receivedPass, hasAuth)
	}
	if receivedForm.Get("server_ip") != "10.0.15.2" || receivedForm.Get("port") != "7419" {
		t.Fatalf("unexpected form params: %+v", receivedForm)
	}
}
