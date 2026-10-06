// Package api wires the HTTP surface of the control plane. Routes come from the
// generated OpenAPI server; handwritten routes are limited to docs, metrics and the media
// gateway (binary and websocket streams, see internal/media).
package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"strings"
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
	// CredentialTrustedProxyCIDRs explicitly authenticates peers allowed to assert forwarded HTTPS for credential install routes.
	CredentialTrustedProxyCIDRs []netip.Prefix
	// SessionIdle ends browser sessions after this long without requests.
	SessionIdle time.Duration
	// Media serves /media/v1 (live, recordings, snapshots, downloads) behind the same
	// authentication as the API. Nil disables it.
	Media http.Handler
	// Realtime serves the GET /ws push feed behind the same authentication (session cookie or
	// bearer token); unauthenticated requests get 401 before any upgrade. Nil disables it.
	Realtime http.Handler
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
	r.Use(credentialInstallHTTPS(opts.CredentialTrustedProxyCIDRs), httpx.RequestID, httpx.ClientAddr(opts.TrustForwardedFor), httpx.Recover(log), httpx.AccessLog(log), httpx.SecurityHeaders, withRequestInfo)
	r.Use(validateOnvifDiscoveryBody, validateOnvifProbeBody, validateAgentTLSConfigBody, validateAgentInstallBody, validateAgentUpdateBody)
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

	if opts.Realtime != nil {
		r.Method(http.MethodGet, "/ws", opts.Realtime)
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

// credentialInstallHTTPS gates only SSH-password installation POSTs. Unlike the
// global browser cookie helper, this trusts forwarded HTTPS only from an explicitly
// configured immediate proxy peer and never from the forwarded chain itself.
func credentialInstallHTTPS(trusted []netip.Prefix) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
			targeted := r.Method == http.MethodPost && len(parts) == 6 && parts[0] == "api" && parts[1] == "v1" && parts[2] == "servers" && parts[4] == "agent" && (parts[5] == "install" || parts[5] == "update-ssh")
			if targeted && !credentialInstallHTTPSAllowed(r, trusted) {
				writeError(w, r, http.StatusForbidden, "secure_transport_required", "agent installation requires HTTPS")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func credentialInstallHTTPSAllowed(r *http.Request, trusted []netip.Prefix) bool {
	if r.TLS != nil {
		return true
	}
	values := r.Header.Values("X-Forwarded-Proto")
	if len(values) != 1 || values[0] != "https" {
		return false
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return false
	}
	peer, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	peer = peer.Unmap()
	for _, prefix := range trusted {
		if prefix.Contains(peer) {
			return true
		}
	}
	return false
}

// validateOnvifDiscoveryBody enforces the narrow request contract at runtime; generated
// OpenAPI decoders do not enforce additionalProperties or body-size limits themselves.
func validateOnvifDiscoveryBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		if r.Method == http.MethodPost && len(parts) == 6 && parts[0] == "api" && parts[1] == "v1" && parts[2] == "servers" && parts[4] == "onvif" && parts[5] == "discover" {
			limited, err := io.ReadAll(io.LimitReader(r.Body, 1025))
			if err != nil || len(limited) > 1024 {
				writeError(w, r, http.StatusBadRequest, "bad_request", "request body exceeds 1 KiB")
				return
			}
			dec := json.NewDecoder(bytes.NewReader(limited))
			dec.DisallowUnknownFields()
			var body struct {
				InterfaceName string `json:"interface_name"`
			}
			if err := dec.Decode(&body); err != nil || dec.Decode(new(any)) != io.EOF {
				writeError(w, r, http.StatusBadRequest, "bad_request", "invalid ONVIF discovery request")
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(limited))
		}
		next.ServeHTTP(w, r)
	})
}

