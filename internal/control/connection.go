package control

import (
	"context"
	"encoding/hex"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	openvmsv1 "github.com/jdolan-exalink/openvms/gen/go/openvms/v1"
	"github.com/jdolan-exalink/openvms/internal/identity"
	"github.com/jdolan-exalink/openvms/internal/inventory"
)

// ConnectionServer implements openvmsv1.ConnectionServiceServer.
type ConnectionServer struct {
	openvmsv1.UnimplementedConnectionServiceServer
	Inv       *inventory.Service
	Identity  *identity.Service
	RelayHost string
	RelayPort int32
	Log       *slog.Logger
}

func (s *ConnectionServer) GetConnectionCandidates(ctx context.Context, req *openvmsv1.GetConnectionCandidatesRequest) (*openvmsv1.GetConnectionCandidatesResponse, error) {
	if s.Inv == nil {
		return nil, status.Error(codes.Internal, "inventory service not configured")
	}

	actor, err := ResolveActor(ctx, s.Identity)
	if err != nil {
		return nil, err
	}

	var nodeID uuid.UUID
	if strings.TrimSpace(req.NodeId) != "" {
		parsed, err := uuid.Parse(strings.TrimSpace(req.NodeId))
		if err != nil {
			return nil, status.Error(codes.InvalidArgument, "invalid node_id")
		}
		nodeID = parsed
	} else if strings.TrimSpace(req.CameraId) != "" {
		camID, err := uuid.Parse(strings.TrimSpace(req.CameraId))
		if err != nil {
			return nil, status.Error(codes.InvalidArgument, "invalid camera_id")
		}
		cam, err := s.Inv.GetCamera(ctx, actor, camID)
		if err != nil {
			return nil, status.Errorf(codes.NotFound, "camera not found: %v", err)
		}
		nodeID = cam.ServerID
	} else {
		return nil, status.Error(codes.InvalidArgument, "node_id or camera_id is required")
	}

	srv, err := s.Inv.GetServer(ctx, actor, nodeID)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "node not found: %v", err)
	}

	host, port := parseHostPort(srv.BaseUrl)

	sessionID := uuid.NewString()
	streamToken := hex.EncodeToString(identity.HashToken(sessionID))

	candidates := make([]*openvmsv1.ConnectionCandidate, 0, 4)

	// 1. Direct LAN candidate (highest priority: 100)
	candidates = append(candidates, &openvmsv1.ConnectionCandidate{
		Type:      openvmsv1.CandidateType_CANDIDATE_TYPE_LAN,
		Address:   host,
		Port:      port,
		Protocol:  openvmsv1.TransportProtocol_TRANSPORT_PROTOCOL_RTP_UDP,
		Priority:  100,
		AuthToken: streamToken,
	})

	// 2. Direct WAN candidate (priority: 80)
	candidates = append(candidates, &openvmsv1.ConnectionCandidate{
		Type:      openvmsv1.CandidateType_CANDIDATE_TYPE_WAN,
		Address:   host,
		Port:      port,
		Protocol:  openvmsv1.TransportProtocol_TRANSPORT_PROTOCOL_WEBRTC,
		Priority:  80,
		AuthToken: streamToken,
	})

	// 3. P2P candidate (priority: 65)
	candidates = append(candidates, &openvmsv1.ConnectionCandidate{
		Type:      openvmsv1.CandidateType_CANDIDATE_TYPE_P2P,
		Address:   host,
		Port:      port,
		Protocol:  openvmsv1.TransportProtocol_TRANSPORT_PROTOCOL_WEBRTC,
		Priority:  65,
		AuthToken: streamToken,
	})

	// 4. Central Relay fallback candidate (priority: 50)
	relayHost := s.RelayHost
	relayPort := s.RelayPort
	if relayHost == "" {
		relayHost = host
	}
	if relayPort == 0 {
		relayPort = 8554
	}

	candidates = append(candidates, &openvmsv1.ConnectionCandidate{
		Type:      openvmsv1.CandidateType_CANDIDATE_TYPE_RELAY,
		Address:   relayHost,
		Port:      relayPort,
		Protocol:  openvmsv1.TransportProtocol_TRANSPORT_PROTOCOL_WEBRTC,
		Priority:  50,
		AuthToken: streamToken,
	})

	return &openvmsv1.GetConnectionCandidatesResponse{
		NodeId:     nodeID.String(),
		CameraId:   req.CameraId,
		Candidates: candidates,
		SessionId:  sessionID,
	}, nil
}

func (s *ConnectionServer) ReportNetworkTelemetry(ctx context.Context, req *openvmsv1.ReportNetworkTelemetryRequest) (*openvmsv1.ReportNetworkTelemetryResponse, error) {
	if s.Log != nil {
		s.Log.Info("desktop network telemetry reported",
			"session_id", req.SessionId,
			"client_device_id", req.ClientDeviceId,
			"node_id", req.NodeId,
			"rtt_ms", req.RttMs,
			"jitter_ms", req.JitterMs,
			"packet_loss_pct", req.PacketLossPct,
			"throughput_kbps", req.ThroughputKbps,
		)
	}

	return &openvmsv1.ReportNetworkTelemetryResponse{
		Acknowledged: true,
	}, nil
}
