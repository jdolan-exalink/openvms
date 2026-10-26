package inventory

import (
	"context"
	"encoding/json"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/access"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

const (
	maxPTZTourSteps    = 64
	maxPTZTourDwellSec = 3600
	maxPTZTourRepeat   = 1000
)

// PTZTourStep is one stop of a tour: go to a device preset, wait, at a speed.
type PTZTourStep struct {
	PresetToken  string  `json:"preset_token"`
	PresetName   string  `json:"preset_name,omitempty"`
	DwellSeconds int     `json:"dwell_seconds"`
	Speed        float64 `json:"speed"`
}

// PTZTour is a persisted per-camera sequence of presets. RepeatCount 0 means repeat until stopped.
type PTZTour struct {
	ID          uuid.UUID     `json:"id"`
	CameraID    uuid.UUID     `json:"camera_id"`
	Name        string        `json:"name"`
	Steps       []PTZTourStep `json:"steps"`
	RepeatCount int           `json:"repeat_count"`
	CreatedAt   time.Time     `json:"created_at"`
	UpdatedAt   time.Time     `json:"updated_at"`
}

// PTZTourInput is the editable tour payload; it carries no tenant identifier.
type PTZTourInput struct {
	Name        string        `json:"name"`
	Steps       []PTZTourStep `json:"steps"`
	RepeatCount int           `json:"repeat_count"`
}

func normalizePTZTour(in PTZTourInput) (PTZTourInput, error) {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || len(in.Name) > 128 {
		return in, invalid("tour name must be 1 to 128 characters")
	}
	if len(in.Steps) == 0 || len(in.Steps) > maxPTZTourSteps {
		return in, invalid("tour requires between 1 and %d steps", maxPTZTourSteps)
	}
	if in.RepeatCount < 0 || in.RepeatCount > maxPTZTourRepeat {
		return in, invalid("tour repeat_count must be between 0 and %d", maxPTZTourRepeat)
	}
	steps := make([]PTZTourStep, len(in.Steps))
	for i, st := range in.Steps {
		st.PresetToken = strings.TrimSpace(st.PresetToken)
		st.PresetName = strings.TrimSpace(st.PresetName)
		if st.PresetToken == "" || len(st.PresetToken) > 128 || len(st.PresetName) > 128 {
			return in, invalid("tour step %d needs a preset_token of at most 128 characters", i+1)
		}
		if st.DwellSeconds < 0 || st.DwellSeconds > maxPTZTourDwellSec {
			return in, invalid("tour step %d dwell_seconds must be between 0 and %d", i+1, maxPTZTourDwellSec)
		}
		if math.IsNaN(st.Speed) || st.Speed < 0 || st.Speed > 1 {
			return in, invalid("tour step %d speed must be between 0 and 1", i+1)
		}
		steps[i] = st
	}
	in.Steps = steps
	return in, nil
}

func ptzTourFromRow(id, cameraID uuid.UUID, name string, steps []byte, repeat int32, created, updated time.Time) (PTZTour, error) {
	out := PTZTour{ID: id, CameraID: cameraID, Name: name, RepeatCount: int(repeat), CreatedAt: created, UpdatedAt: updated}
	if err := json.Unmarshal(steps, &out.Steps); err != nil {
		return PTZTour{}, err
	}
	return out, nil
}

// ListCameraPTZTours returns the tours of a camera; it requires cameras.view on the camera.
func (s *Service) ListCameraPTZTours(ctx context.Context, actor authz.Actor, cameraID uuid.UUID) ([]PTZTour, error) {
	var tours []PTZTour
	err := s.tx(ctx, actor, func(q *db.Queries, checker *access.Checker) error {
		cam, err := q.GetCamera(ctx, cameraID)
		if err != nil {
			return notFoundOr(err)
		}
		if err := checker.Require(authz.CamerasView, cameraResource(cam)); err != nil {
			return err
		}
		rows, err := q.ListCameraPTZTours(ctx, db.ListCameraPTZToursParams{TenantID: cam.TenantID, CameraID: cam.ID})
		if err != nil {
			return err
		}
		tours = make([]PTZTour, 0, len(rows))
		for _, r := range rows {
			t, err := ptzTourFromRow(r.ID, r.CameraID, r.Name, r.Steps, r.RepeatCount, r.CreatedAt, r.UpdatedAt)
			if err != nil {
				return err
			}
			tours = append(tours, t)
		}
		return nil
	})
	return tours, err
}

// CreateCameraPTZTour stores a tour; it requires cameras.manage on the camera.
func (s *Service) CreateCameraPTZTour(ctx context.Context, actor authz.Actor, cameraID uuid.UUID, input PTZTourInput) (PTZTour, error) {
	in, err := normalizePTZTour(input)
	if err != nil {
		return PTZTour{}, err
	}
	var out PTZTour
	err = s.tx(ctx, actor, func(q *db.Queries, checker *access.Checker) error {
		cam, err := q.GetCamera(ctx, cameraID)
		if err != nil {
			return notFoundOr(err)
		}
		if err := checker.Require(authz.CamerasManage, cameraResource(cam)); err != nil {
			return err
		}
		row, err := q.CreateCameraPTZTour(ctx, db.CreateCameraPTZTourParams{TenantID: cam.TenantID, CameraID: cam.ID, Name: in.Name, Steps: mustJSON(in.Steps), RepeatCount: int32(in.RepeatCount)})
		if err != nil {
			return store.Classify(err)
		}
		out, err = ptzTourFromRow(row.ID, row.CameraID, row.Name, row.Steps, row.RepeatCount, row.CreatedAt, row.UpdatedAt)
		return err
	})
	return out, err
}

// UpdateCameraPTZTour replaces a tour; it requires cameras.manage on the camera.
func (s *Service) UpdateCameraPTZTour(ctx context.Context, actor authz.Actor, cameraID, tourID uuid.UUID, input PTZTourInput) (PTZTour, error) {
	in, err := normalizePTZTour(input)
	if err != nil {
		return PTZTour{}, err
	}
	var out PTZTour
	err = s.tx(ctx, actor, func(q *db.Queries, checker *access.Checker) error {
		cam, err := q.GetCamera(ctx, cameraID)
		if err != nil {
			return notFoundOr(err)
		}
		if err := checker.Require(authz.CamerasManage, cameraResource(cam)); err != nil {
			return err
		}
		row, err := q.UpdateCameraPTZTour(ctx, db.UpdateCameraPTZTourParams{TenantID: cam.TenantID, CameraID: cam.ID, ID: tourID, Name: in.Name, Steps: mustJSON(in.Steps), RepeatCount: int32(in.RepeatCount)})
		if err != nil {
			return notFoundOr(store.Classify(err))
		}
		out, err = ptzTourFromRow(row.ID, row.CameraID, row.Name, row.Steps, row.RepeatCount, row.CreatedAt, row.UpdatedAt)
		return err
	})
	return out, err
}

// DeleteCameraPTZTour removes a tour; it requires cameras.manage on the camera.
func (s *Service) DeleteCameraPTZTour(ctx context.Context, actor authz.Actor, cameraID, tourID uuid.UUID) error {
	return s.tx(ctx, actor, func(q *db.Queries, checker *access.Checker) error {
		cam, err := q.GetCamera(ctx, cameraID)
		if err != nil {
			return notFoundOr(err)
		}
		if err := checker.Require(authz.CamerasManage, cameraResource(cam)); err != nil {
			return err
		}
		n, err := q.DeleteCameraPTZTour(ctx, db.DeleteCameraPTZTourParams{TenantID: cam.TenantID, CameraID: cam.ID, ID: tourID})
		if err != nil {
			return err
		}
		if n == 0 {
			return store.ErrNotFound
		}
		return nil
	})
}
