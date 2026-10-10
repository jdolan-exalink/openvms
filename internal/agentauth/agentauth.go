// Package agentauth authenticates edge agents on the agent gRPC listener. The TLS layer
// already proved the peer holds a certificate signed by the agent CA; this package checks
// that certificate against the issued-certificate table on every RPC (it must still be live
// and not revoked) and puts the agent's identity in the request context.
//
// Revocation is a database lookup per RPC rather than a cached list, so a revoked
// certificate is refused on the very next call. An RPC already running when the revocation
// commits finishes normally, and a connection that stays open is re-checked by each of its
// RPCs, so a revoked agent can keep a connection but cannot use it.
package agentauth

import (
	"context"
	"crypto/x509"
	"errors"
	"log/slog"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"

	"github.com/jdolan-exalink/openvms/internal/agentca"
)

// CertificateStore looks up issued certificates by serial (agentca.PgRepo).
type CertificateStore interface {
	// GetCertificate returns agentca.ErrCertificateNotFound for an unknown serial.
	GetCertificate(ctx context.Context, serial string) (agentca.Certificate, error)
}

// Verifier checks the peer certificate of each call.
type Verifier struct {
	Certs CertificateStore
	Log   *slog.Logger     // defaults to slog.Default()
	Now   func() time.Time // defaults to time.Now
	// LookupTimeout bounds the certificate lookup of each call; a lookup that takes longer
	// fails closed as Unavailable. Defaults to DefaultLookupTimeout.
	LookupTimeout time.Duration
}

// DefaultLookupTimeout is how long a call waits for the certificate lookup.
const DefaultLookupTimeout = 3 * time.Second

// The client sees one message for every way a certificate can be refused, so it learns
// nothing about which certificates exist; the reason goes to the log.
var (
	// ErrRejected is the refusal for any unacceptable client certificate. Handlers that find
	// no authenticated identity return it too, so the message is the same everywhere.
	ErrRejected    = status.Error(codes.Unauthenticated, "invalid client certificate")
	errUnavailable = status.Error(codes.Unavailable, "client certificate check unavailable")
)

type (
	ctxKey       struct{}
	serialCtxKey struct{}
)

// IdentityFromContext returns the agent identity the interceptors authenticated.
func IdentityFromContext(ctx context.Context) (agentca.Identity, bool) {
	id, ok := ctx.Value(ctxKey{}).(agentca.Identity)
	return id, ok
}

// SerialFromContext returns the serial (lowercase hex) of the certificate the interceptors
// authenticated, for handlers that act on that specific certificate, such as renewal.
func SerialFromContext(ctx context.Context) (string, bool) {
	serial, ok := ctx.Value(serialCtxKey{}).(string)
	return serial, ok
}

// withAuthenticated records the authenticated identity and certificate serial in ctx.
func withAuthenticated(ctx context.Context, id agentca.Identity) context.Context {
	if leaf := verifiedLeaf(ctx); leaf != nil {
		ctx = context.WithValue(ctx, serialCtxKey{}, leaf.SerialNumber.Text(16))
	}
	return context.WithValue(ctx, ctxKey{}, id)
}

func (v *Verifier) log() *slog.Logger {
	if v.Log != nil {
		return v.Log
	}
	return slog.Default()
}

func (v *Verifier) now() time.Time {
	if v.Now != nil {
		return v.Now()
	}
	return time.Now()
}

// Authenticate verifies the peer certificate of ctx and returns the agent identity. A
// certificate is accepted only if it was verified against the agent CA by the TLS layer,
// carries an agent identity, is recorded, matches the recorded fingerprint, tenant and
// server, is inside the recorded validity window and is not revoked. A revocation time in
// the future is a grace period (a certificate superseded by a renewal): it still
// authenticates until then. A refusal is
// Unauthenticated; a failed lookup is Unavailable (fail closed, and the agent retries).
func (v *Verifier) Authenticate(ctx context.Context) (agentca.Identity, error) {
	leaf := verifiedLeaf(ctx)
	if leaf == nil {
		v.log().Warn("agent call without a verified client certificate")
		return agentca.Identity{}, ErrRejected
	}
	serial := leaf.SerialNumber.Text(16)
	id, err := agentca.IdentityFromCert(leaf)
	if err != nil {
		v.log().Warn("agent certificate has no valid identity", "serial", serial, "error", err)
		return agentca.Identity{}, ErrRejected
	}
	timeout := v.LookupTimeout
	if timeout <= 0 {
		timeout = DefaultLookupTimeout
	}
	lookupCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	rec, err := v.Certs.GetCertificate(lookupCtx, serial)
	if errors.Is(err, agentca.ErrCertificateNotFound) {
		v.log().Warn("agent certificate is not recorded", "serial", serial)
		return agentca.Identity{}, ErrRejected
	}
	if err != nil {
		v.log().Error("agent certificate lookup failed", "serial", serial, "error", err)
		return agentca.Identity{}, errUnavailable
	}
	now := v.now()
	switch {
	case rec.RevokedAt != nil && !rec.RevokedAt.After(now):
		v.log().Warn("agent certificate is revoked", "serial", serial, "server_id", id.ServerID)
	case rec.TenantID != id.TenantID || rec.ServerID != id.ServerID:
		v.log().Warn("agent certificate identity differs from its record", "serial", serial)
	case rec.Fingerprint != agentca.Fingerprint(leaf):
		v.log().Warn("agent certificate fingerprint differs from its record", "serial", serial)
	case now.Before(rec.NotBefore) || now.After(rec.NotAfter):
		v.log().Warn("agent certificate is outside its validity window", "serial", serial)
	default:
		return id, nil
	}
	return agentca.Identity{}, ErrRejected
}

// verifiedLeaf is the peer's leaf certificate, only when the TLS layer verified its chain.
func verifiedLeaf(ctx context.Context) *x509.Certificate {
	p, ok := peer.FromContext(ctx)
	if !ok {
		return nil
	}
	info, ok := p.AuthInfo.(credentials.TLSInfo)
	if !ok || len(info.State.VerifiedChains) == 0 || len(info.State.VerifiedChains[0]) == 0 {
		return nil
	}
	return info.State.VerifiedChains[0][0]
}

// UnaryInterceptor authenticates every unary call.
func (v *Verifier) UnaryInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		id, err := v.Authenticate(ctx)
		if err != nil {
			return nil, err
		}
		return handler(withAuthenticated(ctx, id), req)
	}
}

// StreamInterceptor authenticates a stream when it opens. A stream is not re-checked while
// it runs, so long-lived streams must be bounded by the server's connection age settings.
func (v *Verifier) StreamInterceptor() grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		id, err := v.Authenticate(ss.Context())
		if err != nil {
			return err
		}
		return handler(srv, &identityStream{ServerStream: ss, ctx: withAuthenticated(ss.Context(), id)})
	}
}

type identityStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s *identityStream) Context() context.Context { return s.ctx }
