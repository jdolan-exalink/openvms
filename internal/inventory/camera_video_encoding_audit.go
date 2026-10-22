package inventory

import (
	"context"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/access"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

// AuditCameraVideoEncoding records that someone switched the video encoding of a camera's encoder
// configuration over ONVIF: the camera's picture and its recordings change codec, so who did it
// and from what to what has to be traceable. Camera credentials are never part of the details.
func (s *Service) AuditCameraVideoEncoding(ctx context.Context, actor authz.Actor, tenantID, cameraID uuid.UUID, configToken, from, to string) error {
	return s.tx(ctx, actor, func(q *db.Queries, _ *access.Checker) error {
		return audit(ctx, q, actor, &tenantID, ActionCameraVideoEncoding, "camera", cameraID, map[string]any{
			"config_token": configToken, "from": from, "to": to,
		})
	})
}
