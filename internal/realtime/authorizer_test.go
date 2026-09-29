package realtime_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/realtime"
)

func TestCachedAuthorizerLoadsOncePerTTL(t *testing.T) {
	var loads atomic.Int32
	cam := uuid.New()
	now := time.Unix(1000, 0)
	a := realtime.NewCachedAuthorizer(func(_ context.Context, _ authz.Actor, kind authz.ScopeType, p authz.Permission) ([]uuid.UUID, error) {
		loads.Add(1)
		return []uuid.UUID{cam}, nil
	}, 30*time.Second)
	a.SetClock(func() time.Time { return now })

	actor := tenantActor(uuid.New())
	s := realtime.Scope{Kind: authz.ScopeCamera, ID: cam, Permission: authz.EventsView}
	for i := 0; i < 3; i++ {
		if ok, err := a.Allow(context.Background(), actor, s); err != nil || !ok {
			t.Fatalf("Allow = %v, %v", ok, err)
		}
	}
	if loads.Load() != 1 {
		t.Fatalf("loads = %d, want 1 within the TTL", loads.Load())
	}
	if ok, _ := a.Allow(context.Background(), actor, realtime.Scope{Kind: authz.ScopeCamera, ID: uuid.New(), Permission: authz.EventsView}); ok {
		t.Fatal("a camera outside the authorized set was allowed")
	}
	now = now.Add(31 * time.Second)
	_, _ = a.Allow(context.Background(), actor, s)
	if loads.Load() != 2 {
		t.Fatalf("loads = %d, want a reload after the TTL", loads.Load())
	}
}

func TestCachedAuthorizerSeparatesUsersAndPermissions(t *testing.T) {
	cam := uuid.New()
	a := realtime.NewCachedAuthorizer(func(_ context.Context, actor authz.Actor, _ authz.ScopeType, p authz.Permission) ([]uuid.UUID, error) {
		if actor.Username == "allowed" && p == authz.EventsView {
			return []uuid.UUID{cam}, nil
		}
		return nil, nil
	}, time.Minute)
	s := realtime.Scope{Kind: authz.ScopeCamera, ID: cam, Permission: authz.EventsView}
	tenant := uuid.New()
	allowed := authz.Actor{UserID: uuid.New(), Username: "allowed", TenantID: &tenant}
	denied := authz.Actor{UserID: uuid.New(), Username: "denied", TenantID: &tenant}
	if ok, _ := a.Allow(context.Background(), allowed, s); !ok {
		t.Fatal("allowed user was denied")
	}
	if ok, _ := a.Allow(context.Background(), denied, s); ok {
		t.Fatal("a cached grant leaked to another user")
	}
	if ok, _ := a.Allow(context.Background(), allowed, realtime.Scope{Kind: authz.ScopeCamera, ID: cam, Permission: authz.LPRView}); ok {
		t.Fatal("a cached grant leaked to another permission")
	}
}

func TestCachedAuthorizerErrorsAreNotCached(t *testing.T) {
	var calls atomic.Int32
	cam := uuid.New()
	a := realtime.NewCachedAuthorizer(func(context.Context, authz.Actor, authz.ScopeType, authz.Permission) ([]uuid.UUID, error) {
		if calls.Add(1) == 1 {
			return nil, context.DeadlineExceeded
		}
		return []uuid.UUID{cam}, nil
	}, time.Minute)
	actor := tenantActor(uuid.New())
	s := realtime.Scope{Kind: authz.ScopeCamera, ID: cam, Permission: authz.EventsView}
	if ok, err := a.Allow(context.Background(), actor, s); ok || err == nil {
		t.Fatalf("first call = %v, %v; want denied with error", ok, err)
	}
	if ok, err := a.Allow(context.Background(), actor, s); !ok || err != nil {
		t.Fatalf("retry = %v, %v; want allowed", ok, err)
	}
}

func TestCachedAuthorizerRejectsUnsupportedScope(t *testing.T) {
	a := realtime.NewCachedAuthorizer(func(context.Context, authz.Actor, authz.ScopeType, authz.Permission) ([]uuid.UUID, error) {
		return []uuid.UUID{uuid.Nil}, nil
	}, time.Minute)
	ok, err := a.Allow(context.Background(), tenantActor(uuid.New()), realtime.Scope{Kind: authz.ScopeTenant, ID: uuid.Nil, Permission: authz.EventsView})
	if ok || err == nil {
		t.Fatalf("unsupported kind = %v, %v; want denied with error", ok, err)
	}
}
