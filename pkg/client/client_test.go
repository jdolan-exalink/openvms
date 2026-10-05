package client

import (
	"context"
	"net"
	"testing"

	openvmsv1 "github.com/jdolan-exalink/openvms/gen/go/openvms/v1"
	"github.com/jdolan-exalink/openvms/internal/control"
)

func TestClient_HandshakeIntegration(t *testing.T) {
	// 1. Start test control plane server
	cfg := control.Config{
		Features: []string{"live", "playback", "webrtc", "relay", "adaptive_streaming"},
	}
	server := control.NewServer(cfg)

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	addr := lis.Addr().String()

	go func() {
		_ = server.GRPCServer().Serve(lis)
	}()
	defer server.GracefulStop()

	// 2. Initialize Desktop Client SDK
	cli, err := New(Config{
		ServerAddress: addr,
		DeviceID:      "test-desktop-uuid",
		DeviceName:    "NOC-PC-01",
		Platform:      "windows",
		Architecture:  "x64",
		AppVersion:    "1.0.0",
	})
	if err != nil {
		t.Fatalf("New client failed: %v", err)
	}
	defer cli.Close()

	// 3. Handshake
	hs, err := cli.Handshake(context.Background())
	if err != nil {
		t.Fatalf("Handshake failed: %v", err)
	}

	if hs.ProtocolVersion != 1 {
		t.Errorf("expected protocol version 1, got %d", hs.ProtocolVersion)
	}
	if len(hs.Features) != 5 {
		t.Errorf("expected 5 features, got %d", len(hs.Features))
	}
}

func TestClient_ProbeCandidates(t *testing.T) {
	cli := &Client{}

	candidates := []*openvmsv1.ConnectionCandidate{
		{
			Type:     openvmsv1.CandidateType_CANDIDATE_TYPE_RELAY,
			Address:  "127.0.0.1",
			Port:     8554,
			Priority: 50,
		},
		{
			Type:     openvmsv1.CandidateType_CANDIDATE_TYPE_WAN,
			Address:  "127.0.0.1",
			Port:     443,
			Priority: 80,
		},
		{
			Type:     openvmsv1.CandidateType_CANDIDATE_TYPE_LAN,
			Address:  "127.0.0.1",
			Port:     7443,
			Priority: 100,
		},
	}

	best, err := cli.ProbeBestCandidate(context.Background(), candidates)
	if err != nil {
		t.Fatalf("ProbeBestCandidate failed: %v", err)
	}

	// In fallback / priority ordering, LAN candidate with priority 100 is chosen
	if best.Priority != 100 || best.Type != openvmsv1.CandidateType_CANDIDATE_TYPE_LAN {
		t.Errorf("expected highest priority candidate (LAN 100), got %+v", best)
	}
}

func TestStreamHandle_AdaptiveAndMigrate(t *testing.T) {
	candLAN := &openvmsv1.ConnectionCandidate{
		Type:     openvmsv1.CandidateType_CANDIDATE_TYPE_LAN,
		Address:  "192.168.1.100",
		Port:     8554,
		Priority: 100,
	}

	candRelay := &openvmsv1.ConnectionCandidate{
		Type:      openvmsv1.CandidateType_CANDIDATE_TYPE_RELAY,
		Address:   "relay01.openvms.net",
		Port:      443,
		Priority:  50,
		AuthToken: "tok-abc",
	}

	_, cancel := context.WithCancel(context.Background())
	defer cancel()

	handle := &StreamHandle{
		cameraID:  "cam-front",
		sessionID: "sess-1",
		candidate: candLAN,
		profile:   "sub",
		cancel:    cancel,
	}

	if handle.Profile() != "sub" {
		t.Errorf("expected profile sub, got %s", handle.Profile())
	}
	if handle.StreamURL() != "rtsp://192.168.1.100:8554/cam-front_sub" {
		t.Errorf("unexpected LAN stream url: %s", handle.StreamURL())
	}

	// 1. Adaptive switch: sub -> main
	err := handle.SetProfile(context.Background(), "main")
	if err != nil {
		t.Fatalf("SetProfile failed: %v", err)
	}
	if handle.Profile() != "main" {
		t.Errorf("expected profile main after adaptive switch, got %s", handle.Profile())
	}
	if handle.StreamURL() != "rtsp://192.168.1.100:8554/cam-front_main" {
		t.Errorf("unexpected main stream url: %s", handle.StreamURL())
	}

	// 2. Migrate route: LAN -> Relay
	err = handle.Migrate(context.Background(), candRelay)
	if err != nil {
		t.Fatalf("Migrate failed: %v", err)
	}
	if handle.SelectedCandidate().Type != openvmsv1.CandidateType_CANDIDATE_TYPE_RELAY {
		t.Errorf("expected relay candidate after migration")
	}
	expectedRelayURL := "http://relay01.openvms.net:443/relay/stream?token=tok-abc"
	if handle.StreamURL() != expectedRelayURL {
		t.Errorf("unexpected relay stream url: got %s, want %s", handle.StreamURL(), expectedRelayURL)
	}

	// 3. Close
	err = handle.Close()
	if err != nil {
		t.Fatalf("Close failed: %v", err)
	}
}
