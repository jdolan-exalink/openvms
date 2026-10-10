package control

import (
	"context"
	"errors"
	"log/slog"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"

	openvmsv1 "github.com/jdolan-exalink/openvms/gen/go/openvms/v1"
	"github.com/jdolan-exalink/openvms/internal/agentauth"
)

// AgentConfig configures the agent gRPC listener: the one edge agents reach with a client
// certificate, separate from the user and SDK listener.
type AgentConfig struct {
	// Credentials must require and verify client certificates against the agent CA
	// (grpctls.ServerMTLSCredentials).
	Credentials credentials.TransportCredentials
	// Verifier checks every call's certificate against the issued-certificate table.
	Verifier *agentauth.Verifier
	Log      *slog.Logger
}

// NewAgentServer builds the agent listener's gRPC server. It serves only what agents need
// (today, the node Heartbeat) and every call, unary or stream, goes through the Verifier
// before its handler. It refuses to be built without mutual-TLS credentials and a verifier,
// so a configuration slip cannot expose agent methods unauthenticated.
func NewAgentServer(cfg AgentConfig) (*Server, error) {
	if cfg.Credentials == nil {
		return nil, errors.New("agent gRPC server needs mutual-TLS credentials")
	}
	if cfg.Verifier == nil {
		return nil, errors.New("agent gRPC server needs a certificate verifier")
	}
	if cfg.Log == nil {
		cfg.Log = slog.Default()
	}
	server := &Server{cfg: Config{Log: cfg.Log}}
	s := grpc.NewServer(
		grpc.Creds(cfg.Credentials),
		grpc.StatsHandler(&connStatsHandler{activeConns: &server.activeConns}),
		keepaliveParams(),
		keepaliveEnforcement(),
		// Logging wraps authentication so rejected calls are logged too.
		grpc.ChainUnaryInterceptor(unaryLoggingInterceptor(cfg.Log), cfg.Verifier.UnaryInterceptor()),
		grpc.ChainStreamInterceptor(streamLoggingInterceptor(cfg.Log), cfg.Verifier.StreamInterceptor()),
	)
	openvmsv1.RegisterNodeServiceServer(s, &AgentNodeServer{})
	server.grpcServer = s
	return server, nil
}

// AgentNodeServer is the NodeService an agent sees. Embedding the unimplemented server
// leaves ListNodes and GetNode (user-facing, bearer-authenticated) unreachable here.
type AgentNodeServer struct {
	openvmsv1.UnimplementedNodeServiceServer
}

// Heartbeat accepts a heartbeat only for the server the client certificate was issued to:
// the identity comes from the certificate, never from the request. Persisting what the
// heartbeat reports is a later task.
func (s *AgentNodeServer) Heartbeat(ctx context.Context, req *openvmsv1.NodeHeartbeatRequest) (*openvmsv1.NodeHeartbeatResponse, error) {
	id, ok := agentauth.IdentityFromContext(ctx)
	if !ok {
		return nil, agentauth.ErrRejected
	}
	if req.NodeId == "" {
		return nil, status.Error(codes.InvalidArgument, "node_id is required")
	}
	nodeID, err := uuid.Parse(req.NodeId)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid node_id")
	}
	if nodeID != id.ServerID {
		return nil, status.Error(codes.PermissionDenied, "node_id does not match the client certificate")
	}
	return &openvmsv1.NodeHeartbeatResponse{Acknowledged: true}, nil
}
