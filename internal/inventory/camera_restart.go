package inventory

import (
	"context"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/access"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

// ActionCameraRestart is the audit action of a camera device restart.
const ActionCameraRestart = "CAMERA_RESTART"

// RequireCameraRestart requires cameras.restart on the camera; legacy servers.restart holders keep
// access until they receive granular grants (authz.CompatFor) and nobody else gains it.
func (s *Service) RequireCameraRestart(ctx context.Context, actor authz.Actor, cameraID uuid.UUID) error {
	return s.tx(ctx, actor, func(q *db.Queries, c *access.Checker) error {
		cam, err := q.GetCamera(ctx, cameraID)
		if err != nil {
			return notFoundOr(err)
		}
		return c.RequireCompat(authz.CamerasRestart, cameraResource(cam))
	})
}

// AuditCameraRestart records a restart attempt: who, which camera and the result (started,
// succeeded, failed or outcome_unknown). The permission is checked again so the record cannot be
// forged by a caller without it.
func (s *Service) AuditCameraRestart(ctx context.Context, actor authz.Actor, cameraID, operationID uuid.UUID, status, via string) error {
	if operationID == uuid.Nil {
		return invalid("restart operation ID is required")
	}
	switch status {
	case "started", "succeeded", "failed", "outcome_unknown":
	default:
		return invalid("unsupported restart status")
	}
	return s.tx(ctx, actor, func(q *db.Queries, c *access.Checker) error {
		cam, err := q.GetCamera(ctx, cameraID)
		if err != nil {
			return notFoundOr(err)
		}
		if err := c.RequireCompat(authz.CamerasRestart, cameraResource(cam)); err != nil {
			return err
		}
		return audit(ctx, q, actor, &cam.TenantID, ActionCameraRestart, "camera", cameraID, map[string]any{
			"operation_id": operationID,
			"server_id":    cam.ServerID,
			"status":       status,
			"via":          via,
		})
	})
}
