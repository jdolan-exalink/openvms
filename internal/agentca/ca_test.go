package agentca

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
)

var testNow = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

func newCSR(t *testing.T, key any, tmpl *x509.CertificateRequest) []byte {
	t.Helper()
	der, err := x509.CreateCertificateRequest(rand.Reader, tmpl, key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der})
}

func p256(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func testIdentity() Identity { return Identity{TenantID: uuid.New(), ServerID: uuid.New()} }

func parseLeaf(t *testing.T, certPEM []byte) *x509.Certificate {
	t.Helper()
	b, _ := pem.Decode(certPEM)
	if b == nil {
		t.Fatal("leaf is not PEM")
	}
	c, err := x509.ParseCertificate(b.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestGenerateCAProperties(t *testing.T) {
	ca, err := GenerateCA(testNow)
	if err != nil {
		t.Fatal(err)
	}
	c := ca.Cert
	if !c.IsCA || !c.BasicConstraintsValid || c.MaxPathLen != 0 || !c.MaxPathLenZero {
		t.Fatalf("CA constraints = IsCA %v MaxPathLen %d zero %v", c.IsCA, c.MaxPathLen, c.MaxPathLenZero)
	}
	if c.KeyUsage != x509.KeyUsageCertSign|x509.KeyUsageCRLSign {
		t.Fatalf("CA key usage = %v", c.KeyUsage)
	}
	if c.PublicKeyAlgorithm != x509.ECDSA {
		t.Fatalf("CA key algorithm = %v", c.PublicKeyAlgorithm)
	}
	if got := c.NotAfter.Sub(c.NotBefore); got < 9*365*24*time.Hour {
		t.Fatalf("CA validity = %v, want about 10 years", got)
	}
	if len(ca.CertPEM) == 0 {
		t.Fatal("CertPEM empty")
	}
}

func TestCAKeyRoundTrip(t *testing.T) {
	ca, err := GenerateCA(testNow)
	if err != nil {
		t.Fatal(err)
	}
	der, err := ca.MarshalKey()
	if err != nil {
		t.Fatal(err)
	}
	back, err := ParseCA(ca.CertPEM, der)
	if err != nil {
		t.Fatal(err)
	}
	if !back.Cert.Equal(ca.Cert) || !back.Key.Equal(ca.Key) {
		t.Fatal("round trip changed the CA")
	}
	other, _ := GenerateCA(testNow)
	otherDER, _ := other.MarshalKey()
	if _, err := ParseCA(ca.CertPEM, otherDER); err == nil {
		t.Fatal("ParseCA accepted a key that does not match the certificate")
	}
}

func TestSignCSRIssuesIdentityBoundClientCert(t *testing.T) {
	ca, _ := GenerateCA(testNow)
	id := testIdentity()
	agentKey := p256(t)
	// The CSR asks for foreign SANs and a foreign subject: all must be ignored.
	evil, _ := url.Parse("spiffe://openvms/tenant/" + uuid.NewString() + "/server/" + uuid.NewString())
	csr := newCSR(t, agentKey, &x509.CertificateRequest{
		Subject:  pkix.Name{CommonName: "attacker"},
		DNSNames: []string{"evil.example.com"},
		URIs:     []*url.URL{evil},
	})
	issued, err := ca.SignCSR(csr, id, testNow, DefaultLeafValidity)
	if err != nil {
		t.Fatal(err)
	}
	leaf := parseLeaf(t, issued.CertPEM)
	if leaf.Subject.CommonName != id.ServerID.String() {
		t.Fatalf("CN = %q, want server id", leaf.Subject.CommonName)
	}
	if len(leaf.DNSNames) != 0 || len(leaf.IPAddresses) != 0 || len(leaf.EmailAddresses) != 0 {
		t.Fatalf("leaf carries requested SANs: %v", leaf.DNSNames)
	}
	if len(leaf.URIs) != 1 || leaf.URIs[0].String() != id.URI().String() {
		t.Fatalf("URIs = %v, want only %s", leaf.URIs, id.URI())
	}
	if len(leaf.ExtKeyUsage) != 1 || leaf.ExtKeyUsage[0] != x509.ExtKeyUsageClientAuth {
		t.Fatalf("ExtKeyUsage = %v, want ClientAuth only", leaf.ExtKeyUsage)
	}
	if leaf.IsCA {
		t.Fatal("leaf is a CA")
	}
	if leaf.KeyUsage != x509.KeyUsageDigitalSignature {
		t.Fatalf("KeyUsage = %v", leaf.KeyUsage)
	}
	if !leaf.NotBefore.Before(testNow) {
		t.Fatalf("NotBefore %v is not backdated before %v", leaf.NotBefore, testNow)
	}
	if want := testNow.Add(DefaultLeafValidity); !leaf.NotAfter.Equal(want) {
		t.Fatalf("NotAfter = %v, want %v", leaf.NotAfter, want)
	}
	if !leaf.PublicKey.(*ecdsa.PublicKey).Equal(&agentKey.PublicKey) {
		t.Fatal("leaf public key is not the CSR key")
	}
	if issued.Serial != leaf.SerialNumber.Text(16) || issued.NotAfter != leaf.NotAfter || issued.NotBefore != leaf.NotBefore {
		t.Fatalf("Issued metadata does not match the certificate: %+v", issued)
	}
	if leaf.SerialNumber.Sign() <= 0 || leaf.SerialNumber.BitLen() < 100 {
		t.Fatalf("serial %v is not a 128-bit random value", leaf.SerialNumber)
	}
	if len(issued.Fingerprint) != 64 {
		t.Fatalf("fingerprint = %q, want sha256 hex", issued.Fingerprint)
	}
	if got := Fingerprint(leaf); got != issued.Fingerprint {
		t.Fatalf("Fingerprint(leaf) = %q, want %q", got, issued.Fingerprint)
	}
}

func TestSignCSRSerialsAreUnique(t *testing.T) {
	ca, _ := GenerateCA(testNow)
	csr := newCSR(t, p256(t), &x509.CertificateRequest{})
	a, err := ca.SignCSR(csr, testIdentity(), testNow, DefaultLeafValidity)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := ca.SignCSR(csr, testIdentity(), testNow, DefaultLeafValidity)
	if a.Serial == b.Serial {
		t.Fatal("two certificates share a serial")
	}
}

func TestSignCSRRejectsBadSignature(t *testing.T) {
	ca, _ := GenerateCA(testNow)
	csr := newCSR(t, p256(t), &x509.CertificateRequest{})
	b, _ := pem.Decode(csr)
	b.Bytes[len(b.Bytes)-1] ^= 0xff // corrupt the signature
	bad := pem.EncodeToMemory(b)
	if _, err := ca.SignCSR(bad, testIdentity(), testNow, DefaultLeafValidity); err == nil {
		t.Fatal("SignCSR accepted a CSR with a bad signature")
	}
}

func TestSignCSRRejectsGarbageAndNilIdentity(t *testing.T) {
	ca, _ := GenerateCA(testNow)
	if _, err := ca.SignCSR([]byte("not pem"), testIdentity(), testNow, DefaultLeafValidity); err == nil {
		t.Fatal("accepted garbage")
	}
	csr := newCSR(t, p256(t), &x509.CertificateRequest{})
	if _, err := ca.SignCSR(csr, Identity{}, testNow, DefaultLeafValidity); err == nil {
		t.Fatal("accepted a zero identity")
	}
	if _, err := ca.SignCSR(csr, testIdentity(), testNow, 0); err == nil {
		t.Fatal("accepted a zero validity")
	}
}

func TestSignCSRKeyPolicy(t *testing.T) {
	ca, _ := GenerateCA(testNow)
	p384, _ := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	rsa1024, _ := rsa.GenerateKey(rand.Reader, 1024)
	rsa2048, _ := rsa.GenerateKey(rand.Reader, 2048)
	_, ed, _ := ed25519.GenerateKey(rand.Reader)
	cases := []struct {
		name string
		key  any
		ok   bool
	}{
		{"ecdsa p256", p256(t), true},
		{"ed25519", ed, true},
		{"rsa 2048", rsa2048, true},
		{"rsa 1024", rsa1024, false},
		{"ecdsa p384", p384, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ca.SignCSR(newCSR(t, tc.key, &x509.CertificateRequest{}), testIdentity(), testNow, DefaultLeafValidity)
			if (err == nil) != tc.ok {
				t.Fatalf("err = %v, want ok=%v", err, tc.ok)
			}
		})
	}
}

func TestIssuedCertVerifiesAgainstCAPool(t *testing.T) {
	ca, _ := GenerateCA(testNow)
	id := testIdentity()
	issued, err := ca.SignCSR(newCSR(t, p256(t), &x509.CertificateRequest{}), id, testNow, DefaultLeafValidity)
	if err != nil {
		t.Fatal(err)
	}
	leaf := parseLeaf(t, issued.CertPEM)
	pool := x509.NewCertPool()
	pool.AddCert(ca.Cert)
	opts := x509.VerifyOptions{Roots: pool, CurrentTime: testNow.Add(time.Hour), KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}
	if _, err := leaf.Verify(opts); err != nil {
		t.Fatalf("verify: %v", err)
	}
	// Not valid as a server certificate.
	opts.KeyUsages = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
	if _, err := leaf.Verify(opts); err == nil {
		t.Fatal("client cert verified for server auth")
	}
	// Expired after the validity window.
	opts.KeyUsages = []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}
	opts.CurrentTime = testNow.Add(DefaultLeafValidity + time.Hour)
	if _, err := leaf.Verify(opts); err == nil {
		t.Fatal("expired cert verified")
	}
	// Another CA does not trust it.
	other, _ := GenerateCA(testNow)
	otherPool := x509.NewCertPool()
	otherPool.AddCert(other.Cert)
	if _, err := leaf.Verify(x509.VerifyOptions{Roots: otherPool, CurrentTime: testNow, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}); err == nil {
		t.Fatal("cert verified against an unrelated CA")
	}
	got, err := IdentityFromCert(leaf)
	if err != nil || got != id {
		t.Fatalf("IdentityFromCert = %+v, %v; want %+v", got, err, id)
	}
}

func TestIdentityFromCertRejectsForeignShapes(t *testing.T) {
	mk := func(uris ...string) *x509.Certificate {
		c := &x509.Certificate{}
		for _, u := range uris {
			p, _ := url.Parse(u)
			c.URIs = append(c.URIs, p)
		}
		return c
	}
	tid, sid := uuid.NewString(), uuid.NewString()
	good := "spiffe://openvms/tenant/" + tid + "/server/" + sid
	cases := map[string]*x509.Certificate{
		"no uri":         mk(),
		"two uris":       mk(good, good),
		"wrong scheme":   mk("https://openvms/tenant/" + tid + "/server/" + sid),
		"wrong domain":   mk("spiffe://other/tenant/" + tid + "/server/" + sid),
		"bad uuid":       mk("spiffe://openvms/tenant/x/server/" + sid),
		"extra segments": mk(good + "/extra"),
		"nil uuid":       mk("spiffe://openvms/tenant/" + uuid.Nil.String() + "/server/" + sid),
	}
	for name, c := range cases {
		if _, err := IdentityFromCert(c); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if id, err := IdentityFromCert(mk(good)); err != nil || id.TenantID.String() != tid || id.ServerID.String() != sid {
		t.Fatalf("good = %+v, %v", id, err)
	}
}

// Leaves never outlive the CA: near the end of the CA lifetime the validity is cut
// short, and an expired CA refuses to sign.
func TestSignCSRNeverOutlivesCA(t *testing.T) {
	ca, err := GenerateCA(testNow)
	if err != nil {
		t.Fatal(err)
	}
	csr := newCSR(t, p256(t), &x509.CertificateRequest{})
	nearEnd := ca.Cert.NotAfter.Add(-24 * time.Hour)
	issued, err := ca.SignCSR(csr, Identity{TenantID: uuid.New(), ServerID: uuid.New()}, nearEnd, DefaultLeafValidity)
	if err != nil {
		t.Fatal(err)
	}
	if issued.NotAfter.After(ca.Cert.NotAfter) {
		t.Fatalf("leaf NotAfter %v outlives CA NotAfter %v", issued.NotAfter, ca.Cert.NotAfter)
	}
	if leaf := parseLeaf(t, issued.CertPEM); leaf.NotAfter.After(ca.Cert.NotAfter) {
		t.Fatalf("certificate NotAfter %v outlives CA NotAfter %v", leaf.NotAfter, ca.Cert.NotAfter)
	}
	if _, err := ca.SignCSR(csr, Identity{TenantID: uuid.New(), ServerID: uuid.New()}, ca.Cert.NotAfter.Add(time.Minute), DefaultLeafValidity); err == nil {
		t.Fatal("expired CA signed a certificate")
	}
}
