// Package api wires the HTTP surface of the control plane. Routes come from the
// generated OpenAPI server; handwritten routes are limited to docs, metrics and the media
// gateway (binary and websocket streams, see internal/media).
package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/jdolan-exalink/openvms/internal/access"
	"github.com/jdolan-exalink/openvms/internal/api/gen"
	"github.com/jdolan-exalink/openvms/internal/platform/httpx"
	"github.com/jdolan-exalink/openvms/internal/platform/logging"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

// Options configure cross-cutting behaviour of the router.
type Options struct {
	// Queries resolves API tokens. Nil disables authentication (unit tests of public routes).
	Queries *db.Queries
	// TrustForwardedFor takes the client IP from X-Forwarded-For (API behind Caddy).
	TrustForwardedFor bool
	// SessionIdle ends browser sessions after this long without requests.
	SessionIdle time.Duration
	// Media serves /media/v1 (live, recordings, snapshots, downloads) behind the same
	// authentication as the API. Nil disables it.
	Media http.Handler
}

func NewRouter(h *Handlers, log *slog.Logger, opts Options) (http.Handler, error) {
	spec, err := gen.GetSpec()
	if err != nil {
		return nil, err
	}
	specJSON, err := json.Marshal(spec)
	if err != nil {
		return nil, err
	}

	r := chi.NewRouter()
	r.Use(httpx.RequestID, httpx.ClientAddr(opts.TrustForwardedFor), httpx.Recover(log), httpx.AccessLog(log), httpx.SecurityHeaders, withRequestInfo)
	if opts.Queries != nil {
		r.Use(Authenticate(opts.Queries, AuthOptions{IdleTimeout: opts.SessionIdle}))
	}

	r.Get("/openapi.json", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(specJSON)
	})
	r.Get("/docs", serveDocs)
	r.Handle("/metrics", promhttp.Handler())
	if opts.Media != nil {
		r.Mount("/media/v1", opts.Media)
	}

	strict := gen.NewStrictHandlerWithOptions(h, nil, gen.StrictHTTPServerOptions{
		RequestErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			writeError(w, r, http.StatusBadRequest, "bad_request", err.Error())
		},
		ResponseErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			status, code, msg := statusFor(err)
			if status >= 500 && status != http.StatusBadGateway {
				log.ErrorContext(r.Context(), "handler error", "path", r.URL.Path, "error", err)
			}
			// Audit denied access (PRD §66). This never sees the 401s from Authenticate: those
			// respond directly, without an actor, before a strict handler ever runs.
			if errors.Is(err, access.ErrForbidden) {
				if actor, ok := ActorFrom(r.Context()); ok {
					h.auditDenied(r.Context(), actor, err, r.Method, r.URL.Path)
				}
			}
			writeError(w, r, status, code, msg)
		},
	})
	gen.HandlerFromMux(strict, r)

	return otelhttp.NewHandler(r, "openvms-api"), nil
}

func writeError(w http.ResponseWriter, r *http.Request, status int, code, msg string) {
	reqID := logging.RequestID(r.Context())
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(gen.Error{Code: code, Message: msg, RequestId: &reqID})
}

// serveDocs renders the spec with Scalar. The page only reads /openapi.json from this origin.
func serveDocs(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(`<!doctype html>
<html><head><meta charset="utf-8"><title>OpenVMS API</title>
<meta name="viewport" content="width=device-width, initial-scale=1"></head>
<body><script id="api-reference" data-url="/openapi.json"></script>
<script src="https://cdn.jsdelivr.net/npm/@scalar/api-reference@1"></script></body></html>`))
}
