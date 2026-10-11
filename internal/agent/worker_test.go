package agent

import (
	"context"
	"math"
	"net"
	"sync/atomic"
	"testing"

	"google.golang.org/grpc"

	openvmsv1 "github.com/jdolan-exalink/openvms/gen/go/openvms/v1"
)

func TestSignedBytesSaturatesValuesOutsideProtoRange(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   uint64
		want int64
	}{
		{name: "zero", in: 0, want: 0},
		{name: "maximum signed value", in: math.MaxInt64, want: math.MaxInt64},
		{name: "unsigned overflow", in: math.MaxUint64, want: math.MaxInt64},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := signedBytes(tc.in); got != tc.want {
				t.Fatalf("signedBytes(%d) = %d, want %d", tc.in, got, tc.want)
			}
		})
	}
}

func TestUsedBytesDoesNotUnderflowWhenFreeExceedsTotal(t *testing.T) {
	if got := usedBytes(10, 11); got != 0 {
		t.Fatalf("usedBytes(10, 11) = %d, want 0", got)
	}
	if got := usedBytes(10, 3); got != 7 {
		t.Fatalf("usedBytes(10, 3) = %d, want 7", got)
	}
}

type mockNodeServer struct {
	openvmsv1.UnimplementedNodeServiceServer
	heartbeatCount atomic.Int32
}

func (m *mockNodeServer) Heartbeat(_ context.Context, _ *openvmsv1.NodeHeartbeatRequest) (*openvmsv1.NodeHeartbeatResponse, error) {
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
