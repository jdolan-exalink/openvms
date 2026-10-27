//go:build integration

package inventory_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/access"
	"github.com/jdolan-exalink/openvms/internal/testutil/demofix"
)

func TestCameraRestartPermissionAndAudit(t *testing.T) {
	env := demofix.Setup(t)
	ctx := context.Background()
	cam := env.Cameras["frigate-h01/acceso_norte"]

	if err := env.Svc.RequireCameraRestart(ctx, env.Admin, cam.ID); err != nil {
		t.Fatalf("admin must be allowed: %v", err)
	}
	operator := env.Actor(t, "operador")
	if err := env.Svc.RequireCameraRestart(ctx, operator, cam.ID); !errors.Is(err, access.ErrForbidden) {
		t.Fatalf("operator without servers.restart: %v, want forbidden", err)
	}
	if err := env.Svc.AuditCameraRestart(ctx, operator, cam.ID, uuid.New(), "started", "agent"); !errors.Is(err, access.ErrForbidden) {
		t.Fatalf("audit must not be forgeable without the permission: %v", err)
	}
	op := uuid.New()
	before := auditCount(t, env, "CAMERA_RESTART", cam.ID)
	for _, st := range []string{"started", "succeeded"} {
		if err := env.Svc.AuditCameraRestart(ctx, env.Admin, cam.ID, op, st, "agent"); err != nil {
			t.Fatal(err)
		}
	}
	if got := auditCount(t, env, "CAMERA_RESTART", cam.ID); got != before+2 {
		t.Fatalf("audit rows = %d, want %d", got, before+2)
	}
	if err := env.Svc.AuditCameraRestart(ctx, env.Admin, cam.ID, op, "bogus", "agent"); err == nil {
		t.Fatal("unknown status must be rejected")
	}
}
