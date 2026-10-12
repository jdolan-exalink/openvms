// Package grpctls builds transport credentials for the gRPC control channel.
package grpctls

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"os"

	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

// ClientOptions configures server verification for a gRPC client.
type ClientOptions struct {
	// CAFile is a PEM bundle trusted to sign the server certificate. Empty uses system roots.
	CAFile string
	// ServerName overrides the name verified against the server certificate. Empty uses the dial target host.
	ServerName string
	// GetClientCertificate supplies the client certificate for mutual TLS. It is called on
	// every handshake, so a provider that returns the current certificate lets a renewed one
	// apply to new connections without rebuilding the credentials. Nil presents none.
	GetClientCertificate func(*tls.CertificateRequestInfo) (*tls.Certificate, error)
}

// ServerCredentials loads a PEM certificate/key pair for a TLS gRPC server (TLS 1.2+).
func ServerCredentials(certFile, keyFile string) (credentials.TransportCredentials, error) {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("load gRPC TLS key pair: %w", err)
	}
	return credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}), nil
}

// ServerMTLSCredentials is ServerCredentials plus a required client certificate: a client
// that presents none, or one not signed by a CA in clientCAs, fails the handshake. TLS
// verifies the chain and the client-auth key usage only; whether the certificate is still
// live (not revoked) is the caller's per-call check.
func ServerMTLSCredentials(certFile, keyFile string, clientCAs *x509.CertPool) (credentials.TransportCredentials, error) {
	if clientCAs == nil {
		return nil, fmt.Errorf("gRPC mTLS needs a client CA pool")
	}
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("load gRPC TLS key pair: %w", err)
	}
	return credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    clientCAs,
	}), nil
}

// ClientCredentials builds verifying TLS credentials (TLS 1.2+). The server
// certificate is always verified; there is no insecure-skip-verify option.
func ClientCredentials(opts ClientOptions) (credentials.TransportCredentials, error) {
	cfg := &tls.Config{
		MinVersion: tls.VersionTLS12,
		ServerName: opts.ServerName,

		GetClientCertificate: opts.GetClientCertificate,
	}
	if opts.CAFile != "" {
		pem, err := os.ReadFile(opts.CAFile)
		if err != nil {
			return nil, fmt.Errorf("read gRPC TLS CA file: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("gRPC TLS CA file %q contains no valid certificates", opts.CAFile)
		}
		cfg.RootCAs = pool
	}
	return credentials.NewTLS(cfg), nil
}

// ErrOptionsWithoutTLS means a CA file or server name was given for a client that has TLS off.
// Callers (the edge agent, the SDK) wrap it with their own setting names.
var ErrOptionsWithoutTLS = errors.New("grpctls: CA file and server name require TLS to be enabled")

// TransportCredentials is the one place client TLS settings are validated and turned into
// credentials: plaintext when enabled is false, verifying TLS otherwise. A CA file or server
// name without TLS is refused so a typo cannot silently leave a channel in plaintext, and with TLS
// the CA bundle is loaded now so a broken file fails at configuration time.
func TransportCredentials(enabled bool, opts ClientOptions) (credentials.TransportCredentials, error) {
	if !enabled {
		if opts.CAFile != "" || opts.ServerName != "" {
			return nil, ErrOptionsWithoutTLS
		}
		return insecure.NewCredentials(), nil
	}
	return ClientCredentials(opts)
}
