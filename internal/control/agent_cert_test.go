package control

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	openvmsv1 "github.com/jdolan-exalink/openvms/gen/go/openvms/v1"
	"github.com/jdolan-exalink/openvms/internal/agentauth/agentauthtest"
	"github.com/jdolan-exalink/openvms/internal/agentca"
)

// fakeRenewer signs with the harness CA and records the certificate in the harness store, the
// way agentenroll.Service.Renew does against Postgres.
type fakeRenewer struct {
	pki *agentauthtest.PKI

	mu    sync.Mutex
	calls []renewCall
	err   error
}

type renewCall struct {
	id     agentca.Identity
	serial string
}

func (f *fakeRenewer) Renew(_ context.Context, id agentca.Identity, previousSerial string, csrPEM []byte) (*agentca.Issuance, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, renewCall{id, previousSerial})
	if f.err != nil {
		return nil, f.err
	}
	if err := agentca.ValidateCSR(csrPEM); err != nil {
		return nil, err
	}
	svc := &agentca.Service{}
	res, err := svc.Sign(f.pki.CA, csrPEM, id)
	if err != nil {
		return nil, err
	}
	f.pki.Store.Put(res.Record)
	return res, nil
}

func newCSR(t *testing.T) (csrPEM []byte, key *ecdsa.PrivateKey) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{Subject: pkix.Name{CommonName: "attacker-chosen"}}, key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der}), key
}

func startRenewalServer(t *testing.T) (*agentHarness, *fakeRenewer) {
	t.Helper()
	var renewer *fakeRenewer
	h := startAgentServer(t, func(cfg *AgentConfig, pki *agentauthtest.PKI) {
		renewer = &fakeRenewer{pki: pki}
		cfg.Renewer = renewer
	})
	return h, renewer
}

func renew(h *agentHarness, t *testing.T, client *tls.Certificate, csr []byte) (*openvmsv1.RenewCertificateResponse, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return openvmsv1.NewAgentServiceClient(h.dial(t, client)).RenewCertificate(ctx, &openvmsv1.RenewCertificateRequest{CsrPem: string(csr)})
}

func TestAgentListener_RenewalIssuesACertificateForTheCallersOwnIdentity(t *testing.T) {
	h, renewer := startRenewalServer(t)
	c := h.pki.Issue(t, agentID, time.Now(), time.Hour)
	csr, key := newCSR(t)

	resp, err := renew(h, t, &c.TLS, csr)
	if err != nil {
		t.Fatalf("RenewCertificate: %v", err)
	}
	block, _ := pem.Decode([]byte(resp.CertificatePem))
	if block == nil {
		t.Fatal("response carries no certificate")
	}
	leaf, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	got, err := agentca.IdentityFromCert(leaf)
	if err != nil || got != agentID {
		t.Fatalf("renewed identity = %+v, %v; want %+v", got, err, agentID)
	}
	if !leaf.PublicKey.(*ecdsa.PublicKey).Equal(&key.PublicKey) {
		t.Fatal("renewed certificate is not for the key in the CSR")
	}
	if leaf.SerialNumber.Text(16) == c.Serial {
		t.Fatal("renewal returned the old certificate")
	}
	if resp.CaPem == "" {
		t.Fatal("response carries no CA certificate")
	}
	if len(renewer.calls) != 1 || renewer.calls[0].id != agentID || renewer.calls[0].serial != c.Serial {
		t.Fatalf("renewer calls = %+v, want one for %+v renewing serial %s", renewer.calls, agentID, c.Serial)
	}
	// The renewed certificate works on the same listener.
	next := tls.Certificate{Certificate: [][]byte{block.Bytes}, PrivateKey: key}
	if _, err := heartbeat(h.nodes(t, &next), agentID.ServerID.String()); err != nil {
		t.Fatalf("heartbeat with the renewed certificate: %v", err)
	}
}

func TestAgentListener_RenewalRefusals(t *testing.T) {
	h, renewer := startRenewalServer(t)
	csr, _ := newCSR(t)
	good := h.pki.Issue(t, agentID, time.Now(), time.Hour)
	if _, err := renew(h, t, &good.TLS, csr); err != nil {
		t.Fatalf("positive control: %v", err)
	}

	t.Run("revoked certificate cannot renew", func(t *testing.T) {
		c := h.pki.Issue(t, agentID, time.Now(), time.Hour)
		h.pki.Store.Update(c.Serial, func(r *agentca.Certificate) { n := time.Now(); r.RevokedAt = &n })
		before := len(renewer.calls)
		_, err := renew(h, t, &c.TLS, csr)
		if status.Code(err) != codes.Unauthenticated {
			t.Fatalf("code = %s (%v), want Unauthenticated", status.Code(err), err)
		}
		if len(renewer.calls) != before {
			t.Fatal("a revoked certificate reached the renewer")
		}
	})
	t.Run("no client certificate", func(t *testing.T) {
		if _, err := renew(h, t, nil, csr); status.Code(err) != codes.Unavailable {
			t.Fatalf("code = %s (%v), want Unavailable (failed handshake)", status.Code(err), err)
		}
	})
	t.Run("bad CSR", func(t *testing.T) {
		for name, bad := range map[string][]byte{"empty": nil, "not PEM": []byte("hello"), "oversized": make([]byte, maxRenewCSRBytes+1)} {
			if _, err := renew(h, t, &good.TLS, bad); status.Code(err) != codes.InvalidArgument {
				t.Fatalf("%s: code = %s (%v), want InvalidArgument", name, status.Code(err), err)
			}
		}
	})
	t.Run("renewer failure is not leaked", func(t *testing.T) {
		renewer.mu.Lock()
		renewer.err = errors.New("pq: connection to 10.0.0.5 refused")
		renewer.mu.Unlock()
		_, err := renew(h, t, &good.TLS, csr)
		if status.Code(err) != codes.Internal {
			t.Fatalf("code = %s (%v), want Internal", status.Code(err), err)
		}
		if msg := status.Convert(err).Message(); msg != "certificate renewal failed" {
			t.Fatalf("message = %q leaks the cause", msg)
		}
	})
}

func TestAgentListener_RenewalIsUnavailableWithoutARenewer(t *testing.T) {
	h := startAgentServer(t)
	c := h.pki.Issue(t, agentID, time.Now(), time.Hour)
	csr, _ := newCSR(t)
	if _, err := renew(h, t, &c.TLS, csr); status.Code(err) != codes.Unimplemented {
		t.Fatalf("code = %s (%v), want Unimplemented", status.Code(err), err)
	}
}
