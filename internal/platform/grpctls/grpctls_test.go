package grpctls_test

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"

	"github.com/jdolan-exalink/openvms/internal/agentauth/agentauthtest"
	"github.com/jdolan-exalink/openvms/internal/agentca"
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

func TestClientCredentialsPresentTheClientCertificate(t *testing.T) {
	certFile, keyFile := grpctlstest.WriteSelfSigned(t)
	pki := agentauthtest.NewPKI(t)
	clientPair := pki.Issue(t, agentca.Identity{TenantID: uuid.New(), ServerID: uuid.New()}, time.Now(), time.Hour).TLS
	pool := pki.Pool
	creds, err := grpctls.ServerMTLSCredentials(certFile, keyFile, pool)
	if err != nil {
		t.Fatal(err)
	}
	addr := startServer(t, grpc.Creds(creds))

	t.Run("without a client certificate the handshake fails", func(t *testing.T) {
		cc, err := grpctls.ClientCredentials(grpctls.ClientOptions{CAFile: certFile})
		if err != nil {
			t.Fatal(err)
		}
		if err := check(addr, grpc.WithTransportCredentials(cc)); err == nil {
			t.Fatal("a client without a certificate reached an mTLS server")
		}
	})
	t.Run("a certificate provider is consulted for each handshake", func(t *testing.T) {
		calls := 0
		cc, err := grpctls.ClientCredentials(grpctls.ClientOptions{
			CAFile: certFile,
			GetClientCertificate: func(*tls.CertificateRequestInfo) (*tls.Certificate, error) {
				calls++
				return &clientPair, nil
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := check(addr, grpc.WithTransportCredentials(cc)); err != nil {
			t.Fatalf("client with a certificate: %v", err)
		}
		if calls == 0 {
			t.Fatal("GetClientCertificate was never called")
		}
	})
}

func TestTransportCredentialsRejectsTLSOptionsWithoutTLS(t *testing.T) {
	cert, _ := grpctlstest.WriteSelfSigned(t)
	for name, opts := range map[string]grpctls.ClientOptions{
		"CA file":     {CAFile: cert},
		"server name": {ServerName: "vms.example"},
	} {
		if _, err := grpctls.TransportCredentials(false, opts); !errors.Is(err, grpctls.ErrOptionsWithoutTLS) {
			t.Errorf("%s without TLS: err = %v, want ErrOptionsWithoutTLS", name, err)
		}
	}
	creds, err := grpctls.TransportCredentials(false, grpctls.ClientOptions{})
	if err != nil || creds.Info().SecurityProtocol != "insecure" {
		t.Fatalf("no TLS, no options: %v, %v; want plaintext credentials", creds, err)
	}
	creds, err = grpctls.TransportCredentials(true, grpctls.ClientOptions{CAFile: cert, ServerName: "localhost"})
	if err != nil || creds.Info().SecurityProtocol != "tls" {
		t.Fatalf("TLS: %v, %v; want TLS credentials", creds, err)
	}
	// With TLS the CA is loaded now, so a broken file fails at configuration time.
	if _, err := grpctls.TransportCredentials(true, grpctls.ClientOptions{CAFile: filepath.Join(t.TempDir(), "missing.pem")}); err == nil || errors.Is(err, grpctls.ErrOptionsWithoutTLS) {
		t.Fatalf("missing CA with TLS: err = %v, want a load error", err)
	}
}
