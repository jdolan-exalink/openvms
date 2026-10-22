//go:build integration

package inventory_test

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/testutil/demofix"
)

func TestAuditCameraVideoEncodingRecordsWhoChangedWhatWithoutCredentials(t *testing.T) {
	env := demofix.Setup(t)
	ctx := context.Background()
	cam := env.Cameras["frigate-h01/plaza"]

	if err := env.Svc.AuditCameraVideoEncoding(ctx, env.Admin, cam.TenantID, cam.ID, "enc_main", "H265", "H264"); err != nil {
		t.Fatal(err)
	}

	var actor, targetType, from, to, token string
	var details string
	err := env.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			SELECT actor_name, target_type, details->>'from', details->>'to', details->>'config_token', details::text
			FROM audit_log WHERE action = 'CAMERA_VIDEO_ENCODING_CHANGED' AND target_id = $1`, cam.ID).
			Scan(&actor, &targetType, &from, &to, &token, &details)
	})
	if err != nil {
		t.Fatal(err)
	}
	if actor != env.Admin.Username || targetType != "camera" || from != "H265" || to != "H264" || token != "enc_main" {
		t.Fatalf("audit row: actor=%q target=%q from=%q to=%q token=%q", actor, targetType, from, to, token)
	}
	for _, secret := range []string{"password", "username"} {
		if strings.Contains(details, secret) {
			t.Fatalf("the audit details must not carry camera credentials: %s", details)
		}
	}
}
