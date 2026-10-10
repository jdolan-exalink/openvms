// Package agentca is the internal certificate authority that gives every edge agent a
// unique, revocable X.509 identity. The identity (tenant and server) is encoded in the
// leaf certificate by the CA alone; nothing the agent puts in its CSR reaches the
// certificate except its public key.
package agentca

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	// DefaultLeafValidity is the lifetime of an agent certificate; agents renew well before it ends.
	DefaultLeafValidity = 30 * 24 * time.Hour
	// CAValidity is the lifetime of the CA certificate (CA key rotation is out of scope).
	CAValidity = 10 * 365 * 24 * time.Hour

	// clockSkew backdates NotBefore so agents with a slightly slow clock still trust a fresh certificate.
	clockSkew = 5 * time.Minute

	identityScheme = "spiffe"
	identityHost   = "openvms"
	minRSABits     = 2048
)

// Identity is who an agent certificate speaks for. server_agents is 1:1 with frigate_servers,
// so the server alone identifies the agent; the tenant is carried so listeners need no lookup.
type Identity struct {
	TenantID uuid.UUID
	ServerID uuid.UUID
}

// URI is the URI SAN embedded in the leaf: spiffe://openvms/tenant/<tenant>/server/<server>.
func (id Identity) URI() *url.URL {
	return &url.URL{
		Scheme: identityScheme,
		Host:   identityHost,
		Path:   "/tenant/" + id.TenantID.String() + "/server/" + id.ServerID.String(),
	}
}

// IdentityFromCert extracts the identity from a certificate the caller has already verified
// against the CA pool. It accepts exactly one URI SAN of the expected shape.
func IdentityFromCert(c *x509.Certificate) (Identity, error) {
	if len(c.URIs) != 1 {
		return Identity{}, fmt.Errorf("agentca: certificate has %d URI SANs, want 1", len(c.URIs))
	}
	u := c.URIs[0]
	if u.Scheme != identityScheme || u.Host != identityHost || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return Identity{}, errors.New("agentca: URI SAN is not an openvms agent identity")
	}
	parts := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
	if len(parts) != 4 || parts[0] != "tenant" || parts[2] != "server" {
		return Identity{}, errors.New("agentca: URI SAN path is not tenant/<id>/server/<id>")
	}
	tenant, err := uuid.Parse(parts[1])
	if err != nil || tenant == uuid.Nil {
		return Identity{}, errors.New("agentca: URI SAN has an invalid tenant id")
	}
	server, err := uuid.Parse(parts[3])
	if err != nil || server == uuid.Nil {
		return Identity{}, errors.New("agentca: URI SAN has an invalid server id")
	}
	return Identity{TenantID: tenant, ServerID: server}, nil
}

// CA holds the CA certificate and its signing key.
type CA struct {
	Cert    *x509.Certificate
	CertPEM []byte
	Key     *ecdsa.PrivateKey
}

// GenerateCA creates a new ECDSA P-256 CA valid for CAValidity from now.
func GenerateCA(now time.Time) (*CA, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("agentca: generate CA key: %w", err)
	}
	serial, err := randomSerial()
	if err != nil {
		return nil, err
	}
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "OpenVMS Agent CA", Organization: []string{"OpenVMS"}},
		NotBefore:             now.Add(-clockSkew),
		NotAfter:              now.Add(CAValidity),
		IsCA:                  true,
		BasicConstraintsValid: true,
		MaxPathLen:            0,
		MaxPathLenZero:        true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, fmt.Errorf("agentca: self-sign CA: %w", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, err
	}
	return &CA{Cert: cert, CertPEM: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), Key: key}, nil
}

// MarshalKey returns the CA private key as PKCS#8 DER, the plaintext that gets sealed.
func (ca *CA) MarshalKey() ([]byte, error) {
	return x509.MarshalPKCS8PrivateKey(ca.Key)
}

