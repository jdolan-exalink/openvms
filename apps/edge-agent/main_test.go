package main

import (
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
	newMux := func(discovery http.Handler) *http.ServeMux {
		return buildMux(token, "test", sam, discovery, replace, func() { restarted++ })
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
		mux := newMux(nil)
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
		mux := newMux(handler)
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
}
