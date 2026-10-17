package inventory

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jdolan-exalink/openvms/internal/access"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

const cameraZoneSyncMaxAttempts = 8

// ProcessCameraZoneSyncJobs claims a bounded batch and performs each operation under a
// per-zone advisory lock. Desired-state writers take the same lock, so a newer revision
// cannot commit between the final stale check and its remote write.
func (s *Service) ProcessCameraZoneSyncJobs(ctx context.Context, limit int) error {
	if limit < 1 {
		return nil
	}
	if limit > math.MaxInt32 {
		limit = math.MaxInt32
	}
	maxRows := int32(limit)
	var jobs []db.CameraZoneSyncJob
	err := s.Store.Tx(ctx, store.AllTenants, func(q *db.Queries) error {
		var err error
		jobs, err = q.ClaimCameraZoneSyncJobs(ctx, db.ClaimCameraZoneSyncJobsParams{LeaseSeconds: 120, MaxAttempts: cameraZoneSyncMaxAttempts, MaxRows: maxRows})
		return err
	})
	if err != nil {
		return err
	}
	var first error
	for _, job := range jobs {
		if err := s.processCameraZoneSyncJob(ctx, job); err != nil && first == nil {
			first = err
		}
	}
	return first
}

func (s *Service) processCameraZoneSyncJob(ctx context.Context, claimed db.CameraZoneSyncJob) error {
	return s.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, claimed.ZoneID.String()); err != nil {
			return err
		}
		q := db.New(tx)
		job, err := q.GetCameraZoneSyncJob(ctx, claimed.ZoneID)
		if err != nil {
			return store.Classify(err)
		}
		if job.Status != "processing" || job.DesiredRevision != claimed.DesiredRevision {
			return nil
		}
		target, err := q.GetCameraZoneSyncTarget(ctx, db.GetCameraZoneSyncTargetParams{ZoneID: job.ZoneID, TenantID: job.TenantID})
		if err != nil {
			return s.finishZoneJob(ctx, q, job, "failed", err.Error(), time.Time{}, nil)
		}
		// The queue records the initiating user, but authorization is deliberately reloaded at
		// execution time. A revoked/deleted user never inherits a prior permission assertion.
		if job.ActorUserID == nil {
			return s.finishZoneJob(ctx, q, job, "failed", "sync actor is unavailable", time.Time{}, nil)
		}
		user, err := q.GetUser(ctx, *job.ActorUserID)
		if err != nil || user.Status != "active" || (user.TenantID != nil && *user.TenantID != job.TenantID) {
			return s.finishZoneJob(ctx, q, job, "failed", "sync actor is no longer active in this tenant", time.Time{}, nil)
		}
		actor := authz.Actor{UserID: user.ID, Username: user.Username, TenantID: user.TenantID}
		checker, err := access.Load(ctx, q, actor)
		if err != nil {
			return s.finishZoneJob(ctx, q, job, "failed", "cannot validate current server configuration permission", time.Time{}, nil)
		}
		if err := checker.Require(authz.ServersConfig, access.Server(target.TenantID, target.SiteID, target.ServerID)); err != nil {
			return s.finishZoneJob(ctx, q, job, "failed", "servers.config permission is no longer granted", time.Time{}, nil)
		}
		info, err := s.connInfo(db.FrigateServer{ID: target.ServerID, BaseUrl: target.BaseUrl, AuthMode: target.AuthMode, Username: target.Username, PasswordSealed: target.PasswordSealed, TlsSkipVerify: target.TlsSkipVerify})
		if err != nil {
			return s.retryZoneJob(ctx, q, job, err)
		}
		adapter, err := s.Connect(ctx, info)
		if err != nil {
			return s.retryZoneJob(ctx, q, job, err)
		}
		remoteName := managedZoneKey(target.Name)
		config, err := adapter.CameraConfig(ctx, target.RemoteName, false)
		if err != nil {
			return s.retryZoneJob(ctx, q, job, err)
		}
		zones, _ := config["zones"].(map[string]any)
		current, exists := zones[remoteName]
		var previous any
		if len(job.LastApplied) > 0 {
			if err := json.Unmarshal(job.LastApplied, &previous); err != nil {
				return s.finishZoneJob(ctx, q, job, "failed", "invalid last-applied zone", time.Time{}, nil)
			}
		}
		if job.Operation == "delete" {
			// An unapplied rename leaves the last-applied zone under its previous remote key, so
			// both keys are candidates. Every existing candidate must match the last-applied
			// state before any is removed; otherwise nothing is deleted.
			keys := []string{remoteName}
			if job.PreviousRemoteName != nil {
				if oldName := managedZoneKey(*job.PreviousRemoteName); oldName != remoteName {
					keys = append(keys, oldName)
				}
			}
			var existing []string
			for _, key := range keys {
				value, ok := zones[key]
				if !ok {
					continue
				}
				if previous == nil || !jsonEqual(value, previous) {
					return s.conflictZoneJob(ctx, q, job, "remote managed zone differs from the last applied state")
				}
				existing = append(existing, key)
			}
			for _, key := range existing {
				if err := adapter.DeleteManagedCameraZone(ctx, target.RemoteName, key, true); err != nil {
					return s.retryZoneJob(ctx, q, job, err)
				}
			}
			return s.finishZoneJob(ctx, q, job, "synced", "", time.Time{}, []byte("null"))
		}
		var desired map[string]any
		if err := json.Unmarshal(job.DesiredConfig, &desired); err != nil {
			return s.finishZoneJob(ctx, q, job, "failed", "invalid persisted desired zone", time.Time{}, nil)
		}
		zoneValue, err := desiredFrigateZone(desired, config)
		if err != nil {
			return s.finishZoneJob(ctx, q, job, "failed", err.Error(), time.Time{}, nil)
		}
		if job.PreviousRemoteName != nil && *job.PreviousRemoteName != target.Name {
			oldName := managedZoneKey(*job.PreviousRemoteName)
			oldValue, oldExists := zones[oldName]
			if oldExists && (previous == nil || !jsonEqual(oldValue, previous)) {
				return s.conflictZoneJob(ctx, q, job, "renamed remote zone differs from the last applied state")
			}
			if exists && !jsonEqual(current, zoneValue) {
				return s.conflictZoneJob(ctx, q, job, "new remote zone key is already owned by different state")
			}
			if oldExists && exists && jsonEqual(current, zoneValue) {
				if err := adapter.DeleteManagedCameraZone(ctx, target.RemoteName, oldName, true); err != nil {
					return s.retryZoneJob(ctx, q, job, err)
				}
				return s.finishZoneJob(ctx, q, job, "synced", "", time.Time{}, mustJSON(zoneValue))
			}
			if !oldExists && !exists {
				return s.conflictZoneJob(ctx, q, job, "renamed remote zone disappeared before convergence")
			}
			if !exists || !jsonEqual(current, zoneValue) {
				if err := adapter.UpsertManagedCameraZone(ctx, target.RemoteName, target.Name, zoneValue, true); err != nil {
					return s.retryZoneJob(ctx, q, job, err)
				}
			}
			if oldExists {
				if err := adapter.DeleteManagedCameraZone(ctx, target.RemoteName, oldName, true); err != nil {
					return s.retryZoneJob(ctx, q, job, err)
				}
			}
			return s.finishZoneJob(ctx, q, job, "synced", "", time.Time{}, mustJSON(zoneValue))
		}
		if exists && !jsonEqual(current, previous) && !jsonEqual(current, zoneValue) {
			return s.conflictZoneJob(ctx, q, job, "remote managed zone differs from the last applied state")
		}
		if !exists && previous != nil {
			return s.conflictZoneJob(ctx, q, job, "remote managed zone disappeared after last application")
		}
		if exists && jsonEqual(current, zoneValue) {
			return s.finishZoneJob(ctx, q, job, "synced", "", time.Time{}, mustJSON(zoneValue))
		}
		if exists && previous == nil {
			return s.conflictZoneJob(ctx, q, job, "remote managed zone has no OpenVMS last-applied state")
		}
		if err := adapter.UpsertManagedCameraZone(ctx, target.RemoteName, target.Name, zoneValue, true); err != nil {
			return s.retryZoneJob(ctx, q, job, err)
		}
		return s.finishZoneJob(ctx, q, job, "synced", "", time.Time{}, mustJSON(zoneValue))
	})
}

