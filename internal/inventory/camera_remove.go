package inventory

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"
	"gopkg.in/yaml.v3"

	"github.com/jdolan-exalink/openvms/internal/access"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

// RemoveCamera deletes a camera from OpenVMS and from Frigate's config.yml. A channel of a Provision
// recorder is deleted from OpenVMS only. Frigate goes first and a
// failure there leaves everything as it was; a camera already absent from the config is not an error,
// so a retry after a partial failure finishes the job. The camera's history stays (see DeleteCamera).
// It needs servers.manage on the camera's server; the configuration is read and written by this
// service, so the caller needs neither servers.config nor servers.config.secrets, and gets no config
// content back.
func (s *Service) RemoveCamera(ctx context.Context, actor authz.Actor, id uuid.UUID) error {
	var cam db.GetCameraRow
	var srv db.GetServerRow
	err := s.tx(ctx, actor, func(q *db.Queries, c *access.Checker) error {
		var err error
		if cam, err = q.GetCamera(ctx, id); err != nil {
			return notFoundOr(err)
		}
		if err = c.Require(authz.ServersManage, access.Server(cam.TenantID, cam.SiteID, cam.ServerID)); err != nil {
			return err
		}
		srv, err = q.GetServer(ctx, cam.ServerID)
		return notFoundOr(err)
	})
	if err != nil {
		return err
	}
	if srv.Kind == KindProvisionNVR {
		// A recorder's channel is only forgotten here: the recorder keeps it and keeps recording. There
		// is no configuration to edit and no recording window known, so the history purge skips it.
		return s.DeleteCamera(ctx, actor, id, nil, nil)
	}
	a, err := s.frigateAdapter(ctx, srv)
	if err != nil {
		return err
	}
	raw, err := a.RawConfig(ctx)
	if err != nil {
		return &FrigateError{Err: fmt.Errorf("read config.yml: %w", err)}
	}
	parsed, cameras, err := parseCamerasConfig(raw)
	if err != nil {
		return err
	}
	var retain *int32
	var revID *uuid.UUID
	if block, exists := cameras[cam.RemoteName]; !exists {
		// Absent from the config: either it never was there, or an earlier attempt removed it and
		// failed before deleting the camera here. Recover what that attempt saved.
		retain, revID = s.previousRemoval(ctx, actor, cam)
	} else {
		retain = recordingRetainDays(parsed, block)
		delete(cameras, cam.RemoteName)
		next, err := yaml.Marshal(parsed)
		if err != nil {
			return fmt.Errorf("marshal updated config.yml: %w", err)
		}
		res, err := s.writeRaw(ctx, actor, srv, a, string(next), true, RevisionRawSave, ActionFrigateConfigRawSaved,
			map[string]any{"camera_id": cam.ID, "remote_name": cam.RemoteName, "reason": "camera_removed"})
		if err != nil {
			return err
		}
		revID = &res.RevisionID
	}
	return s.DeleteCamera(ctx, actor, id, retain, revID)
}

// previousRemoval finds the Frigate config revision that already took the camera out of Frigate, and
// the recording window it had, when an earlier RemoveCamera saved the config and failed afterwards.
// It returns nils when there is none.
func (s *Service) previousRemoval(ctx context.Context, actor authz.Actor, cam db.GetCameraRow) (*int32, *uuid.UUID) {
	var retain *int32
	var revID *uuid.UUID
	_ = s.tx(ctx, actor, func(q *db.Queries, _ *access.Checker) error {
		id, err := q.LastCameraRemovalRevision(ctx, cam.ID.String())
		if err != nil {
			return nil
		}
		rev, err := q.GetFrigateConfigRevision(ctx, db.GetFrigateConfigRevisionParams{ID: id, ServerID: cam.ServerID})
		if err != nil {
			return nil
		}
		parsed, cameras, err := parseCamerasConfig(rev.BeforeYaml)
		if err != nil {
			return nil
		}
		block, ok := cameras[cam.RemoteName]
		if !ok {
			return nil
		}
		revID = &id
		retain = recordingRetainDays(parsed, block)
		return nil
	})
	return retain, revID
}

