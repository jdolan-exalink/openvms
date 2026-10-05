package control

import (
	"context"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/identity"
)

type actorContextKey struct{}

// WithActor injects the authenticated actor into context.
func WithActor(ctx context.Context, actor authz.Actor) context.Context {
	return context.WithValue(ctx, actorContextKey{}, actor)
}

// ActorFrom extracts the authenticated actor from context.
func ActorFrom(ctx context.Context) (authz.Actor, bool) {
	a, ok := ctx.Value(actorContextKey{}).(authz.Actor)
	return a, ok
}

// ResolveActor extracts and verifies the bearer token from gRPC metadata.
func ResolveActor(ctx context.Context, idSvc *identity.Service) (authz.Actor, error) {
	if a, ok := ActorFrom(ctx); ok {
		return a, nil
	}

	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return authz.Actor{}, status.Error(codes.Unauthenticated, "metadata missing")
	}

	authHeaders := md.Get("authorization")
	if len(authHeaders) == 0 {
		return authz.Actor{}, status.Error(codes.Unauthenticated, "authorization token required")
	}

	raw := strings.TrimPrefix(authHeaders[0], "Bearer ")
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return authz.Actor{}, status.Error(codes.Unauthenticated, "empty authorization token")
	}

	tokenHash := identity.HashToken(raw)
	actor, err := idSvc.ResolveSession(ctx, tokenHash)
	if err != nil {
		return authz.Actor{}, status.Error(codes.Unauthenticated, "invalid or expired token")
	}

	return actor, nil
}
