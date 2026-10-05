package api

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOnvifProbeRouteIsRegisteredAndRequiresActor(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	router, err := NewRouter(&Handlers{Log: log}, log, Options{})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/servers/11111111-1111-1111-1111-111111111111/onvif/probe", strings.NewReader(`{"endpoint":"http://192.0.2.8/onvif/device_service"}`))
	req.Header.Set("Content-Type", "application/json")
	rw := httptest.NewRecorder()
	router.ServeHTTP(rw, req)
	if rw.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d body=%s; want route actor check", rw.Code, rw.Body.String())
	}
}

func TestOnvifProbeBodyRejectsUnknownTrailingAndOversizedJSON(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	router, err := NewRouter(&Handlers{Log: log}, log, Options{})
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct{ name, body string }{
		{"unknown", `{"endpoint":"http://192.0.2.8/device","username":"u","password":"p","private_key":"secret"}`},
		{"trailing", `{"endpoint":"http://192.0.2.8/device"}{}`},
		{"oversized", `{"endpoint":"http://192.0.2.8/device","username":"` + strings.Repeat("u", 4096) + `"}`},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/servers/11111111-1111-1111-1111-111111111111/onvif/probe", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			rw := httptest.NewRecorder()
			router.ServeHTTP(rw, req)
			if rw.Code != http.StatusBadRequest {
				t.Fatalf("status = %d body=%s; want bad request", rw.Code, rw.Body.String())
			}
		})
	}
}