func managedZoneKey(name string) string {
	if strings.HasPrefix(name, "ovms_") {
		return name
	}
	return "ovms_" + name
}

func desiredFrigateZone(desired, camera map[string]any) (map[string]any, error) {
	points, ok := desired["_ovms_points"].([]any)
	if !ok || len(points) < 3 {
		return nil, fmt.Errorf("persisted normalized zone points are invalid")
	}
	detect, _ := camera["detect"].(map[string]any)
	width, okW := number(detect["width"])
	height, okH := number(detect["height"])
	if !okW || !okH || width <= 0 || height <= 0 {
		return nil, fmt.Errorf("camera detect dimensions are unavailable")
	}
	coords := make([]string, 0, len(points)*2)
	for _, raw := range points {
		p, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("invalid persisted point")
		}
		x, okx := number(p["x"])
		y, oky := number(p["y"])
		if !okx || !oky {
			return nil, fmt.Errorf("invalid persisted point")
		}
		coords = append(coords, strconv.Itoa(int(math.Round(x*width))), strconv.Itoa(int(math.Round(y*height))))
	}
	out := map[string]any{}
	for k, v := range desired {
		if !strings.HasPrefix(k, "_ovms_") {
			out[k] = v
		}
	}
	out["coordinates"] = strings.Join(coords, ",")
	return out, nil
}
func number(v any) (float64, bool) { n, ok := v.(float64); return n, ok }
func jsonEqual(a, b any) bool {
	aa, _ := json.Marshal(a)
	bb, _ := json.Marshal(b)
	return string(aa) == string(bb)
}
func (s *Service) conflictZoneJob(ctx context.Context, q *db.Queries, j db.CameraZoneSyncJob, msg string) error {
	_, err := q.UpdateCameraZoneSyncState(ctx, db.UpdateCameraZoneSyncStateParams{SyncStatus: "conflict", SyncError: &msg, SyncRevision: j.DesiredRevision, LastApplied: j.LastApplied, ZoneID: j.ZoneID, TenantID: j.TenantID})
	if err != nil {
		return err
	}
	return s.finishZoneJob(ctx, q, j, "conflict", msg, time.Time{}, nil)
}
func (s *Service) retryZoneJob(ctx context.Context, q *db.Queries, j db.CameraZoneSyncJob, cause error) error {
	if j.Attempts >= cameraZoneSyncMaxAttempts {
		return s.finishZoneJob(ctx, q, j, "failed", cause.Error(), time.Time{}, nil)
	}
	delay := time.Duration(1<<min(int(j.Attempts), 10)) * time.Second
	if delay > time.Hour {
		delay = time.Hour
	}
	return s.finishZoneJob(ctx, q, j, "pending", cause.Error(), time.Now().Add(delay), nil)
}
func (s *Service) finishZoneJob(ctx context.Context, q *db.Queries, j db.CameraZoneSyncJob, status, msg string, next time.Time, last []byte) error {
	var cause *string
	if msg != "" {
		cause = &msg
	}
	if next.IsZero() {
		next = time.Now()
	}
	if last == nil {
		last = j.LastApplied
	}
	if _, err := q.UpdateCameraZoneSyncState(ctx, db.UpdateCameraZoneSyncStateParams{SyncStatus: status, SyncError: cause, SyncRevision: j.DesiredRevision, LastApplied: last, ZoneID: j.ZoneID, TenantID: j.TenantID}); err != nil {
		return err
	}
	return q.MarkCameraZoneSyncJob(ctx, db.MarkCameraZoneSyncJobParams{Status: status, LastError: cause, LastApplied: last, NextAttemptAt: next, ZoneID: j.ZoneID, DesiredRevision: j.DesiredRevision})
}

