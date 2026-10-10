package grpctls_test

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"

	"github.com/jdolan-exalink/openvms/internal/platform/grpctls"
	"github.com/jdolan-exalink/openvms/internal/platform/grpctls/grpctlstest"
)

func startServer(t *testing.T, opts ...grpc.ServerOption) string {
	t.Helper()
	srv := grpc.NewServer(opts...)
	healthpb.RegisterHealthServer(srv, health.NewServer())
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)
	return lis.Addr().String()
}

func check(addr string, opt grpc.DialOption) error {
	conn, err := grpc.NewClient(addr, opt)
	if err != nil {
		return err
	}
	defer conn.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err = healthpb.NewHealthClient(conn).Check(ctx, &healthpb.HealthCheckRequest{})
	return err
}

func TestServerCredentials(t *testing.T) {
	certFile, keyFile := grpctlstest.WriteSelfSigned(t)
	creds, err := grpctls.ServerCredentials(certFile, keyFile)
	if err != nil {
		t.Fatalf("ServerCredentials: %v", err)
	}
	addr := startServer(t, grpc.Creds(creds))

	t.Run("plaintext client fails", func(t *testing.T) {
		if err := check(addr, grpc.WithTransportCredentials(insecure.NewCredentials())); err == nil {
			t.Fatal("plaintext client reached a TLS server")
		}
	})
	t.Run("client trusting the CA succeeds", func(t *testing.T) {
		cc, err := grpctls.ClientCredentials(grpctls.ClientOptions{CAFile: certFile})
		if err != nil {
			t.Fatal(err)
		}
		if err := check(addr, grpc.WithTransportCredentials(cc)); err != nil {
			t.Fatalf("trusted TLS client failed: %v", err)
		}
	})
	t.Run("client not trusting the cert fails", func(t *testing.T) {
		otherCA, _ := grpctlstest.WriteSelfSigned(t)
		cc, err := grpctls.ClientCredentials(grpctls.ClientOptions{CAFile: otherCA})
		if err != nil {
			t.Fatal(err)
		}
		if err := check(addr, grpc.WithTransportCredentials(cc)); err == nil {
			t.Fatal("untrusted server cert was accepted")
		}
	})
	t.Run("system roots reject self-signed cert", func(t *testing.T) {
		cc, err := grpctls.ClientCredentials(grpctls.ClientOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if err := check(addr, grpc.WithTransportCredentials(cc)); err == nil {
			t.Fatal("self-signed cert accepted with system roots")
		}
	})
	t.Run("server name override", func(t *testing.T) {
		cc, err := grpctls.ClientCredentials(grpctls.ClientOptions{CAFile: certFile, ServerName: "localhost"})
		if err != nil {
			t.Fatal(err)
		}
		if err := check(addr, grpc.WithTransportCredentials(cc)); err != nil {
			t.Fatalf("server name override failed: %v", err)
		}
		bad, err := grpctls.ClientCredentials(grpctls.ClientOptions{CAFile: certFile, ServerName: "wrong.example"})
		if err != nil {
			t.Fatal(err)
		}
		if err := check(addr, grpc.WithTransportCredentials(bad)); err == nil {
			t.Fatal("mismatched server name was accepted")
		}
	})
}

func TestServerCredentialsErrors(t *testing.T) {
	if _, err := grpctls.ServerCredentials(filepath.Join(t.TempDir(), "missing.pem"), "missing-key.pem"); err == nil {
		t.Fatal("expected error for missing cert/key files")
	}
}

func TestClientCredentialsErrors(t *testing.T) {
	if _, err := grpctls.ClientCredentials(grpctls.ClientOptions{CAFile: filepath.Join(t.TempDir(), "missing.pem")}); err == nil {
		t.Fatal("expected error for missing CA file")
	}
	empty := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(empty, []byte("not a pem"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := grpctls.ClientCredentials(grpctls.ClientOptions{CAFile: empty}); err == nil {
		t.Fatal("expected error for CA file without certificates")
	}
}
