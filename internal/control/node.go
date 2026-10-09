package control

import (
	"context"
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	openvmsv1 "github.com/jdolan-exalink/openvms/gen/go/openvms/v1"
	"github.com/jdolan-exalink/openvms/internal/identity"
	"github.com/jdolan-exalink/openvms/internal/inventory"
)

// NodeServer implements openvmsv1.NodeServiceServer.
type NodeServer struct {
	openvmsv1.UnimplementedNodeServiceServer
	Inv       *inventory.Service
	Identity  *identity.Service
	RelayHost string
	RelayPort int32
}

func (s *NodeServer) ListNodes(ctx context.Context, req *openvmsv1.ListNodesRequest) (*openvmsv1.ListNodesResponse, error) {
	if s.Inv == nil {
		return nil, status.Error(codes.Internal, "inventory service not configured")
	}

	actor, err := ResolveActor(ctx, s.Identity)
	if err != nil {
		return nil, err
	}

	var siteID *uuid.UUID
	if req.SiteId != "" {
		id, err := uuid.Parse(strings.TrimSpace(req.SiteId))
		if err != nil {
			return nil, status.Error(codes.InvalidArgument, "invalid site_id")
		}
		siteID = &id
	}

	servers, err := s.Inv.ListServers(ctx, actor, siteID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to list servers: %v", err)
	}

	nodes := make([]*openvmsv1.Node, 0, len(servers))
	for _, srv := range servers {
		nodes = append(nodes, s.serverToProto(srv))
	}

	return &openvmsv1.ListNodesResponse{
		Nodes: nodes,
		Pagination: &openvmsv1.Pagination{
			Page:       1,
			PageSize:   int32(len(nodes)),
			TotalItems: int64(len(nodes)),
			TotalPages: 1,
		},
	}, nil
}

func (s *NodeServer) GetNode(ctx context.Context, req *openvmsv1.GetNodeRequest) (*openvmsv1.GetNodeResponse, error) {
	if s.Inv == nil {
		return nil, status.Error(codes.Internal, "inventory service not configured")
	}

	actor, err := ResolveActor(ctx, s.Identity)
	if err != nil {
		return nil, err
	}

	nodeID, err := uuid.Parse(strings.TrimSpace(req.NodeId))
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid node_id")
	}

	srv, err := s.Inv.GetServer(ctx, actor, nodeID)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "node not found: %v", err)
	}

	return &openvmsv1.GetNodeResponse{
		Node: s.serverToProto(srv),
	}, nil
}

func (s *NodeServer) Heartbeat(ctx context.Context, req *openvmsv1.NodeHeartbeatRequest) (*openvmsv1.NodeHeartbeatResponse, error) {
	if req.NodeId == "" {
		return nil, status.Error(codes.InvalidArgument, "node_id is required")
	}

	// Future: update in-memory node heartbeat table / telemetry ring
	return &openvmsv1.NodeHeartbeatResponse{
		Acknowledged: true,
	}, nil
}

func (s *NodeServer) serverToProto(srv inventory.ServerView) *openvmsv1.Node {
	host, port := parseHostPort(srv.BaseUrl)

	opStatus := openvmsv1.OperationalStatus_OPERATIONAL_STATUS_UNSPECIFIED
	switch strings.ToLower(srv.Status) {
	case "online":
		opStatus = openvmsv1.OperationalStatus_OPERATIONAL_STATUS_ONLINE
	case "offline":
		opStatus = openvmsv1.OperationalStatus_OPERATIONAL_STATUS_OFFLINE
	case "degraded":
		opStatus = openvmsv1.OperationalStatus_OPERATIONAL_STATUS_DEGRADED
	}

	caps := srv.Caps()
	capabilities := []string{}
	if caps.Review {
		capabilities = append(capabilities, "review")
	}
	if caps.Preview {
		capabilities = append(capabilities, "preview")
	}
	if caps.Exports {
		capabilities = append(capabilities, "exports")
	}
	if caps.FaceRecognition {
		capabilities = append(capabilities, "face_recognition")
	}
	if caps.SemanticSearch {
		capabilities = append(capabilities, "semantic_search")
	}
	if caps.LPR {
		capabilities = append(capabilities, "lpr")
	}
	if caps.PTZ {
		capabilities = append(capabilities, "ptz")
	}
	if caps.Audio {
		capabilities = append(capabilities, "audio")
	}

	hs := srv.HealthStats()
	var totalStorage, usedStorage int64
	if hs.Recordings != nil {
		totalStorage = int64(hs.Recordings.TotalMB * 1024 * 1024)
		usedStorage = int64(hs.Recordings.UsedMB * 1024 * 1024)
	}

	relayHost := s.RelayHost
	relayPort := s.RelayPort
	if relayHost == "" {
		relayHost = host
		relayPort = 8554
	}

	return &openvmsv1.Node{
		Id:           srv.ID.String(),
		SiteId:       srv.SiteID.String(),
		Hostname:     srv.Name,
		Version:      srv.FrigateVersion,
		Status:       opStatus,
		LanAddress:   host,
		LanPort:      port,
		WanAddress:   host,
		WanPort:      port,
		RelayAddress: relayHost,
		RelayPort:    relayPort,
		Capabilities: capabilities,
		Specs: &openvmsv1.NodeSpecs{
			StorageTotalBytes: totalStorage,
			StorageUsedBytes:  usedStorage,
		},
		UptimeSeconds: hs.UptimeSeconds,
		CameraCount:   srv.CameraCount,
	}
}

func parseHostPort(rawURL string) (string, int32) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL, 0
	}
	host := u.Hostname()
	portStr := u.Port()
	if portStr == "" {
		if u.Scheme == "https" {
			return host, 443
		}
		return host, 80
	}
	p, err := strconv.Atoi(portStr)
	if err != nil {
		return host, 0
	}
	return host, int32(p)
}

func isPrivateIP(ipStr string) bool {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false
	}
	return ip.IsPrivate() || ip.IsLoopback()
}