// RetryCameraZoneSync requeues a terminal job after rechecking tenant ownership and manage permission.
func (s *Service) RetryCameraZoneSync(ctx context.Context, actor authz.Actor, cameraID, zoneID uuid.UUID) error {
	return s.tx(ctx, actor, func(q *db.Queries, c *access.Checker) error {
		// Same lock as writers and workers: retry touches the job row then the zone row, the
		// opposite of their order, so unserialized it could deadlock against them.
		if err := q.LockCameraZoneSync(ctx, zoneID.String()); err != nil {
			return err
		}
		job, err := q.GetCameraZoneSyncJob(ctx, zoneID)
		if err != nil {
			return notFoundOr(err)
		}
		if job.CameraID != cameraID {
			return store.ErrNotFound
		}
		cam, err := q.GetCamera(ctx, job.CameraID)
		if err != nil {
			return notFoundOr(err)
		}
		if err := c.Require(authz.CamerasManage, cameraResource(cam)); err != nil {
			return err
		}
		rows, err := q.RetryCameraZoneSyncJob(ctx, db.RetryCameraZoneSyncJobParams{ActorUserID: &actor.UserID, ZoneID: zoneID, TenantID: cam.TenantID})
		if err != nil {
			return err
		}
		if rows == 0 {
			return store.ErrConflict
		}
		return nil
	})
}
