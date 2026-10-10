package agentauth_test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"

	"github.com/jdolan-exalink/openvms/internal/agentauth"
	"github.com/jdolan-exalink/openvms/internal/agentauth/agentauthtest"
	"github.com/jdolan-exalink/openvms/internal/agentca"
)

var id = agentca.Identity{TenantID: uuid.New(), ServerID: uuid.New()}

func peerCtx(leaf *x509.Certificate, verified bool) context.Context {
	state := tls.ConnectionState{PeerCertificates: []*x509.Certificate{leaf}}
	if verified {
		state.VerifiedChains = [][]*x509.Certificate{{leaf}}
	}
	return peer.NewContext(context.Background(), &peer.Peer{AuthInfo: credentials.TLSInfo{State: state}})
}

func code(err error) codes.Code { return status.Code(err) }

func TestAuthenticate(t *testing.T) {
	pki := agentauthtest.NewPKI(t)
	now := time.Now()
	good := pki.Issue(t, id, now, time.Hour)
	v := &agentauth.Verifier{Certs: pki.Store}

	got, err := v.Authenticate(peerCtx(good.Leaf, true))
	if err != nil || got != id {
		t.Fatalf("valid certificate: %+v, %v", got, err)
	}

	tests := []struct {
		name string
		ctx  func() context.Context
		want codes.Code
	}{
		{"no peer", func() context.Context { return context.Background() }, codes.Unauthenticated},
		{"plaintext peer", func() context.Context {
			return peer.NewContext(context.Background(), &peer.Peer{})
		}, codes.Unauthenticated},
		{"unverified chain", func() context.Context { return peerCtx(good.Leaf, false) }, codes.Unauthenticated},
		{"unknown serial", func() context.Context {
			c := pki.IssueUnrecorded(t, pki.CA, id, now, time.Hour)
			return peerCtx(c.Leaf, true)
		}, codes.Unauthenticated},
		{"revoked", func() context.Context {
			c := pki.Issue(t, id, now, time.Hour)
			pki.Store.Update(c.Serial, func(r *agentca.Certificate) { n := now; r.RevokedAt = &n })
			return peerCtx(c.Leaf, true)
		}, codes.Unauthenticated},
		{"record names another server", func() context.Context {
			c := pki.Issue(t, id, now, time.Hour)
			pki.Store.Update(c.Serial, func(r *agentca.Certificate) { r.ServerID = uuid.New() })
			return peerCtx(c.Leaf, true)
		}, codes.Unauthenticated},
		{"record names another tenant", func() context.Context {
			c := pki.Issue(t, id, now, time.Hour)
			pki.Store.Update(c.Serial, func(r *agentca.Certificate) { r.TenantID = uuid.New() })
			return peerCtx(c.Leaf, true)
		}, codes.Unauthenticated},
		{"fingerprint differs from the record", func() context.Context {
			c := pki.Issue(t, id, now, time.Hour)
			pki.Store.Update(c.Serial, func(r *agentca.Certificate) { r.Fingerprint = "00" })
			return peerCtx(c.Leaf, true)
		}, codes.Unauthenticated},
		{"record expired", func() context.Context {
			c := pki.Issue(t, id, now, time.Hour)
			pki.Store.Update(c.Serial, func(r *agentca.Certificate) { r.NotAfter = now.Add(-time.Second) })
			return peerCtx(c.Leaf, true)
		}, codes.Unauthenticated},
		{"record not yet valid", func() context.Context {
			c := pki.Issue(t, id, now, time.Hour)
			pki.Store.Update(c.Serial, func(r *agentca.Certificate) { r.NotBefore = now.Add(time.Minute) })
			return peerCtx(c.Leaf, true)
		}, codes.Unauthenticated},
		{"certificate without an agent identity", func() context.Context {
			return peerCtx(pki.CA.Cert, true)
		}, codes.Unauthenticated},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := v.Authenticate(tc.ctx())
			if code(err) != tc.want {
				t.Fatalf("code = %s (%v), want %s", code(err), err, tc.want)
			}
			if err != nil && status.Convert(err).Message() != "invalid client certificate" {
				t.Fatalf("message %q leaks the reason", status.Convert(err).Message())
			}
		})
	}

	t.Run("store outage fails closed as unavailable", func(t *testing.T) {
		pki.Store.Err = errors.New("connection refused to 10.0.0.5:5432")
		defer func() { pki.Store.Err = nil }()
		_, err := v.Authenticate(peerCtx(good.Leaf, true))
		if code(err) != codes.Unavailable || status.Convert(err).Message() != "client certificate check unavailable" {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestInterceptorsPutTheIdentityInTheContext(t *testing.T) {
	pki := agentauthtest.NewPKI(t)
	good := pki.Issue(t, id, time.Now(), time.Hour)
	v := &agentauth.Verifier{Certs: pki.Store}

	var seen agentca.Identity
	var ok bool
	handler := func(ctx context.Context, _ any) (any, error) {
		seen, ok = agentauth.IdentityFromContext(ctx)
		return "resp", nil
	}
	if _, err := v.UnaryInterceptor()(peerCtx(good.Leaf, true), nil, &grpc.UnaryServerInfo{}, handler); err != nil || !ok || seen != id {
		t.Fatalf("unary: identity %+v ok=%v err=%v", seen, ok, err)
	}
	pki.Store.Update(good.Serial, func(r *agentca.Certificate) { n := time.Now(); r.RevokedAt = &n })
	called := false
	_, err := v.UnaryInterceptor()(peerCtx(good.Leaf, true), nil, &grpc.UnaryServerInfo{}, func(context.Context, any) (any, error) {
		called = true
		return nil, nil
	})
	if code(err) != codes.Unauthenticated || called {
		t.Fatalf("revoked: code %s, handler called=%v", code(err), called)
	}

	if _, ok := agentauth.IdentityFromContext(context.Background()); ok {
		t.Fatal("an identity appeared out of nowhere")
	}
}

type fakeStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (f fakeStream) Context() context.Context { return f.ctx }

func TestStreamInterceptor(t *testing.T) {
	pki := agentauthtest.NewPKI(t)
	good := pki.Issue(t, id, time.Now(), time.Hour)
	v := &agentauth.Verifier{Certs: pki.Store}

	var seen agentca.Identity
	handler := func(_ any, ss grpc.ServerStream) error {
		seen, _ = agentauth.IdentityFromContext(ss.Context())
		return nil
	}
	if err := v.StreamInterceptor()(nil, fakeStream{ctx: peerCtx(good.Leaf, true)}, &grpc.StreamServerInfo{}, handler); err != nil || seen != id {
		t.Fatalf("stream: identity %+v err=%v", seen, err)
	}
	if err := v.StreamInterceptor()(nil, fakeStream{ctx: context.Background()}, &grpc.StreamServerInfo{}, handler); code(err) != codes.Unauthenticated {
		t.Fatalf("stream without a peer: %v", err)
	}
}

type slowStore struct{ agentauth.CertificateStore }

func (slowStore) GetCertificate(ctx context.Context, _ string) (agentca.Certificate, error) {
	<-ctx.Done()
	return agentca.Certificate{}, ctx.Err()
}

func TestAuthenticateBoundsTheLookup(t *testing.T) {
	pki := agentauthtest.NewPKI(t)
	good := pki.Issue(t, id, time.Now(), time.Hour)
	v := &agentauth.Verifier{Certs: slowStore{}, LookupTimeout: 50 * time.Millisecond}
	start := time.Now()
	_, err := v.Authenticate(peerCtx(good.Leaf, true))
	if code(err) != codes.Unavailable || time.Since(start) > 2*time.Second {
		t.Fatalf("code = %s (%v) after %s, want Unavailable within the timeout", code(err), err, time.Since(start))
	}
}

func TestRejectedIsTheSharedRefusal(t *testing.T) {
	if code(agentauth.ErrRejected) != codes.Unauthenticated || status.Convert(agentauth.ErrRejected).Message() != "invalid client certificate" {
		t.Fatalf("ErrRejected = %v", agentauth.ErrRejected)
	}
}

// The first successful use of a certificate activates it: the store marks it used and revokes
// the server's other certificates (its parent after a renewal). That happens once, not per call.
func TestAuthenticateActivatesACertificateOnFirstUse(t *testing.T) {
	pki := agentauthtest.NewPKI(t)
	now := time.Now()
	parent := pki.Issue(t, id, now, time.Hour)
	v := &agentauth.Verifier{Certs: pki.Store}

	if _, err := v.Authenticate(peerCtx(parent.Leaf, true)); err != nil {
		t.Fatal(err)
	}
	// A renewal (which needs the parent to have authenticated) issues the successor.
	child := pki.IssueChild(t, id, parent.Serial, now, time.Hour)
	// Before the child is used the parent keeps working.
	if _, err := v.Authenticate(peerCtx(parent.Leaf, true)); err != nil {
		t.Fatalf("parent before the successor was used: %v", err)
	}
	if n := pki.Store.Activations(); n != 1 {
		t.Fatalf("%d activations after two calls with one certificate, want 1 (once per certificate)", n)
	}
	// The child's first call revokes the parent; the child keeps working.
	if _, err := v.Authenticate(peerCtx(child.Leaf, true)); err != nil {
		t.Fatalf("successor: %v", err)
	}
	if _, err := v.Authenticate(peerCtx(parent.Leaf, true)); code(err) != codes.Unauthenticated {
		t.Fatalf("parent after the successor was used: code %s (%v), want Unauthenticated", code(err), err)
	}
	if _, err := v.Authenticate(peerCtx(child.Leaf, true)); err != nil {
		t.Fatalf("successor after revoking its parent: %v", err)
	}
}

func TestAuthenticateFailsClosedWhenActivationFails(t *testing.T) {
	pki := agentauthtest.NewPKI(t)
	c := pki.Issue(t, id, time.Now(), time.Hour)
	pki.Store.ActivateErr = errors.New("db down")
	v := &agentauth.Verifier{Certs: pki.Store}
	_, err := v.Authenticate(peerCtx(c.Leaf, true))
	if code(err) != codes.Unavailable {
		t.Fatalf("code = %s (%v), want Unavailable", code(err), err)
	}
	pki.Store.ActivateErr = nil
	if _, err := v.Authenticate(peerCtx(c.Leaf, true)); err != nil {
		t.Fatalf("after the store recovered: %v", err)
	}
}

// A certificate revoked while it was being activated (a renewal replaced it) is rejected.
func TestAuthenticateRejectsACertificateRevokedDuringActivation(t *testing.T) {
	pki := agentauthtest.NewPKI(t)
	c := pki.Issue(t, id, time.Now(), time.Hour)
	pki.Store.ActivateErr = agentca.ErrCertificateRevoked
	v := &agentauth.Verifier{Certs: pki.Store}
	if _, err := v.Authenticate(peerCtx(c.Leaf, true)); code(err) != codes.Unauthenticated {
		t.Fatalf("code = %s (%v), want Unauthenticated", code(err), err)
	}
}
