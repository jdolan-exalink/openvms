package control

import (
	"context"
	"log/slog"
	"net"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"

	openvmsv1 "github.com/jdolan-exalink/openvms/gen/go/openvms/v1"
	"github.com/jdolan-exalink/openvms/internal/identity"
	"github.com/jdolan-exalink/openvms/internal/inventory"
	"github.com/jdolan-exalink/openvms/internal/mediasession"
	"github.com/jdolan-exalink/openvms/internal/realtime"
)

// Config configures the unified gRPC control server.
type Config struct {
	Addr      string
	RelayHost string
	RelayPort int32
	Identity  *identity.Service
	Inventory *inventory.Service
	Realtime  *realtime.Hub
	Sessions  *mediasession.Manager
	Log       *slog.Logger
	Features  []string
}

// Server coordinates all OpenVMS gRPC services.
type Server struct {
	cfg        Config
	grpcServer *grpc.Server
	listener   net.Listener
}

// NewServer instantiates and registers all Control Plane and Event Plane services.
func NewServer(cfg Config) *Server {
	if cfg.Log == nil {
		cfg.Log = slog.Default()
	}
	if cfg.Sessions == nil {
		cfg.Sessions = mediasession.NewManager(15 * time.Minute)
	}

	opts := []grpc.ServerOption{
		grpc.KeepaliveParams(keepalive.ServerParameters{
			MaxConnectionIdle:     15 * time.Minute,
			MaxConnectionAge:      2 * time.Hour,
			MaxConnectionAgeGrace: 5 * time.Minute,
			Time:                  1 * time.Minute,
			Timeout:               20 * time.Second,
		}),
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
			MinTime:             30 * time.Second,
			PermitWithoutStream: true,
		}),
		grpc.UnaryInterceptor(unaryLoggingInterceptor(cfg.Log)),
		grpc.StreamInterceptor(streamLoggingInterceptor(cfg.Log)),
	}

	s := grpc.NewServer(opts...)

	// Register services
	authSrv := &AuthServer{Identity: cfg.Identity, Features: cfg.Features}
	openvmsv1.RegisterAuthServiceServer(s, authSrv)

	siteSrv := &SiteServer{Inv: cfg.Inventory, Identity: cfg.Identity}
	openvmsv1.RegisterSiteServiceServer(s, siteSrv)

	nodeSrv := &NodeServer{
		Inv:       cfg.Inventory,
		Identity:  cfg.Identity,
		RelayHost: cfg.RelayHost,
		RelayPort: cfg.RelayPort,
	}
	openvmsv1.RegisterNodeServiceServer(s, nodeSrv)

	camSrv := &CameraServer{Inv: cfg.Inventory, Identity: cfg.Identity}
	openvmsv1.RegisterCameraServiceServer(s, camSrv)

	connSrv := &ConnectionServer{
		Inv:       cfg.Inventory,
		Identity:  cfg.Identity,
		Sessions:  cfg.Sessions,
		RelayHost: cfg.RelayHost,
		RelayPort: cfg.RelayPort,
		Log:       cfg.Log,
	}
	openvmsv1.RegisterConnectionServiceServer(s, connSrv)

	eventSrv := &EventServer{
		Hub:      cfg.Realtime,
		Identity: cfg.Identity,
		Log:      cfg.Log,
	}
	openvmsv1.RegisterEventServiceServer(s, eventSrv)

	reflection.Register(s)

	return &Server{
		cfg:        cfg,
		grpcServer: s,
	}
}

// Start listens on the given address and serves gRPC requests.
func (s *Server) Start(addr string) error {
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	s.listener = lis
	s.cfg.Log.Info("openvms gRPC control server listening", "addr", addr)
	return s.grpcServer.Serve(lis)
}

// GracefulStop gracefully shuts down the gRPC server.
func (s *Server) GracefulStop() {
	if s.grpcServer != nil {
		s.grpcServer.GracefulStop()
	}
}

// Stop terminates the gRPC server immediately.
func (s *Server) Stop() {
	if s.grpcServer != nil {
		s.grpcServer.Stop()
	}
}

// GRPCServer returns the underlying *grpc.Server.
func (s *Server) GRPCServer() *grpc.Server {
	return s.grpcServer
}

func unaryLoggingInterceptor(log *slog.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp any, err error) {
		start := time.Now()
		resp, err = handler(ctx, req)
		duration := time.Since(start)

		st, _ := status.FromError(err)
		code := st.Code()

		if code != codes.OK && code != codes.Canceled {
			log.Warn("grpc request failed",
				"method", info.FullMethod,
				"code", code.String(),
				"duration", duration,
				"error", err,
			)
		} else {
			log.Debug("grpc request completed",
				"method", info.FullMethod,
				"code", code.String(),
				"duration", duration,
			)
		}
		return resp, err
	}
}

func streamLoggingInterceptor(log *slog.Logger) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		start := time.Now()
		err := handler(srv, ss)
		duration := time.Since(start)

		st, _ := status.FromError(err)
		code := st.Code()

		if code != codes.OK && code != codes.Canceled {
			log.Warn("grpc stream closed with error",
				"method", info.FullMethod,
				"code", code.String(),
				"duration", duration,
				"error", err,
			)
		} else {
			log.Debug("grpc stream closed",
				"method", info.FullMethod,
				"code", code.String(),
				"duration", duration,
			)
		}
		return err
	}
}