// ParseCA rebuilds a CA from its certificate PEM and PKCS#8 DER key, checking they match.
func ParseCA(certPEM, keyDER []byte) (*CA, error) {
	b, _ := pem.Decode(certPEM)
	if b == nil || b.Type != "CERTIFICATE" {
		return nil, errors.New("agentca: CA certificate is not PEM")
	}
	cert, err := x509.ParseCertificate(b.Bytes)
	if err != nil {
		return nil, fmt.Errorf("agentca: parse CA certificate: %w", err)
	}
	parsed, err := x509.ParsePKCS8PrivateKey(keyDER)
	if err != nil {
		return nil, fmt.Errorf("agentca: parse CA key: %w", err)
	}
	key, ok := parsed.(*ecdsa.PrivateKey)
	if !ok {
		return nil, errors.New("agentca: CA key is not ECDSA")
	}
	pub, ok := cert.PublicKey.(*ecdsa.PublicKey)
	if !ok || !pub.Equal(&key.PublicKey) {
		return nil, errors.New("agentca: CA key does not match the CA certificate")
	}
	return &CA{Cert: cert, CertPEM: certPEM, Key: key}, nil
}

// Issued is a signed agent certificate plus the metadata kept for revocation.
type Issued struct {
	CertPEM     []byte
	Serial      string // lowercase hex of the serial number
	Fingerprint string // lowercase hex SHA-256 of the DER certificate
	NotBefore   time.Time
	NotAfter    time.Time
}

// SignCSR signs the public key of a PEM CSR as a client certificate for id. The CSR
// signature is verified (proof of possession) but its subject and SANs are ignored: the
// identity comes only from the caller. Accepted keys: ECDSA P-256, Ed25519, RSA >= 2048.
func (ca *CA) SignCSR(csrPEM []byte, id Identity, now time.Time, validity time.Duration) (*Issued, error) {
	if id.TenantID == uuid.Nil || id.ServerID == uuid.Nil {
		return nil, errors.New("agentca: identity needs a tenant and a server")
	}
	if validity <= 0 {
		return nil, errors.New("agentca: validity must be positive")
	}
	b, _ := pem.Decode(csrPEM)
	if b == nil || b.Type != "CERTIFICATE REQUEST" {
		return nil, errors.New("agentca: CSR is not PEM")
	}
	csr, err := x509.ParseCertificateRequest(b.Bytes)
	if err != nil {
		return nil, fmt.Errorf("agentca: parse CSR: %w", err)
	}
	if err := csr.CheckSignature(); err != nil {
		return nil, fmt.Errorf("agentca: CSR signature: %w", err)
	}
	if err := checkPublicKey(csr.PublicKey); err != nil {
		return nil, err
	}
	serial, err := randomSerial()
	if err != nil {
		return nil, err
	}
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: id.ServerID.String()},
		URIs:                  []*url.URL{id.URI()},
		NotBefore:             now.Add(-clockSkew),
		NotAfter:              now.Add(validity),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca.Cert, csr.PublicKey, ca.Key)
	if err != nil {
		return nil, fmt.Errorf("agentca: sign: %w", err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, err
	}
	return &Issued{
		CertPEM:     pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		Serial:      leaf.SerialNumber.Text(16),
		Fingerprint: Fingerprint(leaf),
		NotBefore:   leaf.NotBefore,
		NotAfter:    leaf.NotAfter,
	}, nil
}

// Fingerprint is the lowercase hex SHA-256 of the DER certificate.
func Fingerprint(c *x509.Certificate) string {
	sum := sha256.Sum256(c.Raw)
	return hex.EncodeToString(sum[:])
}

func checkPublicKey(pub any) error {
	switch k := pub.(type) {
	case *ecdsa.PublicKey:
		if k.Curve != elliptic.P256() {
			return errors.New("agentca: ECDSA keys must use P-256")
		}
	case ed25519.PublicKey:
	case *rsa.PublicKey:
		if k.N.BitLen() < minRSABits {
			return fmt.Errorf("agentca: RSA keys must be at least %d bits", minRSABits)
		}
	default:
		return errors.New("agentca: unsupported public key type")
	}
	return nil
}

// randomSerial returns a positive 128-bit random serial number.
func randomSerial() (*big.Int, error) {
	n, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, fmt.Errorf("agentca: serial: %w", err)
	}
	// Force the top bit so the serial is always a full 128-bit value, never zero.
	return n.SetBit(n, 127, 1), nil
}
