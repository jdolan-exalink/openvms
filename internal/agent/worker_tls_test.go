package agent

import (
	"context"
	"net"
	"path/filepath"
	"testing"

	"google.golang.org/grpc"

	openvmsv1 "github.com/jdolan-exalink/openvms/gen/go/openvms/v1"
	"github.com/jdolan-exalink/openvms/internal/platform/grpctls"
	"github.com/jdolan-exalink/openvms/internal/platform/grpctls/grpctlstest"
)

// heartbeatsAgainstTLSServer runs a worker against a TLS node server and returns the heartbeats it delivered.
func heartbeatsAgainstTLSServer(t *testing.T, serverCert, serverKey string, cfg WorkerConfig) int32 {
	t.Helper()
	creds, err := grpctls.ServerCredentials(serverCert, serverKey)
	if err != nil {
		t.Fatal(err)
	}
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := grpc.NewServer(grpc.Creds(creds))
	mock := &mockNodeServer{}
	openvmsv1.RegisterNodeServiceServer(srv, mock)
	go func() { _ = srv.Serve(lis) }()
	defer srv.Stop()

	cfg.NodeID = "tls-node"
	cfg.ControlGRPCAddr = lis.Addr().String()
	d := newBeatDriver()
	d.wire(&cfg)
	w := NewWorker(cfg)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = w.Run(ctx); close(done) }()
	// The initial heartbeat is the whole experiment: it either reaches the server or fails.
	_ = d.next(t)
	cancel()
	<-done
	return mock.heartbeatCount.Load()
}

func TestWorker_TLS(t *testing.T) {
	cert, key := grpctlstest.WriteSelfSigned(t)

	if n := heartbeatsAgainstTLSServer(t, cert, key, WorkerConfig{TLS: true, TLSCAFile: cert}); n < 1 {
		t.Errorf("TLS worker trusting the CA delivered %d heartbeats, want >= 1", n)
	}
	if n := heartbeatsAgainstTLSServer(t, cert, key, WorkerConfig{}); n != 0 {
		t.Errorf("plaintext worker delivered %d heartbeats to a TLS server, want 0", n)
	}
	otherCA, _ := grpctlstest.WriteSelfSigned(t)
	if n := heartbeatsAgainstTLSServer(t, cert, key, WorkerConfig{TLS: true, TLSCAFile: otherCA}); n != 0 {
		t.Errorf("TLS worker with an untrusted CA delivered %d heartbeats, want 0", n)
	}
}

func TestWorker_TLSInvalidCAFileFailsRun(t *testing.T) {
	w := NewWorker(WorkerConfig{
		ControlGRPCAddr: "127.0.0.1:1",
		TLS:             true,
		TLSCAFile:       filepath.Join(t.TempDir(), "missing.pem"),
	})
	if err := w.Run(context.Background()); err == nil {
		t.Fatal("expected Run to fail on a missing CA file")
	}
}
