package control

import (
	"context"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	openvmsv1 "github.com/jdolan-exalink/openvms/gen/go/openvms/v1"
	"github.com/jdolan-exalink/openvms/internal/authz"
)

func TestAuthServer_Handshake(t *testing.T) {
	srv := &AuthServer{
		Features: []string{"live", "playback", "webrtc"},
	}

	req := &openvmsv1.HandshakeRequest{
		Client: &openvmsv1.ClientInfo{
			DeviceId: "dev-123",
			Name:     "Test Desktop",
			Platform: "windows",
		},
	}

	resp, err := srv.Handshake(context.Background(), req)
	if err != nil {
		t.Fatalf("Handshake failed: %v", err)
	}

	if resp.ProtocolVersion != 1 {
		t.Errorf("expected protocol version 1, got %d", resp.ProtocolVersion)
	}

	if len(resp.Features) != 3 {
		t.Errorf("expected 3 features, got %d", len(resp.Features))
	}
}

func TestParseHostPort(t *testing.T) {
	tests := []struct {
		url      string
		wantHost string
		wantPort int32
	}{
		{"http://192.168.1.100:5000", "192.168.1.100", 5000},
		{"https://frigate.lan:8971", "frigate.lan", 8971},
		{"http://node01.local", "node01.local", 80},
		{"https://node01.local", "node01.local", 443},
	}

	for _, tt := range tests {
		h, p := parseHostPort(tt.url)
		if h != tt.wantHost || p != tt.wantPort {
			t.Errorf("parseHostPort(%q) = (%q, %d), want (%q, %d)", tt.url, h, p, tt.wantHost, tt.wantPort)
		}
	}
}

func TestNodeServer_Heartbeat(t *testing.T) {
	srv := &NodeServer{}
	req := &openvmsv1.NodeHeartbeatRequest{
		NodeId:    "node-1",
		Version:   "0.14.0",
		Timestamp: time.Now().Unix(),
	}

	resp, err := srv.Heartbeat(context.Background(), req)
	if err != nil {
		t.Fatalf("Heartbeat failed: %v", err)
	}
	if !resp.Acknowledged {
		t.Errorf("expected acknowledged true")
	}
}

func TestConnectionServer_Telemetry(t *testing.T) {
	srv := &ConnectionServer{}
	req := &openvmsv1.ReportNetworkTelemetryRequest{
		SessionId:      "sess-1",
		ClientDeviceId: "dev-1",
		NodeId:         "node-1",
		RttMs:          12.5,
		JitterMs:       1.2,
		PacketLossPct:  0.0,
		ThroughputKbps: 45000,
	}

	resp, err := srv.ReportNetworkTelemetry(context.Background(), req)
	if err != nil {
		t.Fatalf("ReportNetworkTelemetry failed: %v", err)
	}
	if !resp.Acknowledged {
		t.Errorf("expected acknowledged true")
	}
}

func TestEventMapping(t *testing.T) {
	if got := mapStringToProtoEventType("camera.status_changed"); got != openvmsv1.EventType_EVENT_TYPE_CAMERA_STATUS {
		t.Errorf("expected CAMERA_STATUS, got %v", got)
	}
	if got := mapStringToProtoEventType("server.status"); got != openvmsv1.EventType_EVENT_TYPE_NODE_STATUS {
		t.Errorf("expected NODE_STATUS, got %v", got)
	}
	if got := mapStringToProtoEventType("alarm.active"); got != openvmsv1.EventType_EVENT_TYPE_ALARM {
		t.Errorf("expected ALARM, got %v", got)
	}
}

func TestGRPCServer_Integration(t *testing.T) {
	cfg := Config{
		Features: []string{"live", "playback", "webrtc", "relay"},
	}

	server := NewServer(cfg)

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen on dynamic port: %v", err)
	}
	addr := lis.Addr().String()

	go func() {
		_ = server.GRPCServer().Serve(lis)
	}()
	defer server.GracefulStop()

	// Dial gRPC server directly
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("failed to dial gRPC: %v", err)
	}
	defer conn.Close()

	authClient := openvmsv1.NewAuthServiceClient(conn)
	hsResp, err := authClient.Handshake(context.Background(), &openvmsv1.HandshakeRequest{
		Client: &openvmsv1.ClientInfo{
			DeviceId: "test-client",
			Platform: "linux",
		},
	})
	if err != nil {
		t.Fatalf("Handshake RPC failed: %v", err)
	}

	if hsResp.ProtocolVersion != ProtocolVersion {
		t.Errorf("expected protocol version %d, got %d", ProtocolVersion, hsResp.ProtocolVersion)
	}

	connClient := openvmsv1.NewConnectionServiceClient(conn)
	telemResp, err := connClient.ReportNetworkTelemetry(context.Background(), &openvmsv1.ReportNetworkTelemetryRequest{
		SessionId: "s-1",
	})
	if err != nil {
		t.Fatalf("ReportNetworkTelemetry RPC failed: %v", err)
	}
	if !telemResp.Acknowledged {
		t.Errorf("expected telem acknowledged true")
	}
}

func TestActorFromContext(t *testing.T) {
	ctx := context.Background()
	_, ok := ActorFrom(ctx)
	if ok {
		t.Errorf("expected no actor from empty context")
	}

	dummy := authz.Actor{Username: "operator"}
	ctxWithActor := WithActor(ctx, dummy)
	got, ok := ActorFrom(ctxWithActor)
	if !ok || got.Username != "operator" {
		t.Errorf("expected actor operator, got %v", got)
	}
}
