package alarms

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

// Outcome of correlating one event.
type Outcome int

const (
	// Skipped: the event is already linked to an alarm of its source (idempotent replay).
	Skipped Outcome = iota
	// Created: a new alarm was opened for the event.
	Created
	// Merged: the event joined an ongoing alarm.
	Merged
)

// maxCorrelateAttempts bounds the retry when two writers race for the same key.
const maxCorrelateAttempts = 3

// CorrelateInput describes one event to fold into an alarm.
type CorrelateInput struct {
	TenantID, SiteID, CameraID, EventID uuid.UUID
	// Source is "event" (opened by the event syncer) or "rule" (create_alarm action).
	Source string
	// RuleID is the firing rule for Source "rule"; nil for event-source alarms.
	RuleID *uuid.UUID
	Labels []string
	// At is the event's start time. The activity window is measured on event time, not wall
	// clock, so catch-up after downtime correlates the same way as live ingestion.
	At time.Time
}

// CorrelateResult is the alarm the event ended up in.
type CorrelateResult struct {
	Outcome    Outcome
	AlarmID    uuid.UUID
	Status     string
	CreatedAt  time.Time
	UpdatedAt  time.Time
	EventCount int
	// Closed lists stale alarms of the same key that were auto-closed to free it.
	Closed []ClosedAlarm
}

// CorrelationKey is source|rule_id-or-none|camera_id|label. The label of a multi-label event is
// its smallest label (lower-cased, trimmed, alphabetical), so the key does not depend on the order
// Frigate lists objects in; an event without labels uses "none".
func CorrelationKey(source string, ruleID *uuid.UUID, cameraID uuid.UUID, labels []string) string {
	rule := "none"
	if ruleID != nil {
		rule = ruleID.String()
	}
	label := "none"
	norm := make([]string, 0, len(labels))
	for _, l := range labels {
		if l = strings.ToLower(strings.TrimSpace(l)); l != "" {
			norm = append(norm, l)
		}
	}
	if len(norm) > 0 {
		sort.Strings(norm)
		label = norm[0]
	}
	return fmt.Sprintf("%s|%s|%s|%s", source, rule, cameraID, label)
}

// Correlate folds the event into the ongoing alarm of its correlation key or opens a new one,
// inside the caller's transaction. An event joins the non-terminal alarm of its key when that
// alarm's last event is within alarm_settings.auto_close_after (alarms held by a person, i.e.
// assigned or investigating, keep absorbing regardless); otherwise a new alarm is opened and any
// stale open/acknowledged alarm of the key is auto-closed first. Concurrent writers are resolved
// by the partial unique index on (tenant_id, correlation_key) plus a bounded retry.
func Correlate(ctx context.Context, tx pgx.Tx, in CorrelateInput) (CorrelateResult, error) {
	q := db.New(tx)
	linked, err := q.AlarmEventLinked(ctx, db.AlarmEventLinkedParams{EventID: in.EventID, Source: in.Source})
	if err != nil {
		return CorrelateResult{}, store.Classify(err)
	}
	if linked {
		return CorrelateResult{Outcome: Skipped}, nil
	}
	settings, err := q.GetAlarmSettings(ctx)
	if err != nil {
		return CorrelateResult{}, store.Classify(err)
	}
	window := time.Duration(settings.AutoCloseSeconds * float64(time.Second))
	key := CorrelationKey(in.Source, in.RuleID, in.CameraID, in.Labels)

	var res CorrelateResult
	for attempt := 0; attempt < maxCorrelateAttempts; attempt++ {
		cur, err := q.FindMergeableAlarm(ctx, db.FindMergeableAlarmParams{
			TenantID: in.TenantID, CorrelationKey: &key, Cutoff: in.At.Add(-window),
		})
		if err == nil {
			if err := q.LinkAlarmEvent(ctx, db.LinkAlarmEventParams{AlarmID: cur.ID, EventID: in.EventID, TenantID: in.TenantID, Source: in.Source}); err != nil {
				return CorrelateResult{}, store.Classify(err)
			}
			bumped, err := q.BumpAlarmActivity(ctx, db.BumpAlarmActivityParams{ID: cur.ID, At: in.At})
			if err != nil {
				return CorrelateResult{}, store.Classify(err)
			}
			res.Outcome, res.AlarmID, res.Status, res.CreatedAt, res.UpdatedAt, res.EventCount = Merged, bumped.ID, bumped.Status, cur.CreatedAt, bumped.UpdatedAt, int(bumped.EventCount)
			return res, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return CorrelateResult{}, store.Classify(err)
		}

		// No alarm absorbs the event. An open/acknowledged one past its window would block the
		// unique index: close it as the sweeper would.
		stale, err := q.ListStaleCorrelatedAlarms(ctx, db.ListStaleCorrelatedAlarmsParams{TenantID: in.TenantID, CorrelationKey: &key, Cutoff: in.At.Add(-window)})
		if err != nil {
			return CorrelateResult{}, store.Classify(err)
		}
		for _, id := range stale {
			c, ok, err := autoClose(ctx, q, id, time.Now(), window)
			if err != nil {
				return CorrelateResult{}, err
			}
			if ok {
				res.Closed = append(res.Closed, c)
			}
		}

		row, err := q.InsertCorrelatedAlarm(ctx, db.InsertCorrelatedAlarmParams{
			TenantID: in.TenantID, SiteID: in.SiteID, CameraID: in.CameraID, EventID: in.EventID,
			Source: in.Source, CorrelationKey: &key, At: in.At,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			continue // lost a race for the key: merge into the winner on the next attempt
		}
		if err != nil {
			return CorrelateResult{}, store.Classify(err)
		}
		if err := q.LinkAlarmEvent(ctx, db.LinkAlarmEventParams{AlarmID: row.ID, EventID: in.EventID, TenantID: in.TenantID, Source: in.Source}); err != nil {
			return CorrelateResult{}, store.Classify(err)
		}
		res.Outcome, res.AlarmID, res.Status, res.CreatedAt, res.UpdatedAt, res.EventCount = Created, row.ID, row.Status, row.CreatedAt, row.CreatedAt, 1
		return res, nil
	}
	return CorrelateResult{}, fmt.Errorf("correlate event %s: key %q still contended after %d attempts", in.EventID, key, maxCorrelateAttempts)
}
