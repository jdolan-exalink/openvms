//go:build integration

package retention_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/jdolan-exalink/openvms/internal/access"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/bootstrap"
	"github.com/jdolan-exalink/openvms/internal/inventory"
	"github.com/jdolan-exalink/openvms/internal/retention"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/testutil/demofix"
)

func auditCount(t *testing.T, env *demofix.Env, action string) int {
	t.Helper()
	var n int
	err := env.Store.TxRaw(context.Background(), store.AllTenants, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), `SELECT count(*) FROM audit_log WHERE action = $1`, action).Scan(&n)
	})
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestServiceChangesTheRetentionForAPlatformAdminAndAuditsIt(t *testing.T) {
	env := demofix.Setup(t)
	ctx := context.Background()
	svc := &retention.Service{Store: env.Store}

	before, err := svc.Get(ctx, env.Admin)
	if err != nil {
		t.Fatal(err)
	}
	if before.Settings.FaceUnknownDays != 30 || before.Volume.Measured {
		t.Fatalf("default view = %+v, want 30 days and an unmeasured volume (no path configured)", before)
	}
	days := 7
	saved, err := svc.Update(ctx, env.Admin, retention.Settings{
		FaceUnknownDays: 14, NotificationReadDays: &days, PressureCleanup: false, PressurePercent: 90,
	})
	if err != nil {
		t.Fatal(err)
	}
	if saved.FaceUnknownDays != 14 || saved.NotificationReadDays == nil || *saved.NotificationReadDays != 7 || saved.PressureCleanup || saved.PressurePercent != 90 {
		t.Fatalf("saved = %+v", saved)
	}
	if saved.UpdatedBy == nil || *saved.UpdatedBy != env.Admin.UserID {
		t.Fatalf("updated_by = %v, want the admin", saved.UpdatedBy)
	}
	after, err := svc.Get(ctx, env.Admin)
	if err != nil || after.Settings.FaceUnknownDays != 14 {
		t.Fatalf("after = %+v, err = %v", after, err)
	}
	if n := auditCount(t, env, retention.ActionUpdated); n != 1 {
		t.Fatalf("audit entries = %d, want 1", n)
	}
}

func TestServiceRejectsInvalidSettingsWithoutChangingOrAuditing(t *testing.T) {
	env := demofix.Setup(t)
	ctx := context.Background()
	svc := &retention.Service{Store: env.Store}
	_, err := svc.Update(ctx, env.Admin, retention.Settings{FaceUnknownDays: 4000, PressurePercent: 85})
	if !errors.Is(err, retention.ErrInvalid) {
		t.Fatalf("err = %v, want ErrInvalid", err)
	}
	if _, err := svc.Preview(ctx, env.Admin, -1); !errors.Is(err, retention.ErrInvalid) {
		t.Fatalf("preview err = %v, want ErrInvalid", err)
	}
	got, err := svc.Get(ctx, env.Admin)
	if err != nil || got.Settings.FaceUnknownDays != 30 {
		t.Fatalf("setting changed by a rejected update: %+v, err = %v", got, err)
	}
	if n := auditCount(t, env, retention.ActionUpdated); n != 0 {
		t.Fatalf("audit entries = %d, want 0", n)
	}
}

// A tenant administrator holds tenant.manage for their own tenant, but the retention deletes data of every
// tenant, so only the platform scope may touch it.
func TestServiceRefusesAnyoneWithoutThePlatformPermission(t *testing.T) {
	env := demofix.Setup(t)
	ctx := context.Background()
	svc := &retention.Service{Store: env.Store}
	u, _, err := bootstrap.TenantUser(ctx, env.Store, env.Demo.TenantID, "retencion")
	if err != nil {
		t.Fatal(err)
	}
	tid := env.Demo.TenantID
	if _, err := env.Svc.CreateGrant(ctx, env.Admin, inventory.GrantInput{
		SubjectType: "user", SubjectID: u.UserID, Permission: authz.TenantManage, Effect: authz.Allow, ScopeType: authz.ScopeTenant, ScopeID: &tid,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Get(ctx, u); !errors.Is(err, access.ErrForbidden) {
		t.Fatalf("Get err = %v, want forbidden", err)
	}
	if _, err := svc.Update(ctx, u, retention.Settings{FaceUnknownDays: 1, PressurePercent: 85}); !errors.Is(err, access.ErrForbidden) {
		t.Fatalf("Update err = %v, want forbidden", err)
	}
	if _, err := svc.Preview(ctx, u, 1); !errors.Is(err, access.ErrForbidden) {
		t.Fatalf("Preview err = %v, want forbidden", err)
	}
	got, err := svc.Get(ctx, env.Admin)
	if err != nil || got.Settings.FaceUnknownDays != 30 {
		t.Fatalf("a refused update changed the setting: %+v, err = %v", got, err)
	}
}

func TestServicePreviewCountsWithoutErasing(t *testing.T) {
	env := demofix.Setup(t)
	ctx := context.Background()
	svc := &retention.Service{Store: env.Store}
	err := env.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO face_sightings (tenant_id, server_id, source_file, source_seen_at, first_seen_at, crop_key, crop_content_type,
		        crop_size_bytes, crop_sha256, review_status)
		    SELECT $1, id, 'old.jpg', now() - interval '45 days', now() - interval '45 days', 'faces/crops/old', 'image/jpeg', 1000, repeat('a', 64), 'pending'
		    FROM frigate_servers WHERE tenant_id = $1 LIMIT 1`, env.Demo.TenantID)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := svc.Preview(ctx, env.Admin, 0); err != nil || got.Records != 0 || got.Bytes != 0 {
		t.Fatalf("a retention of forever must erase nothing, got %+v, err = %v", got, err)
	}
	if got, err := svc.Preview(ctx, env.Admin, 60); err != nil || got.Records != 0 {
		t.Fatalf("60 days must not reach a 45 day old face, got %+v, err = %v", got, err)
	}
	got, err := svc.Preview(ctx, env.Admin, 30)
	if err != nil || got.Records != 1 || got.Bytes != 1000 {
		t.Fatalf("30 days must count the 45 day old face once, got %+v, err = %v", got, err)
	}
	view, err := svc.Get(ctx, env.Admin)
	if err != nil || view.FacesPending.Records != 1 {
		t.Fatalf("the preview erased something: %+v, err = %v", view, err)
	}
}
