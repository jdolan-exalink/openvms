package agent

import (
	"context"
	"net"
	"sync/atomic"
	"testing"

	"google.golang.org/grpc"

	openvmsv1 "github.com/jdolan-exalink/openvms/gen/go/openvms/v1"
)

type mockNodeServer struct {
	openvmsv1.UnimplementedNodeServiceServer
	heartbeatCount atomic.Int32
}

func (m *mockNodeServer) Heartbeat(ctx context.Context, req *openvmsv1.NodeHeartbeatRequest) (*openvmsv1.NodeHeartbeatResponse, error) {
	m.heartbeatCount.Add(1)
	return &openvmsv1.NodeHeartbeatResponse{
		Acknowledged: true,
		Command:      "noop",
	}, nil
}

func TestWorker_HeartbeatIntegration(t *testing.T) {
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	defer lis.Close()

	grpcSrv := grpc.NewServer()
	mockSrv := &mockNodeServer{}
	openvmsv1.RegisterNodeServiceServer(grpcSrv, mockSrv)

	go func() {
		_ = grpcSrv.Serve(lis)
	}()
	defer grpcSrv.Stop()

	d := newBeatDriver()
	cfg := WorkerConfig{NodeID: "test-node-01", ControlGRPCAddr: lis.Addr().String()}
	d.wire(&cfg)
	w := NewWorker(cfg)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = w.Run(ctx); close(done) }()

	if err := d.next(t); err != nil {
		t.Fatalf("initial heartbeat: %v", err)
	}
	if err := d.tick(t); err != nil {
		t.Fatalf("heartbeat on a tick: %v", err)
	}
	cancel()
	<-done

	if count := mockSrv.heartbeatCount.Load(); count != 2 {
		t.Errorf("control server saw %d heartbeats, want exactly the initial one and one tick", count)
	}
	if got := w.HeartbeatsAcknowledged(); got != 2 {
		t.Errorf("worker counted %d acknowledged heartbeats, want 2", got)
	}
}
