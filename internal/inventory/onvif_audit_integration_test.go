//go:build integration

package inventory_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jdolan-exalink/openvms/internal/access"
	"github.com/jdolan-exalink/openvms/internal/testutil/demofix"
)

func TestAuditCameraOnvifMutation(t *testing.T) {
	env := demofix.Setup(t)
	ctx := context.Background()
	camera := env.Cameras["frigate-h01/plaza"]
	cameraID := camera.ID
	operationID := uuid.New()

	if err := env.Svc.AuditCameraOnvifMutation(ctx, env.Admin, cameraID, operationID, "ptz", "started", ""); err != nil {
		t.Fatalf("record started event: %v", err)
	}
	if err := env.Svc.AuditCameraOnvifMutation(ctx, env.Admin, cameraID, operationID, "ptz", "succeeded", ""); err != nil {
		t.Fatalf("record succeeded event: %v", err)
	}
	unsupportedCategoryOperationID := uuid.New()
	err := env.Svc.AuditCameraOnvifMutation(ctx, env.Admin, cameraID, unsupportedCategoryOperationID, "ptz", "outcome_unknown", "raw remote error")
	if err == nil {
		t.Fatal("accepted an unsupported ONVIF mutation error category")
	}
	var unsupportedCategoryRows int
	if err := env.Pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE action = 'ONVIF_MUTATION' AND details->>'operation_id' = $1`, unsupportedCategoryOperationID.String()).Scan(&unsupportedCategoryRows); err != nil {
		t.Fatal(err)
	}
	if unsupportedCategoryRows != 0 {
		t.Fatalf("unsupported error category created %d audit records", unsupportedCategoryRows)
	}

	rows, err := env.Pool.Query(ctx, `SELECT action, details, occurred_at FROM audit_log WHERE action = 'ONVIF_MUTATION' AND target_id = $1 AND details->>'operation_id' = $2 ORDER BY id`, cameraID, operationID.String())
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var statuses []string
	var timestamps []time.Time
	for rows.Next() {
		var action string
		var details []byte
		var occurredAt time.Time
		if err := rows.Scan(&action, &details, &occurredAt); err != nil {
			t.Fatal(err)
		}
		var metadata map[string]any
		if err := json.Unmarshal(details, &metadata); err != nil {
			t.Fatal(err)
		}
		if action != "ONVIF_MUTATION" || metadata["operation"] != "ptz" || metadata["operation_id"] != operationID.String() || metadata["server_id"] != camera.ServerID.String() {
			t.Fatalf("unexpected audit record: action=%q details=%s", action, details)
		}
		statuses = append(statuses, metadata["status"].(string))
		timestamps = append(timestamps, occurredAt)
		if _, ok := metadata["endpoint"]; ok {
			t.Fatalf("audit record contains endpoint: %s", details)
		}
		if _, ok := metadata["password"]; ok {
			t.Fatalf("audit record contains password: %s", details)
		}
		for _, forbidden := range []string{"username", "uri", "soap"} {
			if _, ok := metadata[forbidden]; ok {
				t.Fatalf("audit record contains %q: %s", forbidden, details)
			}
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 2 || statuses[0] != "started" || statuses[1] != "succeeded" {
		t.Fatalf("statuses = %v, want [started succeeded]", statuses)
	}
	if len(timestamps) != 2 || timestamps[0].IsZero() || timestamps[1].IsZero() || timestamps[1].Before(timestamps[0]) {
		t.Fatalf("occurred_at timestamps = %v, want ordered start and terminal timestamps", timestamps)
	}

	operator := env.Actor(t, "operador")
	err = env.Svc.AuditCameraOnvifMutation(ctx, operator, cameraID, uuid.New(), "ptz", "started", "")
	if err == nil {
		t.Fatal("operator without cameras.manage recorded a mutation")
	}
	if !errors.Is(err, access.ErrForbidden) {
		t.Fatalf("denied actor error = %v, want authorization denial", err)
	}
	var count int
	if err := env.Pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE action = 'ONVIF_MUTATION' AND details->>'operation_id' = $1`, operationID.String()).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("denied actor changed audit count to %d, want 2", count)
	}
}
