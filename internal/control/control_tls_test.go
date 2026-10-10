package control

import (
	"context"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	openvmsv1 "github.com/jdolan-exalink/openvms/gen/go/openvms/v1"
	"github.com/jdolan-exalink/openvms/internal/platform/grpctls"
	"github.com/jdolan-exalink/openvms/internal/platform/grpctls/grpctlstest"
)

func handshake(addr string, opt grpc.DialOption) error {
	conn, err := grpc.NewClient(addr, opt)
	if err != nil {
		return err
	}
	defer conn.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err = openvmsv1.NewAuthServiceClient(conn).Handshake(ctx, &openvmsv1.HandshakeRequest{
		Client: &openvmsv1.ClientInfo{DeviceId: "tls-test", Platform: "linux"},
	})
	return err
}

func TestGRPCServer_TLS(t *testing.T) {
	certFile, keyFile := grpctlstest.WriteSelfSigned(t)
	creds, err := grpctls.ServerCredentials(certFile, keyFile)
	if err != nil {
		t.Fatal(err)
	}
	server := NewServer(Config{Credentials: creds})
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = server.GRPCServer().Serve(lis) }()
	defer server.Stop()
	addr := lis.Addr().String()

	if err := handshake(addr, grpc.WithTransportCredentials(insecure.NewCredentials())); err == nil {
		t.Fatal("plaintext client reached a TLS control server")
	}
	trusted, err := grpctls.ClientCredentials(grpctls.ClientOptions{CAFile: certFile})
	if err != nil {
		t.Fatal(err)
	}
	if err := handshake(addr, grpc.WithTransportCredentials(trusted)); err != nil {
		t.Fatalf("TLS client trusting the CA failed: %v", err)
	}
	otherCA, _ := grpctlstest.WriteSelfSigned(t)
	untrusted, err := grpctls.ClientCredentials(grpctls.ClientOptions{CAFile: otherCA})
	if err != nil {
		t.Fatal(err)
	}
	if err := handshake(addr, grpc.WithTransportCredentials(untrusted)); err == nil {
		t.Fatal("TLS client accepted an untrusted server certificate")
	}
}
