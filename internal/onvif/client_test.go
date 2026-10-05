package onvif

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func response(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}

func TestEndpointValidation(t *testing.T) {
	for _, raw := range []string{"", "ftp://camera.local/device", "http://user:pass@camera/device", "http://camera/device?token=x", "http://camera/device#frag", "http://", "http://camera\\evil"} {
		t.Run(raw, func(t *testing.T) {
			if _, err := ParseEndpoint(raw); err == nil {
				t.Fatalf("ParseEndpoint(%q) succeeded", raw)
			}
		})
	}
	for _, raw := range []string{"http://camera.local/onvif/device_service", "https://192.0.2.4/onvif/device_service"} {
		if _, err := ParseEndpoint(raw); err != nil {
			t.Errorf("ParseEndpoint(%q): %v", raw, err)
		}
	}
}

func TestCallBuildsSOAPAndParsesResponse(t *testing.T) {
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/soap+xml; charset=utf-8; action=\"urn:test:GetDeviceInformation\"" {
			t.Errorf("unexpected request: %s %v", r.Method, r.Header)
		}
		b, _ := io.ReadAll(r.Body)
		for _, want := range []string{"http://www.w3.org/2003/05/soap-envelope", "urn:test", "<tds:GetDeviceInformation"} {
			if !strings.Contains(string(b), want) {
				t.Errorf("SOAP request missing %q: %s", want, b)
			}
		}
		return response(200, `<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"><s:Body><tds:GetDeviceInformationResponse xmlns:tds="urn:test"><tds:Model>MockCam</tds:Model></tds:GetDeviceInformationResponse></s:Body></s:Envelope>`), nil
	})
	c := NewClient(transport, Config{Timeout: time.Second, MaxBodyBytes: 4096, MaxAttempts: 2})
	ep, _ := ParseEndpoint("http://camera.local/onvif/device_service")
	got, err := c.Call(context.Background(), ep, "urn:test:GetDeviceInformation", `<tds:GetDeviceInformation xmlns:tds="urn:test"/>`, ReadOnly)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "MockCam") {
		t.Fatalf("response: %s", got)
	}
}

func TestCallBoundsBodyAndMapsFaults(t *testing.T) {
	ep, _ := ParseEndpoint("http://camera.local/service")
	t.Run("oversize", func(t *testing.T) {
		c := NewClient(roundTripFunc(func(*http.Request) (*http.Response, error) { return response(200, strings.Repeat("x", 100)), nil }), Config{Timeout: time.Second, MaxBodyBytes: 32})
		_, err := c.Call(context.Background(), ep, "urn:test:Read", `<x/>`, ReadOnly)
		if !errors.Is(err, ErrResponseTooLarge) {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("unsupported fault", func(t *testing.T) {
		body := `<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"><s:Body><s:Fault><s:Code><s:Value>s:Sender</s:Value><s:Subcode><s:Value>ter:ActionNotSupported</s:Value></s:Subcode></s:Code><s:Reason><s:Text xml:lang="en">not supported</s:Text></s:Reason></s:Fault></s:Body></s:Envelope>`
		c := NewClient(roundTripFunc(func(*http.Request) (*http.Response, error) { return response(500, body), nil }), Config{Timeout: time.Second})
		_, err := c.Call(context.Background(), ep, "urn:test:PTZ", `<x/>`, Mutating)
		var callErr *CallError
		if !errors.As(err, &callErr) || callErr.Status != StatusUnsupported {
			t.Fatalf("got %#v", err)
		}
	})
	t.Run("auth redacted", func(t *testing.T) {
		c := NewClient(roundTripFunc(func(*http.Request) (*http.Response, error) { return response(401, "secret response"), nil }), Config{Timeout: time.Second})
		_, err := c.Call(context.Background(), ep, "urn:test:Read", `<x/>`, ReadOnly)
		if strings.Contains(err.Error(), "secret") || !strings.Contains(err.Error(), "authentication") {
			t.Fatalf("unsafe error: %v", err)
		}
	})
}

func TestRetryPolicyAndCancellation(t *testing.T) {
	ep, _ := ParseEndpoint("http://camera.local/service")
	t.Run("only read-only retried", func(t *testing.T) {
		for _, tc := range []struct {
			op   Operation
			want int
		}{{ReadOnly, 2}, {Mutating, 1}} {
			calls := 0
			c := NewClient(roundTripFunc(func(*http.Request) (*http.Response, error) { calls++; return nil, io.ErrUnexpectedEOF }), Config{Timeout: time.Second, MaxAttempts: 2, RetryDelay: time.Millisecond})
			_, _ = c.Call(context.Background(), ep, "urn:test:Action", `<x/>`, tc.op)
			if calls != tc.want {
				t.Errorf("operation %v called %d times, want %d", tc.op, calls, tc.want)
			}
		}
	})
	t.Run("cancel", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		c := NewClient(roundTripFunc(func(*http.Request) (*http.Response, error) {
			t.Fatal("transport invoked after cancellation")
			return nil, nil
		}), Config{Timeout: time.Second})
		_, err := c.Call(ctx, ep, "urn:test:Read", `<x/>`, ReadOnly)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("timeout", func(t *testing.T) {
		c := NewClient(roundTripFunc(func(r *http.Request) (*http.Response, error) {
			<-r.Context().Done()
			return nil, r.Context().Err()
		}), Config{Timeout: time.Millisecond, MaxAttempts: 3})
		_, err := c.Call(context.Background(), ep, "urn:test:Read", `<x/>`, ReadOnly)
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("got %v", err)
		}
	})
}
