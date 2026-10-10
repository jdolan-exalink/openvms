// Package agentauthtest builds throwaway agent PKI for tests: a CA, client certificates it
// signs, and an in-memory certificate store.
package agentauthtest

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"sync"
	"testing"
	"time"

	"github.com/jdolan-exalink/openvms/internal/agentca"
)

// Store is an in-memory agentauth.CertificateStore.
type Store struct {
	mu    sync.Mutex
	certs map[string]agentca.Certificate
	// Err, when set, is returned by every lookup (a database outage).
	Err error
	// ActivateErr, when set, is returned by every activation.
	ActivateErr error
	activations int
}

func (s *Store) GetCertificate(_ context.Context, serial string) (agentca.Certificate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Err != nil {
		return agentca.Certificate{}, s.Err
	}
	c, ok := s.certs[serial]
	if !ok {
		return agentca.Certificate{}, agentca.ErrCertificateNotFound
	}
	return c, nil
}

// ActivateCertificate mirrors agentca.PgRepo.ActivateCertificate in memory.
func (s *Store) ActivateCertificate(_ context.Context, rec agentca.Certificate) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ActivateErr != nil {
		return s.ActivateErr
	}
	cur, ok := s.certs[rec.Serial]
	if !ok || cur.RevokedAt != nil {
		return agentca.ErrCertificateRevoked
	}
	if cur.FirstUsedAt != nil {
		return nil
	}
	now := time.Now()
	cur.FirstUsedAt = &now
	s.certs[rec.Serial] = cur
	for serial, c := range s.certs {
		if serial != rec.Serial && c.ServerID == cur.ServerID && c.TenantID == cur.TenantID && c.RevokedAt == nil {
			c.RevokedAt = &now
			s.certs[serial] = c
		}
	}
	s.activations++
	return nil
}

// Activations is how many certificates were activated (first uses) so far.
func (s *Store) Activations() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.activations
}

// Put stores or replaces a record.
func (s *Store) Put(c agentca.Certificate) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.certs == nil {
		s.certs = map[string]agentca.Certificate{}
	}
	s.certs[c.Serial] = c
}

// Update changes a stored record in place.
func (s *Store) Update(serial string, f func(*agentca.Certificate)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.certs[serial]
	f(&c)
	s.certs[serial] = c
}

// PKI is a CA with a record store.
type PKI struct {
	CA    *agentca.CA
	Pool  *x509.CertPool
	Store *Store
}

// NewPKI generates a CA.
func NewPKI(t testing.TB) *PKI {
	t.Helper()
	ca, err := agentca.GenerateCA(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(ca.Cert)
	return &PKI{CA: ca, Pool: pool, Store: &Store{}}
}

// IssueChild is Issue for a renewal: the certificate records parent as its parent serial.
func (p *PKI) IssueChild(t testing.TB, id agentca.Identity, parent string, now time.Time, validity time.Duration) Client {
	t.Helper()
	c := p.IssueUnrecorded(t, p.CA, id, now, validity)
	c.Record.ParentSerial = parent
	p.Store.Put(c.Record)
	return c
}

// Client is an issued client certificate with its private key.
type Client struct {
	TLS    tls.Certificate
	Leaf   *x509.Certificate
	Serial string
	Record agentca.Certificate
}

// Issue signs a client certificate for id valid from now-5m to now+validity and records it.
func (p *PKI) Issue(t testing.TB, id agentca.Identity, now time.Time, validity time.Duration) Client {
	t.Helper()
	c := p.IssueUnrecorded(t, p.CA, id, now, validity)
	p.Store.Put(c.Record)
	return c
}

// IssueUnrecorded signs with ca (possibly a different CA than the PKI's) and records nothing.
func (p *PKI) IssueUnrecorded(t testing.TB, ca *agentca.CA, id agentca.Identity, now time.Time, validity time.Duration) Client {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	csrDER, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{Subject: pkix.Name{CommonName: "agent"}}, key)
	if err != nil {
		t.Fatal(err)
	}
	issued, err := ca.SignCSR(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER}), id, now, validity)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	pair, err := tls.X509KeyPair(issued.CertPEM, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}))
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	return Client{TLS: pair, Leaf: leaf, Serial: issued.Serial, Record: agentca.Certificate{
		Serial: issued.Serial, TenantID: id.TenantID, ServerID: id.ServerID,
		Fingerprint: issued.Fingerprint, NotBefore: issued.NotBefore, NotAfter: issued.NotAfter,
	}}
}