// ReinstateCamera restores a deleted camera in OpenVMS and puts its block back into Frigate's
// config.yml, from the revision kept when it was removed. The camera keeps its id, so the history it
// already has continues with the recordings that come next. It is refused with store.ErrConflict when
// a live camera has the same name, and with a validation error when its Frigate block was not kept.
// Needs servers.manage only, like RemoveCamera.
func (s *Service) ReinstateCamera(ctx context.Context, actor authz.Actor, id uuid.UUID) (db.GetCameraRow, error) {
	var cam db.Camera
	var srv db.GetServerRow
	var tomb db.CameraTombstone
	var haveTomb bool
	var rev db.FrigateConfigRevision
	err := s.tx(ctx, actor, func(q *db.Queries, c *access.Checker) error {
		var err error
		if cam, err = q.GetDeletedCamera(ctx, id); err != nil {
			return notFoundOr(err)
		}
		if err = c.Require(authz.ServersManage, access.Server(cam.TenantID, cam.SiteID, cam.ServerID)); err != nil {
			return err
		}
		n, err := q.CountLiveCamerasNamed(ctx, db.CountLiveCamerasNamedParams{ServerID: cam.ServerID, RemoteName: cam.RemoteName})
		if err != nil {
			return err
		}
		if n > 0 {
			return fmt.Errorf("%w: a live camera is already named %q on this server", store.ErrConflict, cam.RemoteName)
		}
		if srv, err = q.GetServer(ctx, cam.ServerID); err != nil {
			return notFoundOr(err)
		}
		if tomb, err = q.GetCameraTombstone(ctx, id); err == nil {
			haveTomb = true
		} else if !errors.Is(notFoundOr(err), store.ErrNotFound) {
			return err
		}
		if haveTomb && tomb.HadFrigateConfig && tomb.ConfigRevisionID == nil {
			return invalid("the Frigate configuration of camera %q is no longer stored, so it cannot be restored", cam.RemoteName)
		}
		if haveTomb && tomb.ConfigRevisionID != nil {
			rev, err = q.GetFrigateConfigRevision(ctx, db.GetFrigateConfigRevisionParams{ID: *tomb.ConfigRevisionID, ServerID: cam.ServerID})
			if err != nil {
				return invalid("the Frigate configuration of camera %q was not kept, so it cannot be restored", cam.RemoteName)
			}
		}
		return nil
	})
	if err != nil {
		return db.GetCameraRow{}, err
	}
	if haveTomb && tomb.ConfigRevisionID != nil {
		if err := s.restoreFrigateBlock(ctx, actor, srv, cam, rev.BeforeYaml); err != nil {
			return db.GetCameraRow{}, err
		}
	}
	return s.RestoreCamera(ctx, actor, id)
}

// restoreFrigateBlock adds the camera's block from the config it was removed from back into the
// server's current config.yml. A camera already present is left alone.
func (s *Service) restoreFrigateBlock(ctx context.Context, actor authz.Actor, srv db.GetServerRow, cam db.Camera, beforeYAML string) error {
	_, old, err := parseCamerasConfig(beforeYAML)
	if err != nil {
		return err
	}
	block, ok := old[cam.RemoteName]
	if !ok {
		return invalid("the Frigate configuration of camera %q was not kept, so it cannot be restored", cam.RemoteName)
	}
	a, err := s.frigateAdapter(ctx, srv)
	if err != nil {
		return err
	}
	raw, err := a.RawConfig(ctx)
	if err != nil {
		return &FrigateError{Err: fmt.Errorf("read config.yml: %w", err)}
	}
	parsed, cameras, err := parseCamerasConfig(raw)
	if err != nil {
		return err
	}
	if _, exists := cameras[cam.RemoteName]; exists {
		return nil
	}
	cameras[cam.RemoteName] = block
	next, err := yaml.Marshal(parsed)
	if err != nil {
		return fmt.Errorf("marshal updated config.yml: %w", err)
	}
	_, err = s.writeRaw(ctx, actor, srv, a, string(next), true, RevisionRawSave, ActionFrigateConfigRawSaved,
		map[string]any{"camera_id": cam.ID, "remote_name": cam.RemoteName, "reason": "camera_restored"})
	return err
}

