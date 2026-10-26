package inventory

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/access"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/frigate"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

// CameraEngine is a camera the actor may view together with its Frigate server. Adapter is nil
// when the server could not be reached; the camera is still usable.
type CameraEngine struct {
	Camera     db.GetCameraRow
	ServerName string
	Adapter    frigate.Adapter
	// State is the stored desired state; nil when the camera was never configured through the
	// normalized model.
	State *db.CameraConfigState
}

// OpenCameraEngine loads a camera (cameras.view required) and connects to its Frigate server.
// A connection failure is not an error: the caller degrades to inventory data.
func (s *Service) OpenCameraEngine(ctx context.Context, actor authz.Actor, cameraID uuid.UUID) (CameraEngine, error) {
	var out CameraEngine
	var srv db.GetServerRow
	err := s.tx(ctx, actor, func(q *db.Queries, c *access.Checker) error {
		cam, err := q.GetCamera(ctx, cameraID)
		if err != nil {
			return notFoundOr(err)
		}
		if err := c.Require(authz.CamerasView, cameraResource(cam)); err != nil {
			return err
		}
		if err := c.RequireCompat(authz.CamerasConfigView, cameraResource(cam)); err != nil {
			return err
		}
		if srv, err = q.GetServer(ctx, cam.ServerID); err != nil {
			return notFoundOr(err)
		}
		out.Camera, out.ServerName = cam, srv.Name
		st, err := q.GetCameraConfigState(ctx, cameraID)
		switch {
		case err == nil:
			out.State = &st
		case !errors.Is(store.Classify(err), store.ErrNotFound):
			return err
		}
		return nil
	})
	if err != nil {
		return CameraEngine{}, err
	}
	if a, err := s.frigateAdapter(ctx, srv); err == nil {
		out.Adapter = a
	}
	return out, nil
}
