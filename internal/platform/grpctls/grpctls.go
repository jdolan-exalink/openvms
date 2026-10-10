// Package grpctls builds transport credentials for the gRPC control channel.
package grpctls

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"

	"google.golang.org/grpc/credentials"
)

// ClientOptions configures server verification for a gRPC client.
type ClientOptions struct {
	// CAFile is a PEM bundle trusted to sign the server certificate. Empty uses system roots.
	CAFile string
	// ServerName overrides the name verified against the server certificate. Empty uses the dial target host.
	ServerName string
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

// ClientCredentials builds verifying TLS credentials (TLS 1.2+). The server
// certificate is always verified; there is no insecure-skip-verify option.
func ClientCredentials(opts ClientOptions) (credentials.TransportCredentials, error) {
	cfg := &tls.Config{
		MinVersion: tls.VersionTLS12,
		ServerName: opts.ServerName,
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