// DeletedCamera is a camera removed from OpenVMS that can still be restored.
type DeletedCamera struct {
	ID          uuid.UUID
	ServerID    uuid.UUID
	RemoteName  string
	DisplayName string
	DeletedAt   time.Time
	// RetainDays is the recording window the camera had when it was deleted; nil when unknown.
	RetainDays *int32
	// HasFrigateConfig is true when its Frigate block was kept, so it can be put back.
	HasFrigateConfig bool
}

// ListDeletedCameras lists a server's deleted cameras. Needs servers.manage on the server.
func (s *Service) ListDeletedCameras(ctx context.Context, actor authz.Actor, serverID uuid.UUID) ([]DeletedCamera, error) {
	var out []DeletedCamera
	err := s.tx(ctx, actor, func(q *db.Queries, c *access.Checker) error {
		srv, err := q.GetServer(ctx, serverID)
		if err != nil {
			return notFoundOr(err)
		}
		if err := c.Require(authz.ServersManage, access.Server(srv.TenantID, srv.SiteID, srv.ID)); err != nil {
			return err
		}
		rows, err := q.ListDeletedCameras(ctx, serverID)
		if err != nil {
			return err
		}
		out = make([]DeletedCamera, 0, len(rows))
		for _, r := range rows {
			d := DeletedCamera{ID: r.ID, ServerID: r.ServerID, RemoteName: r.RemoteName, DisplayName: r.DisplayName,
				RetainDays: r.RetainDays, HasFrigateConfig: r.HasFrigateConfig}
			if r.DeletedAt != nil {
				d.DeletedAt = *r.DeletedAt
			}
			out = append(out, d)
		}
		return nil
	})
	return out, err
}

// parseCamerasConfig parses a config.yml and returns it with its (created if absent) cameras map.
func parseCamerasConfig(raw string) (map[string]any, map[string]any, error) {
	var parsed map[string]any
	if err := yaml.Unmarshal([]byte(raw), &parsed); err != nil {
		return nil, nil, fmt.Errorf("parse config.yml: %w", err)
	}
	if parsed == nil {
		parsed = map[string]any{}
	}
	cameras, ok := parsed["cameras"].(map[string]any)
	if !ok {
		cameras = map[string]any{}
		parsed["cameras"] = cameras
	}
	return parsed, cameras, nil
}

// recordingRetainDays is the longest recording window found in the camera's own record section, or
// in the global one when the camera has none: the retention of continuous, motion, alert and detection
// recordings across the Frigate versions that name them differently. nil when the config states none,
// which later keeps the history purge away from the camera.
func recordingRetainDays(parsed map[string]any, camera any) *int32 {
	if cam, ok := camera.(map[string]any); ok {
		if days, ok := recordSectionDays(cam["record"]); ok {
			return &days
		}
	}
	if days, ok := recordSectionDays(parsed["record"]); ok {
		return &days
	}
	return nil
}

func recordSectionDays(section any) (int32, bool) {
	rec, ok := section.(map[string]any)
	if !ok {
		return 0, false
	}
	var best int32
	found := false
	for _, path := range [][]string{{"retain", "days"}, {"continuous", "days"}, {"motion", "days"}, {"alerts", "retain", "days"}, {"detections", "retain", "days"}} {
		var cur any = rec
		for _, key := range path {
			m, ok := cur.(map[string]any)
			if !ok {
				cur = nil
				break
			}
			cur = m[key]
		}
		if days, ok := wholeDays(cur); ok {
			found = true
			best = max(best, days)
		}
	}
	return best, found
}

// wholeDays reads a YAML number as a non-negative number of days; fractions round up.
func wholeDays(v any) (int32, bool) {
	var f float64
	switch n := v.(type) {
	case int:
		f = float64(n)
	case int64:
		f = float64(n)
	case float64:
		f = n
	default:
		return 0, false
	}
	if f < 0 || f > math.MaxInt32 {
		return 0, false
	}
	return int32(math.Ceil(f)), true
}
