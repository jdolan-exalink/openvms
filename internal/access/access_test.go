package access

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jdolan-exalink/openvms/internal/authz"
)

func TestSystemActorAccess(t *testing.T) {
	ctx := context.Background()
	actor := authz.SystemActor()

	c, err := Load(ctx, nil, actor)
	if err != nil {
		t.Fatalf("Load with SystemActor failed: %v", err)
	}

	res := authz.Resource{
		Kind:     authz.ScopeServer,
		ID:       uuid.New(),
		TenantID: uuid.New(),
	}

	if !c.Can(authz.ServersManage, res) {
		t.Errorf("SystemActor should have ServersManage")
	}
	if !c.Can(authz.CamerasManage, res) {
		t.Errorf("SystemActor should have CamerasManage")
	}
	if err := c.Require(authz.ServersManage, res); err != nil {
		t.Errorf("SystemActor Require should succeed, got: %v", err)
	}
}
