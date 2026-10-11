package control

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"

	openvmsv1 "github.com/jdolan-exalink/openvms/gen/go/openvms/v1"
	"github.com/jdolan-exalink/openvms/internal/agentauth"
	"github.com/jdolan-exalink/openvms/internal/agentca"
	"github.com/jdolan-exalink/openvms/internal/agentstatus"
)

// AgentConfig configures the agent gRPC listener: the one edge agents reach with a client
// certificate, separate from the user and SDK listener.
type AgentConfig struct {
	// Credentials must require and verify client certificates against the agent CA
	// (grpctls.ServerMTLSCredentials).
	Credentials credentials.TransportCredentials
	// Verifier checks every call's certificate against the issued-certificate table.
	Verifier *agentauth.Verifier
	// Heartbeats persists what each heartbeat reports; nil acknowledges without storing.
	Heartbeats HeartbeatRecorder
	// Renewer enables AgentService.RenewCertificate; nil leaves the method Unimplemented.
	Renewer CertificateRenewer
	Log     *slog.Logger
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
	openvmsv1.RegisterNodeServiceServer(s, &AgentNodeServer{Heartbeats: cfg.Heartbeats, Log: cfg.Log})
	if cfg.Renewer != nil {
		openvmsv1.RegisterAgentServiceServer(s, &AgentCertServer{Renewer: cfg.Renewer, Log: cfg.Log})
	}
	server.grpcServer = s
	return server, nil
}

// HeartbeatRecorder stores the latest state an authenticated agent reported
// (agentstatus.Recorder).
type HeartbeatRecorder interface {
	RecordHeartbeat(ctx context.Context, id agentca.Identity, hb agentstatus.Heartbeat) error
}

// heartbeatWriteTimeout bounds the database write of one heartbeat; a slow database makes the
// heartbeat fail (the agent sends another soon) rather than pile up.
const heartbeatWriteTimeout = 3 * time.Second

// AgentNodeServer is the NodeService an agent sees. Embedding the unimplemented server
// leaves ListNodes and GetNode (user-facing, bearer-authenticated) unreachable here.
type AgentNodeServer struct {
	openvmsv1.UnimplementedNodeServiceServer
	// Heartbeats, when set, stores each accepted heartbeat.
	Heartbeats HeartbeatRecorder
	Log        *slog.Logger

	errLog logLimiter
}

// logLimiter allows one log line per key per window and counts what it suppressed.
type logLimiter struct {
	every time.Duration // defaults to a minute
	now   func() time.Time

	mu    sync.Mutex
	state map[uuid.UUID]*limiterState
}

type limiterState struct {
	last       time.Time
	suppressed int
}

// allow reports whether to log now for key and, when it does, how many calls since the last
// logged one were skipped.
func (l *logLimiter) allow(key uuid.UUID) (ok bool, suppressed int) {
	every, now := l.every, time.Now
	if every <= 0 {
		every = time.Minute
	}
	if l.now != nil {
		now = l.now
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.state == nil {
		l.state = map[uuid.UUID]*limiterState{}
	}
	t := now()
	st := l.state[key]
	if st == nil {
		l.state[key] = &limiterState{last: t}
		return true, 0
	}
	if t.Sub(st.last) < every {
		st.suppressed++
		return false, 0
	}
	suppressed, st.suppressed, st.last = st.suppressed, 0, t
	return true, suppressed
}

// Heartbeat accepts a heartbeat only for the server the client certificate was issued to:
// the identity comes from the certificate, never from the request, and what the heartbeat
// reports (version, status) is stored for that server and tenant only. A heartbeat that
// cannot be stored fails as Unavailable so the agent retries; nothing from the underlying
// error reaches it.
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
	version := strings.TrimSpace(req.Version)
	if s.Heartbeats != nil {
		wctx, cancel := context.WithTimeout(ctx, heartbeatWriteTimeout)
		defer cancel()
		err := s.Heartbeats.RecordHeartbeat(wctx, id, agentstatus.Heartbeat{Version: version, Status: statusName(req.Status)})
		switch {
		case err == nil:
		case errors.Is(err, agentstatus.ErrInvalid):
			// The agent sent something that can never be stored; retrying cannot help.
			return nil, status.Error(codes.InvalidArgument, "invalid heartbeat")
		case errors.Is(err, agentstatus.ErrUnknownServer):
			return nil, status.Error(codes.NotFound, "server not found")
		default:
			// A real outage: Unavailable so the agent retries. Logged at most once a minute per
			// server (with the number of suppressed errors), not once per heartbeat per agent.
			if ok, suppressed := s.errLog.allow(id.ServerID); ok {
				s.log().Error("agent heartbeat not recorded", "server_id", id.ServerID, "error", err, "suppressed", suppressed)
			}
			return nil, status.Error(codes.Unavailable, "heartbeat not recorded")
		}
	}
	return &openvmsv1.NodeHeartbeatResponse{Acknowledged: true}, nil
}

func (s *AgentNodeServer) log() *slog.Logger {
	if s.Log != nil {
		return s.Log
	}
	return slog.Default()
}

// statusName maps the wire enum to the stored vocabulary; anything unrecognized is "unknown".
func statusName(st openvmsv1.OperationalStatus) string {
	switch st {
	case openvmsv1.OperationalStatus_OPERATIONAL_STATUS_ONLINE:
		return agentstatus.Online
	case openvmsv1.OperationalStatus_OPERATIONAL_STATUS_OFFLINE:
		return agentstatus.Offline
	case openvmsv1.OperationalStatus_OPERATIONAL_STATUS_DEGRADED:
		return agentstatus.Degraded
	case openvmsv1.OperationalStatus_OPERATIONAL_STATUS_MAINTENANCE:
		return agentstatus.Maintenance
	}
	return agentstatus.Unknown
}
