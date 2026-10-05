package agent

import (
	"context"
	"net"
	"sync/atomic"
	"testing"
	"time"

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

	w := NewWorker(WorkerConfig{
		NodeID:            "test-node-01",
		ControlGRPCAddr:   lis.Addr().String(),
		HeartbeatInterval: 50 * time.Millisecond,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		_ = w.Run(ctx)
	}()

	// Wait for heartbeat with timeout
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if mockSrv.heartbeatCount.Load() > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	cancel()

	count := mockSrv.heartbeatCount.Load()
	if count < 1 {
		t.Errorf("expected at least 1 heartbeat, got %d", count)
	}
}
