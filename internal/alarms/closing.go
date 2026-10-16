package alarms

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

// systemActor is the audit actor name of changes made by the platform itself.
const systemActor = "system"

// ClosedAlarm identifies an alarm closed by the system, for the post-commit realtime update.
type ClosedAlarm struct {
	ID, TenantID, SiteID, CameraID, EventID uuid.UUID
	UpdatedAt                               time.Time
}

// autoClose closes one open/acknowledged alarm as the system: status, system transition and audit
// entry. It reports false when the alarm is no longer closable (a person moved it meanwhile).
func autoClose(ctx context.Context, q *db.Queries, id uuid.UUID, now time.Time, window time.Duration) (ClosedAlarm, bool, error) {
	row, err := q.AutoCloseAlarm(ctx, db.AutoCloseAlarmParams{ID: id, ClosedAt: now})
	if errors.Is(err, pgx.ErrNoRows) {
		return ClosedAlarm{}, false, nil
	}
	if err != nil {
		return ClosedAlarm{}, false, store.Classify(err)
	}
	to := "closed"
	comment := fmt.Sprintf("auto-closed after %d minutes without activity", int(window.Minutes()))
	if _, err := q.CreateAlarmTransition(ctx, db.CreateAlarmTransitionParams{
		TenantID: row.TenantID, AlarmID: row.ID, FromStatus: &row.FromStatus, ToStatus: &to, Comment: comment,
	}); err != nil {
		return ClosedAlarm{}, false, store.Classify(err)
	}
	details, err := json.Marshal(map[string]any{"previous_status": row.FromStatus, "comment": comment, "auto_closed": true})
	if err != nil {
		return ClosedAlarm{}, false, err
	}
	if err := q.InsertAudit(ctx, db.InsertAuditParams{
		TenantID: &row.TenantID, ActorName: systemActor, Action: ActionAlarmClosed,
		TargetType: "alarm", TargetID: &row.ID, Details: details,
	}); err != nil {
		return ClosedAlarm{}, false, store.Classify(err)
	}
	return ClosedAlarm{ID: row.ID, TenantID: row.TenantID, SiteID: row.SiteID, CameraID: row.CameraID, EventID: row.EventID, UpdatedAt: row.UpdatedAt}, true, nil
}

// PublishAlarm publishes alarm.<action>.<tenant> in the shape the realtime feed decodes. It is
// best effort: a failed publish is logged and never fails the caller.
func PublishAlarm(ctx context.Context, pub Publisher, log *slog.Logger, action, status string, tenantID, siteID, cameraID, eventID, alarmID uuid.UUID, updatedAt time.Time) {
	if pub == nil {
		return
	}
	data, err := json.Marshal(map[string]any{
		"id": alarmID, "tenant_id": tenantID, "site_id": siteID, "camera_id": cameraID,
		"event_id": eventID, "status": status, "updated_at": updatedAt,
	})
	if err != nil {
		return
	}
	subject := fmt.Sprintf("alarm.%s.%s", action, tenantID)
	if err := pub.Publish(ctx, subject, data); err != nil && log != nil {
		log.WarnContext(ctx, "publish alarm event", "subject", subject, "error", err)
	}
}
