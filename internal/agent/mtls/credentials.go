// Package mtls gives the edge agent its mutual-TLS identity: it enrolls with a one-time
// token, keeps the key and certificate on disk, hands the current certificate to every new
// gRPC connection and renews it before it expires. The private key is generated here and
// never leaves the agent; only a CSR is sent to the API.
package mtls

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"time"

	"github.com/jdolan-exalink/openvms/internal/agentca"
)

// Credentials is a verified agent identity: key, certificate and the CA that signed it.
type Credentials struct {
	KeyPEM  []byte
	CertPEM []byte
	CAPEM   []byte

	TLS      tls.Certificate
	Leaf     *x509.Certificate
	Identity agentca.Identity
}

// NotBefore and NotAfter bound the certificate's validity.
func (c *Credentials) NotBefore() time.Time { return c.Leaf.NotBefore }
func (c *Credentials) NotAfter() time.Time  { return c.Leaf.NotAfter }

// RenewalDue is when the certificate should be renewed: two thirds of its lifetime.
func (c *Credentials) RenewalDue() time.Time {
	life := c.Leaf.NotAfter.Sub(c.Leaf.NotBefore)
	return c.Leaf.NotBefore.Add(life * 2 / 3)
}

// Expired reports whether the certificate is past its NotAfter at now.
func (c *Credentials) Expired(now time.Time) bool { return !now.Before(c.Leaf.NotAfter) }

// NewCredentials checks that the certificate belongs to the key, carries an agent identity
// and chains to the CA, and returns the parsed result. It does not check the validity
// window: an expired certificate is still the agent's identity and is reported separately.
func NewCredentials(keyPEM, certPEM, caPEM []byte) (*Credentials, error) {
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, fmt.Errorf("certificate does not match the private key: %w", err)
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return nil, fmt.Errorf("parse certificate: %w", err)
	}
	pair.Leaf = leaf
	id, err := agentca.IdentityFromCert(leaf)
	if err != nil {
		return nil, err
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return nil, errors.New("CA bundle contains no certificate")
	}
	// Verify at the certificate's own NotBefore so only the chain and key usage are checked.
	if _, err := leaf.Verify(x509.VerifyOptions{
		Roots:       pool,
		KeyUsages:   []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		CurrentTime: leaf.NotBefore.Add(time.Second),
	}); err != nil {
		return nil, fmt.Errorf("certificate does not chain to the CA: %w", err)
	}
	return &Credentials{KeyPEM: keyPEM, CertPEM: certPEM, CAPEM: caPEM, TLS: pair, Leaf: leaf, Identity: id}, nil
}

// newKeyAndCSR generates a fresh ECDSA P-256 key and a CSR for it. The CSR subject is
// informational: the CA ignores it and takes the identity from the enrollment token or the
// authenticated certificate.
func newKeyAndCSR() (keyPEM, csrPEM []byte, err error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("generate key: %w", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal key: %w", err)
	}
	csrDER, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{Subject: pkix.Name{CommonName: "openvms-agent"}}, key)
	if err != nil {
		return nil, nil, fmt.Errorf("create CSR: %w", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER}), nil
}
