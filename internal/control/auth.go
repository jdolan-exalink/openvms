package control

import (
	"context"
	"errors"
	"net/netip"
	"strings"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	openvmsv1 "github.com/jdolan-exalink/openvms/gen/go/openvms/v1"
	"github.com/jdolan-exalink/openvms/internal/identity"
	"github.com/jdolan-exalink/openvms/internal/platform/buildinfo"
)

const (
	ProtocolVersion      = 1
	MinimumClientVersion = "1.0.0"
)

var defaultFeatures = []string{
	"live",
	"playback",
	"ptz",
	"audio",
	"lpr",
	"maps",
	"alarms",
	"webrtc",
	"relay",
	"adaptive_streaming",
	"video_wall",
	"multi_monitor",
}

// AuthServer implements openvmsv1.AuthServiceServer.
type AuthServer struct {
	openvmsv1.UnimplementedAuthServiceServer
	Identity *identity.Service
	Features []string
}

func (s *AuthServer) Handshake(ctx context.Context, req *openvmsv1.HandshakeRequest) (*openvmsv1.HandshakeResponse, error) {
	if req.Client == nil {
		return nil, status.Error(codes.InvalidArgument, "client info is required")
	}

	features := s.Features
	if len(features) == 0 {
		features = defaultFeatures
	}

	return &openvmsv1.HandshakeResponse{
		ServerVersion:        buildinfo.Version,
		ProtocolVersion:      ProtocolVersion,
		MinimumClientVersion: MinimumClientVersion,
		Features:             features,
	}, nil
}

func (s *AuthServer) Login(ctx context.Context, req *openvmsv1.LoginRequest) (*openvmsv1.LoginResponse, error) {
	if s.Identity == nil {
		return nil, status.Error(codes.Internal, "identity service not configured")
	}
	if strings.TrimSpace(req.Username) == "" || req.Password == "" {
		return nil, status.Error(codes.InvalidArgument, "username and password are required")
	}

	loginInput := identity.LoginInput{
		Username:  req.Username,
		Password:  req.Password,
		TOTP:      req.MfaCode,
		UserAgent: "OpenVMS-Desktop/" + req.DeviceId,
	}

	ip, err := clientIPFromContext(ctx)
	if err == nil {
		loginInput.IP = &ip
	}

	res, err := s.Identity.Login(ctx, loginInput)
	if err != nil {
		if errors.Is(err, identity.ErrInvalidCredentials) {
			return nil, status.Error(codes.Unauthenticated, "invalid username or password")
		}
		if errors.Is(err, identity.ErrLocked) {
			return nil, status.Error(codes.ResourceExhausted, "account locked due to failed attempts")
		}
		if errors.Is(err, identity.ErrMFAInvalid) {
			return nil, status.Error(codes.FailedPrecondition, "invalid MFA verification code")
		}
		return nil, status.Errorf(codes.Internal, "login failed: %v", err)
	}

	tenantStr := ""
	if res.User.TenantID != nil {
		tenantStr = res.User.TenantID.String()
	}

	role := "operator"
	if res.User.TenantID == nil {
		role = "admin"
	}

	expiresIn := int64(time.Until(res.ExpiresAt).Seconds())
	if expiresIn < 0 {
		expiresIn = 0
	}

	return &openvmsv1.LoginResponse{
		AccessToken:  res.Token,
		RefreshToken: res.Token, // V1 dual-use session token
		ExpiresIn:    expiresIn,
		User: &openvmsv1.User{
			Id:          res.User.ID.String(),
			Username:    res.User.Username,
			DisplayName: res.User.DisplayName,
			TenantId:    tenantStr,
			Permissions: []string{"live.view", "playback.view", "maps.view", "alarms.view", "ptz.control"},
			Roles:       []string{role},
		},
	}, nil
}

func (s *AuthServer) RefreshToken(ctx context.Context, req *openvmsv1.RefreshTokenRequest) (*openvmsv1.RefreshTokenResponse, error) {
	if s.Identity == nil {
		return nil, status.Error(codes.Internal, "identity service not configured")
	}
	if req.RefreshToken == "" {
		return nil, status.Error(codes.InvalidArgument, "refresh_token is required")
	}

	// Verify token hash
	tokenHash := identity.HashToken(req.RefreshToken)
	actor, err := s.Identity.ResolveSession(ctx, tokenHash)
	if err != nil {
		return nil, status.Error(codes.Unauthenticated, "invalid or expired session token")
	}
	_ = actor

	return &openvmsv1.RefreshTokenResponse{
		AccessToken:  req.RefreshToken,
		RefreshToken: req.RefreshToken,
		ExpiresIn:    int64(s.Identity.SessionTTL.Seconds()),
	}, nil
}

func (s *AuthServer) Logout(ctx context.Context, req *openvmsv1.LogoutRequest) (*openvmsv1.LogoutResponse, error) {
	if s.Identity == nil {
		return nil, status.Error(codes.Internal, "identity service not configured")
	}
	if req.AccessToken == "" {
		return &openvmsv1.LogoutResponse{Success: true}, nil
	}

	tokenHash := identity.HashToken(req.AccessToken)
	_ = s.Identity.RevokeSession(ctx, tokenHash)

	return &openvmsv1.LogoutResponse{Success: true}, nil
}

func clientIPFromContext(ctx context.Context) (netip.Addr, error) {
	// Defaults to localhost in direct call
	return netip.ParseAddr("127.0.0.1")
}

func parseUUID(s string) (uuid.UUID, error) {
	return uuid.Parse(strings.TrimSpace(s))
}
