package provision

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// VerifiedAgentHTTPClient is bound to one registered IPv4 address and secure port.
// It accepts paths, never caller-supplied URLs, and cannot silently use plaintext.
type VerifiedAgentHTTPClient struct {
	baseURL string
	client  *http.Client
}

// NewVerifiedAgentHTTPClient builds an HTTPS-only client for a registered agent.
// Set useSystemRoots explicitly to use the host root store; otherwise caPEM must
// contain a non-empty bundle of trusted CA certificates.
func NewVerifiedAgentHTTPClient(registeredIPv4 string, securePort uint16, caPEM []byte, useSystemRoots bool, timeout time.Duration) (*VerifiedAgentHTTPClient, error) {
	return newVerifiedAgentHTTPClient(registeredIPv4, securePort, caPEM, useSystemRoots, timeout, nil, nil)
}

func newVerifiedAgentHTTPClient(registeredIPv4 string, securePort uint16, caPEM []byte, useSystemRoots bool, timeout time.Duration, base *http.Client, testTransport http.RoundTripper) (*VerifiedAgentHTTPClient, error) {
	ip := net.ParseIP(registeredIPv4)
	if ip == nil || ip.To4() == nil || securePort == 0 || timeout <= 0 {
		return nil, errors.New("invalid registered agent TLS configuration")
	}
	ipv4 := ip.To4().String()
	tlsConfig, err := buildAgentTLSConfig(ipv4, caPEM, useSystemRoots)
	if err != nil {
		return nil, err
	}
	transport := &http.Transport{
		Proxy:                 nil,
		TLSClientConfig:       tlsConfig,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: timeout,
	}
	var roundTripper http.RoundTripper = transport
	if testTransport != nil {
		roundTripper = testTransport
	}
	client := &http.Client{Timeout: timeout}
	if base != nil {
		*client = *base
		client.Timeout = timeout
	}
	client.Transport = roundTripper
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &VerifiedAgentHTTPClient{
		baseURL: "https://" + net.JoinHostPort(ipv4, strconv.Itoa(int(securePort))),
		client:  client,
	}, nil
}

func buildAgentTLSConfig(registeredIPv4 string, caPEM []byte, useSystemRoots bool) (*tls.Config, error) {
	ip := net.ParseIP(registeredIPv4)
	if ip == nil || ip.To4() == nil {
		return nil, errors.New("registered agent must be an IPv4 literal")
	}
	var roots *x509.CertPool
	if useSystemRoots {
		if len(caPEM) != 0 {
			return nil, errors.New("choose either system roots or an explicit agent CA bundle")
		}
		var err error
		roots, err = x509.SystemCertPool()
		if err != nil || roots == nil {
			return nil, errors.New("system certificate roots are unavailable")
		}
	} else {
		if len(caPEM) == 0 {
			return nil, errors.New("an explicit agent CA bundle is required")
		}
		var err error
		roots, err = parseAgentRoots(caPEM)
		if err != nil {
			return nil, err
		}
	}
	ipv4 := ip.To4().String()
	config := &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots, ServerName: ipv4}
	config.VerifyConnection = func(state tls.ConnectionState) error {
		return verifyAgentPeer(roots, ipv4, state)
	}
	return config, nil
}

func parseAgentRoots(data []byte) (*x509.CertPool, error) {
	pool := x509.NewCertPool()
	rest := data
	count := 0
	for len(bytes.TrimSpace(rest)) > 0 {
		trimmed := bytes.TrimSpace(rest)
		if !bytes.HasPrefix(trimmed, []byte("-----BEGIN CERTIFICATE-----")) {
			return nil, errors.New("invalid agent CA bundle")
		}
		block, remaining := pem.Decode(trimmed)
		if block == nil || block.Type != "CERTIFICATE" || len(block.Headers) != 0 {
			return nil, errors.New("invalid agent CA bundle")
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil || !cert.IsCA || !cert.BasicConstraintsValid {
			return nil, errors.New("invalid agent CA bundle")
		}
		pool.AddCert(cert)
		count++
		rest = remaining
	}
	if count == 0 {
		return nil, errors.New("invalid agent CA bundle")
	}
	return pool, nil
}

func verifyAgentPeer(roots *x509.CertPool, registeredIPv4 string, state tls.ConnectionState) error {
	if len(state.PeerCertificates) == 0 {
		return errors.New("agent TLS peer did not provide a certificate")
	}
	intermediates := x509.NewCertPool()
	for _, cert := range state.PeerCertificates[1:] {
		intermediates.AddCert(cert)
	}
	_, err := state.PeerCertificates[0].Verify(x509.VerifyOptions{
		Roots:         roots,
		Intermediates: intermediates,
		DNSName:       registeredIPv4,
		KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	})
	return err
}

// Do sends a request to a relative path on the registered agent authority.
// Absolute URLs and query strings are rejected to keep secrets out of URLs.
func (c *VerifiedAgentHTTPClient) Do(ctx context.Context, method, path string, headers http.Header, body io.Reader) (*http.Response, error) {
	if c == nil || c.client == nil {
		return nil, errors.New("verified agent client is unavailable")
	}
	parsed, err := url.ParseRequestURI(path)
	if err != nil || !strings.HasPrefix(path, "/") || parsed.IsAbs() || parsed.Host != "" || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || strings.ContainsAny(path, "\\\r\n\t") {
		return nil, errors.New("agent request path must be relative and query-free")
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return nil, errors.New("agent request is invalid")
	}
	req.Header = headers.Clone()
	if req.Header == nil {
		req.Header = make(http.Header)
	}
	return c.client.Do(req)
}
