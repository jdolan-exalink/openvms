package inventory

import (
	"context"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/access"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

const ActionOnvifMutation = "ONVIF_MUTATION"

const onvifMutationErrorCategoryRemoteOutcomeUnknown = "remote_outcome_unknown"

// AuditCameraOnvifMutation records a sanitized lifecycle event for a camera mutation.
func (s *Service) AuditCameraOnvifMutation(ctx context.Context, actor authz.Actor, cameraID, operationID uuid.UUID, operation, status, errorCategory string) error {
	if operation != "sync_time" && operation != "ptz" && operation != "video_config" && operation != "video_encoding" && operation != "imaging" {
		return invalid("unsupported ONVIF mutation operation")
	}
	if operationID == uuid.Nil {
		return invalid("ONVIF operation ID is required")
	}
	if status != "started" && status != "succeeded" && status != "failed" && status != "outcome_unknown" {
		return invalid("unsupported ONVIF mutation status")
	}
	if status == "outcome_unknown" && !supportedOnvifMutationErrorCategory(errorCategory) {
		return invalid("unknown ONVIF outcome requires a sanitized error category")
	}
	if status != "outcome_unknown" && errorCategory != "" {
		return invalid("error category is only valid for an unknown ONVIF outcome")
	}
	return s.tx(ctx, actor, func(q *db.Queries, checker *access.Checker) error {
		cam, err := q.GetCamera(ctx, cameraID)
		if err != nil {
			return notFoundOr(err)
		}
		if err := checker.Require(authz.CamerasManage, cameraResource(cam)); err != nil {
			return err
		}
		return audit(ctx, q, actor, &cam.TenantID, ActionOnvifMutation, "camera", cameraID, map[string]any{
			"operation_id":   operationID,
			"server_id":      cam.ServerID,
			"operation":      operation,
			"status":         status,
			"error_category": errorCategory,
		})
	})
}

func supportedOnvifMutationErrorCategory(category string) bool {
	switch category {
	case onvifMutationErrorCategoryRemoteOutcomeUnknown:
		return true
	default:
		return false
	}
}
