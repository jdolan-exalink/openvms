package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jdolan-exalink/openvms/internal/agent"
	"github.com/jdolan-exalink/openvms/internal/agent/onvifdiscover"
	"github.com/jdolan-exalink/openvms/internal/onvif"
	"github.com/jdolan-exalink/openvms/internal/platform/buildinfo"
)

type muxFakeDiscovery struct{ called bool }

func (f *muxFakeDiscovery) Probe(_ context.Context, _ string) ([]onvif.DiscoveredDevice, error) {
	f.called = true
	return []onvif.DiscoveredDevice{{XAddrs: []onvif.Endpoint{muxEndpoint("http://192.168.1.22/onvif/device_service")}}}, nil
}

func muxEndpoint(raw string) onvif.Endpoint {
	e, err := onvif.ParseEndpoint(raw)
	if err != nil {
		panic(err)
	}
	return e
}

func TestBuildMuxFeatureRoutesRemainIsolated(t *testing.T) {
	const token = "agent-token"
	sam := agent.NewSampler(agent.Paths{})
	replaced, restarted := 0, 0
	replace := func(r *http.Request) error {
		replaced++
		b, _ := io.ReadAll(r.Body)
		if string(b) != "fake-binary" {
			return errors.New("unexpected update body")
		}
		return nil
	}
	newMux := func(discovery, probe http.Handler) *http.ServeMux {
		return buildMux(token, "test", sam, discovery, probe, nil, nil, replace, func() { restarted++ })
	}
	post := func(mux *http.ServeMux, path, body, auth string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		if auth != "" {
			r.Header.Set("Authorization", "Bearer "+auth)
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	get := func(mux *http.ServeMux, path, auth string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, path, nil)
		if auth != "" {
			r.Header.Set("Authorization", "Bearer "+auth)
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}

	t.Run("disabled discovery leaves metrics and update intact", func(t *testing.T) {
		mux := newMux(nil, nil)
		if got := get(mux, "/v1/metrics", token).Code; got != http.StatusOK {
			t.Fatalf("metrics status=%d", got)
		}
		if got := get(mux, "/v1/metrics", "").Code; got != http.StatusUnauthorized {
			t.Fatalf("unauthorized metrics status=%d", got)
		}
		if got := post(mux, "/v1/metrics", "", token).Code; got != http.StatusMethodNotAllowed {
			t.Fatalf("metrics wrong-method status=%d", got)
		}
		if got := post(mux, "/v1/onvif/discover", `{"interface_name":"eth0"}`, token).Code; got != http.StatusNotFound {
			t.Fatalf("disabled discovery status=%d", got)
		}
		if got := post(mux, "/v1/onvif/probe", `{"endpoint":"http://192.168.1.10/onvif"}`, token).Code; got != http.StatusNotFound {
			t.Fatalf("disabled probe status=%d", got)
		}
		if got := post(mux, "/v1/update", "fake-binary", "").Code; got != http.StatusUnauthorized {
			t.Fatalf("unauthorized update status=%d", got)
		}
		if got := post(mux, "/v1/update", "bad-binary", token).Code; got != http.StatusBadRequest {
			t.Fatalf("invalid update status=%d", got)
		}
		if got := post(mux, "/v1/update", "fake-binary", token).Code; got != http.StatusAccepted {
			t.Fatalf("update status=%d", got)
		}
		if got := get(mux, "/v1/update", token).Code; got != http.StatusMethodNotAllowed {
			t.Fatalf("update wrong-method status=%d", got)
		}
		if replaced != 2 || restarted != 1 {
			t.Fatalf("update callback counts replaced=%d restarted=%d", replaced, restarted)
		}
	})

	t.Run("enabled discovery is registered and authenticated", func(t *testing.T) {
		cfg, enabled, err := onvifdiscover.LoadConfig("eth0", "192.168.1.0/24")
		if err != nil || !enabled {
			t.Fatalf("load config enabled=%v err=%v", enabled, err)
		}
		fake := &muxFakeDiscovery{}
		handler, err := onvifdiscover.NewHandler(token, fake, cfg)
		if err != nil {
			t.Fatal(err)
		}
		mux := newMux(handler, nil)
		if got := post(mux, "/v1/onvif/discover", `{"interface_name":"eth0"}`, "").Code; got != http.StatusUnauthorized {
			t.Fatalf("unauthorized discovery status=%d", got)
		}
		w := post(mux, "/v1/onvif/discover", `{"interface_name":"eth0"}`, token)
		if w.Code != http.StatusOK || !fake.called || !strings.Contains(w.Body.String(), "192.168.1.22") {
			t.Fatalf("discovery status=%d called=%v body=%s", w.Code, fake.called, w.Body.String())
		}
		if get(mux, "/v1/metrics", token).Code != http.StatusOK {
			t.Fatal("metrics route changed when discovery enabled")
		}
	})

	t.Run("probe handler is registered and authenticated on main mux", func(t *testing.T) {
		cfg, _, err := onvifdiscover.LoadConfig("eth0", "192.168.1.0/24")
		if err != nil {
			t.Fatal(err)
		}
		probeCalled := false
		probeHandler, err := onvifdiscover.NewProbeHandler(token, cfg, func(context.Context, onvif.Endpoint, string, string) (onvifdiscover.ProbeResult, error) {
			probeCalled = true
			return onvifdiscover.ProbeResult{
				Information: onvif.DeviceInformation{Manufacturer: "Acme", Model: "Cam1"},
				Services:    []onvif.Service{{Namespace: "urn:test", Endpoint: "http://192.168.1.50"}},
			}, nil
		})
		if err != nil {
			t.Fatal(err)
		}
		mux := newMux(nil, probeHandler)
		if got := post(mux, "/v1/onvif/probe", `{"endpoint":"http://192.168.1.50/onvif"}`, "").Code; got != http.StatusUnauthorized {
			t.Fatalf("unauthorized probe status=%d", got)
		}
		w := post(mux, "/v1/onvif/probe", `{"endpoint":"http://192.168.1.50/onvif"}`, token)
		if w.Code != http.StatusOK || !probeCalled || !strings.Contains(w.Body.String(), "Acme") {
			t.Fatalf("probe status=%d called=%v body=%s", w.Code, probeCalled, w.Body.String())
		}
	})

	t.Run("ops handler is registered on main mux", func(t *testing.T) {
		cfg, _, err := onvifdiscover.LoadConfig("eth0", "192.168.1.0/24")
		if err != nil {
			t.Fatal(err)
		}
		ops, err := onvifdiscover.NewOpsHandler(token, cfg)
		if err != nil {
			t.Fatal(err)
		}
		mux := buildMux(token, "test", sam, nil, nil, ops, nil, replace, func() {})
		if got := post(mux, "/v1/onvif/sync-time", `{}`, "").Code; got != http.StatusUnauthorized {
			t.Fatalf("expected unauthorized, got=%d", got)
		}
		if got := post(mux, "/v1/onvif/ptz", `{}`, "").Code; got != http.StatusUnauthorized {
			t.Fatalf("expected unauthorized, got=%d", got)
		}
		// The video-device and reboot operations are mounted with the control scope; a missing
		// route would answer 404, which OpenVMS reads as "agent too old".
		for _, path := range []string{"/v1/onvif/video-device", "/v1/onvif/reboot"} {
			if got := post(mux, path, `{}`, "").Code; got != http.StatusUnauthorized {
				t.Fatalf("%s without a token: got=%d, want 401", path, got)
			}
			if got := post(mux, path, `{"endpoint":"http://10.9.9.9/x"}`, token).Code; got != http.StatusBadRequest {
				t.Fatalf("%s with a token: got=%d, want 400 (disallowed endpoint)", path, got)
			}
		}
	})

	t.Run("provision keepalive and configure routes mounted", func(t *testing.T) {
		lpr := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		})
		mux := buildMux(token, "test", sam, nil, nil, nil, lpr, replace, func() {})
		if got := post(mux, "/SendKeepalive", "", "").Code; got != http.StatusOK {
			t.Fatalf("keepalive status=%d", got)
		}
		if got := post(mux, "/v1/provision-lpr/configure", `{"target_host":"10.0.15.2"}`, "").Code; got != http.StatusUnauthorized {
			t.Fatalf("expected unauthorized without token, got=%d", got)
		}
		if got := post(mux, "/v1/provision-lpr/configure", `{"target_host":"10.0.15.2"}`, token).Code; got != http.StatusOK {
			t.Fatalf("configure status=%d", got)
		}
	})
}

func TestRunVersionFlagPrintsOnlyTheBuildVersion(t *testing.T) {
	original := buildinfo.Version
	buildinfo.Version = "1.2.3"
	t.Cleanup(func() { buildinfo.Version = original })
	for _, args := range [][]string{{"--version"}, {"-version"}, {"version"}} {
		var out bytes.Buffer
		if !runVersionFlag(args, &out) || out.String() != "1.2.3\n" {
			t.Fatalf("args %v: handled output %q", args, out.String())
		}
	}
	var out bytes.Buffer
	if runVersionFlag(nil, &out) || runVersionFlag([]string{"--other"}, &out) || out.Len() != 0 {
		t.Fatalf("version printed without a version argument: %q", out.String())
	}
}
