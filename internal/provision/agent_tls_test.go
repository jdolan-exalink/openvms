package provision

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestVerifiedAgentTLSConfigValidatesPeer(t *testing.T) {
	root, rootKey := testCA(t, "operator root", time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
	good := testLeaf(t, root, rootKey, "10.30.0.7", time.Now().Add(-time.Minute), time.Now().Add(time.Hour))
	wrongIP := testLeaf(t, root, rootKey, "10.30.0.8", time.Now().Add(-time.Minute), time.Now().Add(time.Hour))
	expired := testLeaf(t, root, rootKey, "10.30.0.7", time.Now().Add(-2*time.Hour), time.Now().Add(-time.Hour))
	unknownRoot, unknownKey := testCA(t, "unknown root", time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
	unknown := testLeaf(t, unknownRoot, unknownKey, "10.30.0.7", time.Now().Add(-time.Minute), time.Now().Add(time.Hour))
	client, err := NewVerifiedAgentHTTPClient("10.30.0.7", 9443, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: root.Raw}), false, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	transport, ok := client.client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("client transport has type %T, want *http.Transport", client.client.Transport)
	}
	config := transport.TLSClientConfig
	if config.MinVersion != tls.VersionTLS12 || config.InsecureSkipVerify {
		t.Fatalf("unsafe TLS config: min=%x insecure=%v", config.MinVersion, config.InsecureSkipVerify)
	}
	for _, tt := range []struct {
		name    string
		leaf    *x509.Certificate
		wantErr bool
	}{
		{name: "trusted certificate with matching IP SAN", leaf: good},
		{name: "wrong IP SAN", leaf: wrongIP, wantErr: true},
		{name: "unknown issuer", leaf: unknown, wantErr: true},
		{name: "expired certificate", leaf: expired, wantErr: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := config.VerifyConnection(tls.ConnectionState{PeerCertificates: []*x509.Certificate{tt.leaf}})
			if (err != nil) != tt.wantErr {
				t.Fatalf("VerifyConnection error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestVerifiedAgentTLSClientTrustChoicesFailClosed(t *testing.T) {
	root, _ := testCA(t, "operator root", time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
	validPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: root.Raw})
	tests := []struct {
		name    string
		ca      []byte
		system  bool
		wantErr bool
	}{
		{name: "explicit system roots", system: true},
		{name: "explicit CA bundle", ca: validPEM},
		{name: "empty CA is not implicit system roots", wantErr: true},
		{name: "empty explicit CA is rejected", ca: []byte{}, wantErr: true},
		{name: "malformed CA rejected", ca: []byte("broken pem"), wantErr: true},
		{name: "trailing non-PEM bytes rejected", ca: append(append([]byte(nil), validPEM...), []byte("junk")...), wantErr: true},
		{name: "ambiguous CA and system roots rejected", ca: validPEM, system: true, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := buildAgentTLSConfig("10.30.0.7", tt.ca, tt.system)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil && tt.system && cfg.RootCAs == nil {
				t.Fatal("explicit system root pool was not configured")
			}
			if err == nil && !tt.system && cfg.RootCAs == nil {
				t.Fatal("explicit CA pool was not configured")
			}
		})
	}
}

func TestNewVerifiedAgentHTTPClientRejectsUnregisteredAuthority(t *testing.T) {
	root, _ := testCA(t, "operator root", time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
	ca := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: root.Raw})
	for _, host := range []string{"camera.example", "::1", "", "10.30.0.7:443"} {
		if _, err := NewVerifiedAgentHTTPClient(host, 9443, ca, false, time.Second); err == nil {
			t.Errorf("host %q unexpectedly accepted", host)
		}
	}
	if _, err := NewVerifiedAgentHTTPClient("10.30.0.7", 0, ca, false, time.Second); err == nil {
		t.Fatal("port zero accepted")
	}
	if _, err := NewVerifiedAgentHTTPClient("10.30.0.7", 9443, ca, false, 0); err == nil {
		t.Fatal("zero timeout accepted")
	}
}

func TestVerifiedAgentHTTPClientUsesFixedHTTPSAuthorityAndNeverFollowsRedirect(t *testing.T) {
	root, _ := testCA(t, "operator root", time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
	ca := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: root.Raw})
	var calls atomic.Int32
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		if r.URL.Scheme != "https" || r.URL.Host != "10.30.0.7:9443" || r.URL.Path != "/v1/onvif/probe" || r.URL.RawQuery != "" {
			t.Errorf("unexpected request target: %s", r.URL)
		}
		return &http.Response{StatusCode: http.StatusFound, Header: http.Header{"Location": []string{"https://attacker.invalid/steal"}}, Body: io.NopCloser(strings.NewReader("redirect")), Request: r}, nil
	})
	base := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return nil }}
	client, err := newVerifiedAgentHTTPClient("10.30.0.7", 9443, ca, false, time.Second, base, transport)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Do(context.Background(), http.MethodPost, "/v1/onvif/probe", nil, strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound || calls.Load() != 1 {
		t.Fatalf("status=%d transport calls=%d", resp.StatusCode, calls.Load())
	}
	if base.CheckRedirect == nil {
		t.Fatal("base client's policy unexpectedly mutated")
	}
	if _, err := client.Do(context.Background(), http.MethodPost, "https://attacker.invalid/steal", nil, nil); err == nil {
		t.Fatal("absolute URL path accepted")
	}
	if _, err := client.Do(context.Background(), http.MethodPost, "/v1/onvif/probe?token=secret", nil, nil); err == nil {
		t.Fatal("query string accepted")
	}
	if calls.Load() != 1 {
		t.Fatalf("invalid paths reached transport: %d calls", calls.Load())
	}
}

func TestVerifiedAgentHTTPClientTimeoutBoundsInjectedTransport(t *testing.T) {
	root, _ := testCA(t, "operator root", time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
	ca := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: root.Raw})
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) { <-r.Context().Done(); return nil, r.Context().Err() })
	client, err := newVerifiedAgentHTTPClient("10.30.0.7", 9443, ca, false, 20*time.Millisecond, nil, transport)
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	_, err = client.Do(context.Background(), http.MethodPost, "/v1/onvif/probe", nil, nil)
	if err == nil || time.Since(started) > time.Second {
		t.Fatalf("timeout error=%v duration=%s", err, time.Since(started))
	}
}

func testCA(t *testing.T, commonName string, notBefore, notAfter time.Time) (*x509.Certificate, *ecdsa.PrivateKey) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: commonName}, NotBefore: notBefore, NotAfter: notAfter, IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return cert, key
}

func testLeaf(t *testing.T, root *x509.Certificate, rootKey *ecdsa.PrivateKey, ip string, notBefore, notAfter time.Time) *x509.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	serial, err := rand.Int(rand.Reader, big.NewInt(1<<62))
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: ip}, NotBefore: notBefore, NotAfter: notAfter, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, IPAddresses: []net.IP{net.ParseIP(ip)}}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, root, &key.PublicKey, rootKey)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return cert
}