// validateOnvifProbeBody bounds transient credentials and rejects unknown or trailing JSON.
func validateOnvifProbeBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		if r.Method != http.MethodPost || len(parts) != 6 || parts[0] != "api" || parts[1] != "v1" || parts[2] != "servers" || parts[4] != "onvif" || parts[5] != "probe" {
			next.ServeHTTP(w, r)
			return
		}
		limited, err := io.ReadAll(io.LimitReader(r.Body, onvifProbeRequestMaxBytes+1))
		if err != nil || len(limited) > onvifProbeRequestMaxBytes {
			writeError(w, r, http.StatusBadRequest, "bad_request", "ONVIF probe request exceeds the size limit")
			return
		}
		dec := json.NewDecoder(bytes.NewReader(limited))
		dec.DisallowUnknownFields()
		var body gen.OnvifProbeRequest
		if dec.Decode(&body) != nil || dec.Decode(new(any)) != io.EOF {
			writeError(w, r, http.StatusBadRequest, "bad_request", "invalid ONVIF probe request")
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(limited))
		next.ServeHTTP(w, r)
	})
}

const (
	agentTLSPemMaxBytes       = 64 << 10
	agentTLSRequestMaxBytes   = 128 << 10
	onvifProbeRequestMaxBytes = 4096
)

const agentInstallRequestMaxBytes = 8192

func validateAgentUpdateBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		if r.Method != http.MethodPost || len(parts) != 6 || parts[0] != "api" || parts[1] != "v1" || parts[2] != "servers" || parts[4] != "agent" || parts[5] != "update-ssh" {
			next.ServeHTTP(w, r)
			return
		}
		limited, err := io.ReadAll(io.LimitReader(r.Body, agentInstallRequestMaxBytes+1))
		if err != nil || len(limited) > agentInstallRequestMaxBytes {
			writeError(w, r, http.StatusBadRequest, "bad_request", "agent update request exceeds the size limit")
			return
		}
		dec := json.NewDecoder(bytes.NewReader(limited))
		dec.DisallowUnknownFields()
		var body gen.ServerAgentUpdateRequest
		if dec.Decode(&body) != nil || dec.Decode(new(any)) != io.EOF || body.SshPassword == nil || body.SshPort < 1 || body.SshPort > 65535 {
			writeError(w, r, http.StatusBadRequest, "bad_request", "invalid agent update request")
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(limited))
		next.ServeHTTP(w, r)
	})
}

// validateAgentInstallBody enforces strict bounded JSON. It does not log or retain the body.
func validateAgentInstallBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		if r.Method != http.MethodPost || len(parts) != 6 || parts[0] != "api" || parts[1] != "v1" || parts[2] != "servers" || parts[4] != "agent" || parts[5] != "install" {
			next.ServeHTTP(w, r)
			return
		}
		limited, err := io.ReadAll(io.LimitReader(r.Body, agentInstallRequestMaxBytes+1))
		if err != nil || len(limited) > agentInstallRequestMaxBytes {
			writeError(w, r, http.StatusBadRequest, "bad_request", "agent install request exceeds the size limit")
			return
		}
		dec := json.NewDecoder(bytes.NewReader(limited))
		dec.DisallowUnknownFields()
		var body gen.ServerAgentInstallRequest
		if dec.Decode(&body) != nil || dec.Decode(new(any)) != io.EOF {
			writeError(w, r, http.StatusBadRequest, "bad_request", "invalid agent install request")
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(limited))
		next.ServeHTTP(w, r)
	})
}

// validateAgentTLSConfigBody bounds and strictly decodes public trust configuration.
func validateAgentTLSConfigBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		if r.Method != http.MethodPut || len(parts) != 6 || parts[0] != "api" || parts[1] != "v1" || parts[2] != "servers" || parts[4] != "agent" || parts[5] != "tls" {
			next.ServeHTTP(w, r)
			return
		}
		limited, err := io.ReadAll(io.LimitReader(r.Body, agentTLSRequestMaxBytes+1))
		if err != nil || len(limited) > agentTLSRequestMaxBytes {
			writeError(w, r, http.StatusBadRequest, "bad_request", "request body exceeds the agent TLS configuration limit")
			return
		}
		dec := json.NewDecoder(bytes.NewReader(limited))
		dec.DisallowUnknownFields()
		var body gen.ServerAgentTLSConfig
		if err := dec.Decode(&body); err != nil || dec.Decode(new(any)) != io.EOF {
			writeError(w, r, http.StatusBadRequest, "bad_request", "invalid agent TLS configuration")
			return
		}
		if _, err := provisionAgentTLSConfig(body); err != nil {
			writeError(w, r, http.StatusBadRequest, "bad_request", "invalid agent TLS configuration")
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(limited))
		next.ServeHTTP(w, r)
	})
}
