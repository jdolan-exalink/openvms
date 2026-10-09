package provision

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jdolan-exalink/openvms/internal/access"
	"github.com/jdolan-exalink/openvms/internal/authz"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestDiscoverAgentStrictResponse(t *testing.T) {
	tests := []struct {
		name, body string
		status     int
		wantErr    bool
	}{
		{"valid", `{"devices":[{"xaddrs":["http://192.0.2.4/onvif/device_service"]}]}`, 200, false},
		{"unknown field", `{"devices":[],"token":"secret"}`, 200, true},
		{"oversized", `{"devices":[{"xaddrs":["` + strings.Repeat("x", 1<<20) + `"]}]}`, 200, true},
		{"agent failure", `{"error":"sensitive"}`, 503, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if r.URL.String() != "http://192.0.2.10:8080/v1/onvif/discover" {
					t.Fatalf("unexpected URL: %s", r.URL)
				}
				if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer token" {
					t.Fatalf("unexpected request: %s", r.Header)
				}
				body, _ := io.ReadAll(r.Body)
				if string(body) != `{"interface_name":"eth0"}` {
					t.Fatalf("request body: %s", body)
				}
				return &http.Response{StatusCode: tt.status, Body: io.NopCloser(strings.NewReader(tt.body)), Header: make(http.Header)}, nil
			})}
			got, err := discoverAgent(context.Background(), client, "192.0.2.10", 8080, "token", "eth0")
			if (err != nil) != tt.wantErr {
				t.Fatalf("discoverAgent error = %v", err)
			}
			if !tt.wantErr && (len(got.Devices) != 1 || len(got.Devices[0].XAddrs) != 1) {
				t.Fatalf("unexpected discovery: %#v", got)
			}
		})
	}
}

func TestDiscoverAgentRejectsNonIPv4Host(t *testing.T) {
	for _, host := range []string{"example.invalid", "::1", "192.0.2.10/path"} {
		t.Run(host, func(t *testing.T) {
			if _, err := discoverAgent(context.Background(), &http.Client{}, host, 8080, "token", "eth0"); err == nil {
				t.Fatal("expected invalid agent host")
			}
		})
	}
}

func TestDiscoverAgentRejectsUnsafeAddresses(t *testing.T) {
	for _, address := range []string{"http://user:pass@192.0.2.4/device", "http://device.local/device", "file:///etc/passwd", "http://[::1]/device"} {
		t.Run(address, func(t *testing.T) {
			client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				body := `{"devices":[{"xaddrs":["` + address + `"]}]}`
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
			})}
			if _, err := discoverAgent(context.Background(), client, "192.0.2.10", 8080, "token", "eth0"); err == nil {
				t.Fatal("expected unsafe address rejection")
			}
		})
	}
}

func TestDiscoverAgentDoesNotFollowRedirects(t *testing.T) {
	calls := 0
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: http.StatusFound, Header: http.Header{"Location": []string{"http://192.0.2.11/collect"}}, Body: io.NopCloser(strings.NewReader("redirect"))}, nil
	})}
	if _, err := discoverAgent(context.Background(), client, "192.0.2.10", 8080, "token", "eth0"); err == nil {
		t.Fatal("expected redirect rejection")
	}
	if calls != 1 {
		t.Fatalf("transport calls = %d, want 1", calls)
	}
}

func TestDiscoverAgentPropagatesCallerCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := discoverAgent(ctx, nil, "192.0.2.10", 8080, "token", "eth0"); !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

func TestDiscoverDenialStopsBeforeSecretOrTransportDependencies(t *testing.T) {
	called := false
	svc := &Service{
		requireServerManage: func(context.Context, authz.Actor, uuid.UUID) error {
			called = true
			return access.ErrForbidden
		},
		// Store, Sealer, and Inv are intentionally nil: any downstream work is a test failure.
	}
	_, err := svc.Discover(context.Background(), authz.Actor{}, uuid.New(), "eth0")
	if !called {
		t.Fatal("authorization check was not called")
	}
	if !errors.Is(err, access.ErrForbidden) {
		t.Fatalf("error = %v, want access.ErrForbidden", err)
	}
}

func TestDiscoverAgentOverridesInjectedRedirectPolicy(t *testing.T) {
	calls, redirectPolicyCalls := 0, 0
	client := &http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			calls++
			if calls == 1 {
				return &http.Response{StatusCode: http.StatusFound, Header: http.Header{"Location": []string{"http://192.0.2.11/collect"}}, Body: io.NopCloser(strings.NewReader("redirect"))}, nil
			}
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"devices":[]}`))}, nil
		}),
		CheckRedirect: func(*http.Request, []*http.Request) error { redirectPolicyCalls++; return nil },
	}
	if _, err := discoverAgent(context.Background(), client, "192.0.2.10", 8080, "token", "eth0"); !errors.Is(err, ErrAgentUnavailable) {
		t.Fatalf("error = %v, want ErrAgentUnavailable", err)
	}
	if calls != 1 || redirectPolicyCalls != 0 {
		t.Fatalf("transport calls=%d caller redirect policy calls=%d; want 1 and 0", calls, redirectPolicyCalls)
	}
}
