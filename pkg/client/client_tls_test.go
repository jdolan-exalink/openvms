package client

import (
	"context"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/jdolan-exalink/openvms/internal/control"
	"github.com/jdolan-exalink/openvms/internal/platform/grpctls"
	"github.com/jdolan-exalink/openvms/internal/platform/grpctls/grpctlstest"
)

func startTLSControl(t *testing.T, certFile, keyFile string) string {
	t.Helper()
	creds, err := grpctls.ServerCredentials(certFile, keyFile)
	if err != nil {
		t.Fatal(err)
	}
	server := control.NewServer(control.Config{Credentials: creds, Features: []string{"live"}})
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = server.GRPCServer().Serve(lis) }()
	t.Cleanup(server.Stop)
	return lis.Addr().String()
}

func handshakeWith(t *testing.T, cfg Config) error {
	t.Helper()
	cfg.DeviceID = "tls-desktop"
	cfg.Timeout = 2 * time.Second
	cli, err := New(cfg)
	if err != nil {
		return err
	}
	defer cli.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err = cli.Handshake(ctx)
	return err
}

func TestClient_TLS(t *testing.T) {
	cert, key := grpctlstest.WriteSelfSigned(t)
	addr := startTLSControl(t, cert, key)

	if err := handshakeWith(t, Config{ServerAddress: addr, TLS: true, TLSCAFile: cert}); err != nil {
		t.Fatalf("TLS client trusting the CA failed: %v", err)
	}
	if err := handshakeWith(t, Config{ServerAddress: addr}); err == nil {
		t.Fatal("plaintext client reached a TLS server")
	}
	otherCA, _ := grpctlstest.WriteSelfSigned(t)
	if err := handshakeWith(t, Config{ServerAddress: addr, TLS: true, TLSCAFile: otherCA}); err == nil {
		t.Fatal("TLS client accepted an untrusted server certificate")
	}
}

func TestClient_TLSInvalidCAFile(t *testing.T) {
	_, err := New(Config{ServerAddress: "127.0.0.1:1", TLS: true, TLSCAFile: filepath.Join(t.TempDir(), "missing.pem")})
	if err == nil {
		t.Fatal("expected New to fail on a missing CA file")
	}
}

func TestClient_TLSOptionsRequireTLS(t *testing.T) {
	if _, err := New(Config{ServerAddress: "127.0.0.1:1", TLSCAFile: "/ca.pem"}); err == nil {
		t.Fatal("expected New to reject TLSCAFile without TLS")
	}
}
