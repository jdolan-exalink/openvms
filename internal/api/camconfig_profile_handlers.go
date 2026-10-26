package api

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/api/gen"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/camconfig"
	"github.com/jdolan-exalink/openvms/internal/inventory"
)

// camProfileService is what the profile and bulk handlers need from the inventory service;
// tests replace it.
type camProfileService interface {
	ListConfigProfiles(ctx context.Context, a authz.Actor) ([]inventory.ProfileView, error)
	GetConfigProfile(ctx context.Context, a authz.Actor, id uuid.UUID) (inventory.ProfileView, []inventory.ConfigProfileCamera, error)
	CreateConfigProfile(ctx context.Context, a authz.Actor, in inventory.ProfileInput) (inventory.ProfileView, error)
	UpdateConfigProfile(ctx context.Context, a authz.Actor, id uuid.UUID, in inventory.ProfileInput) (inventory.ProfileView, error)
	DeleteConfigProfile(ctx context.Context, a authz.Actor, id uuid.UUID) error
	ReviewCameraProfile(ctx context.Context, a authz.Actor, id uuid.UUID, in inventory.ProfileAssignInput, link inventory.VideoLinker) (inventory.ProfileReview, error)
	ApplyCameraProfile(ctx context.Context, a authz.Actor, id uuid.UUID, in inventory.ProfileAssignInput, link inventory.VideoLinker) (inventory.ProfileApplyResult, error)
	CameraProfileContext(ctx context.Context, a authz.Actor, id uuid.UUID) (*camconfig.ProfileRef, camconfig.Patch, error)
	GroupCameraIDs(ctx context.Context, a authz.Actor, groupID uuid.UUID) ([]uuid.UUID, error)
}

var errProfilesUnavailable = errors.New("camera profiles unavailable")

func (h *Handlers) profileSvc() camProfileService {
	if h.camProfile != nil {
		return h.camProfile
	}
	if h.Inv != nil {
		return h.Inv
	}
	return nil
}

func (h *Handlers) mustProfileSvc() (camProfileService, error) {
	if ps := h.profileSvc(); ps != nil {
		return ps, nil
	}
	return nil, errProfilesUnavailable
}

type profileJSON struct {
	ID          uuid.UUID       `json:"id"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Patch       camconfig.Patch `json:"patch"`
	Version     int64           `json:"version"`
	CameraCount int             `json:"camera_count"`
	CreatedAt   any             `json:"created_at"`
	UpdatedAt   any             `json:"updated_at"`
}

func profileToGen(v inventory.ProfileView) (gen.CameraConfigProfile, error) {
	return toGen[gen.CameraConfigProfile](profileJSON{ID: v.ID, Name: v.Name, Description: v.Description,
		Patch: v.PatchDoc, Version: v.Version, CameraCount: v.CameraCount, CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt})
}

func profileInput(in gen.CameraConfigProfileInput) (inventory.ProfileInput, error) {
	patch, err := parsePatch(in.Patch)
	if err != nil {
		return inventory.ProfileInput{}, err
	}
	out := inventory.ProfileInput{Name: in.Name, Patch: patch}
	if in.Description != nil {
		out.Description = *in.Description
	}
	if in.ExpectedVersion != nil {
		out.ExpectedVersion = *in.ExpectedVersion
	}
	return out, nil
}

func (h *Handlers) ListCameraConfigProfiles(ctx context.Context, _ gen.ListCameraConfigProfilesRequestObject) (gen.ListCameraConfigProfilesResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	ps, err := h.mustProfileSvc()
	if err != nil {
		return nil, err
	}
	rows, err := ps.ListConfigProfiles(ctx, a)
	if err != nil {
		return nil, err
	}
	out := make(gen.ListCameraConfigProfiles200JSONResponse, 0, len(rows))
	for _, r := range rows {
		g, gerr := profileToGen(r)
		if gerr != nil {
			return nil, gerr
		}
		out = append(out, g)
	}
	return out, nil
}

func (h *Handlers) CreateCameraConfigProfile(ctx context.Context, r gen.CreateCameraConfigProfileRequestObject) (gen.CreateCameraConfigProfileResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	ps, err := h.mustProfileSvc()
	if err != nil {
		return nil, err
	}
	if r.Body == nil {
		return nil, &inventory.ValidationError{Msg: "profile is required"}
	}
	in, err := profileInput(*r.Body)
	if err != nil {
		return nil, err
	}
	v, err := ps.CreateConfigProfile(ctx, a, in)
	if err != nil {
		return nil, err
	}
	g, err := profileToGen(v)
	if err != nil {
		return nil, err
	}
	return gen.CreateCameraConfigProfile201JSONResponse(g), nil
}

func (h *Handlers) GetCameraConfigProfile(ctx context.Context, r gen.GetCameraConfigProfileRequestObject) (gen.GetCameraConfigProfileResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	ps, err := h.mustProfileSvc()
	if err != nil {
		return nil, err
	}
	v, cams, err := ps.GetConfigProfile(ctx, a, r.ProfileId)
	if err != nil {
		return nil, err
	}
	type camJSON struct {
		CameraID uuid.UUID `json:"camera_id"`
		Outdated bool      `json:"outdated"`
	}
	list := make([]camJSON, 0, len(cams))
	for _, c := range cams {
		list = append(list, camJSON{CameraID: c.CameraID, Outdated: c.Outdated})
	}
	out, err := toGen[gen.CameraConfigProfileDetail](struct {
		profileJSON
		Cameras []camJSON `json:"cameras"`
	}{profileJSON{ID: v.ID, Name: v.Name, Description: v.Description, Patch: v.PatchDoc, Version: v.Version,
		CameraCount: v.CameraCount, CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt}, list})
	if err != nil {
		return nil, err
	}
	return gen.GetCameraConfigProfile200JSONResponse(out), nil
}

func (h *Handlers) UpdateCameraConfigProfile(ctx context.Context, r gen.UpdateCameraConfigProfileRequestObject) (gen.UpdateCameraConfigProfileResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	ps, err := h.mustProfileSvc()
	if err != nil {
		return nil, err
	}
	if r.Body == nil {
		return nil, &inventory.ValidationError{Msg: "profile is required"}
	}
	in, err := profileInput(*r.Body)
	if err != nil {
		return nil, err
	}
	v, err := ps.UpdateConfigProfile(ctx, a, r.ProfileId, in)
	if err != nil {
		return nil, err
	}
	g, err := profileToGen(v)
	if err != nil {
		return nil, err
	}
	return gen.UpdateCameraConfigProfile200JSONResponse(g), nil
}

func (h *Handlers) DeleteCameraConfigProfile(ctx context.Context, r gen.DeleteCameraConfigProfileRequestObject) (gen.DeleteCameraConfigProfileResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	ps, err := h.mustProfileSvc()
	if err != nil {
		return nil, err
	}
	if err := ps.DeleteConfigProfile(ctx, a, r.ProfileId); err != nil {
		return nil, err
	}
	return gen.DeleteCameraConfigProfile204Response{}, nil
}

type profileReviewJSON struct {
	Profile    *camconfig.ProfileRef `json:"profile,omitempty"`
	Review     camconfig.Review      `json:"review"`
	Parameters int                   `json:"parameters"`
	Shadowed   []string              `json:"shadowed"`
	Converted  []string              `json:"converted"`
	Dropped    []string              `json:"dropped"`
}

func profileReviewOf(v inventory.ProfileReview) profileReviewJSON {
	nn := func(s []string) []string {
		if s == nil {
			return []string{}
		}
		return s
	}
	return profileReviewJSON{Profile: v.Profile, Review: v.Review, Parameters: v.Parameters,
		Shadowed: nn(v.Shadowed), Converted: nn(v.Converted), Dropped: nn(v.Dropped)}
}

func assignInput(b *gen.CameraConfigProfileAssign) (inventory.ProfileAssignInput, error) {
	if b == nil {
		return inventory.ProfileAssignInput{}, &inventory.ValidationError{Msg: "profile_id is required (null unlinks)"}
	}
	in := inventory.ProfileAssignInput{KeepValues: true}
	if b.ProfileId != nil {
		id := *b.ProfileId
		in.ProfileID = &id
	}
	if b.KeepValues != nil {
		in.KeepValues = *b.KeepValues
	}
	return in, nil
}

func (h *Handlers) ReviewCameraProfile(ctx context.Context, r gen.ReviewCameraProfileRequestObject) (gen.ReviewCameraProfileResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	ps, err := h.mustProfileSvc()
	if err != nil {
		return nil, err
	}
	in, err := assignInput(r.Body)
	if err != nil {
		return nil, err
	}
	rv, err := ps.ReviewCameraProfile(ctx, a, r.CameraId, in, h.inventoryLinker(a))
	if err != nil {
		return nil, err
	}
	out, err := toGen[gen.CameraConfigProfileReview](profileReviewOf(rv))
	if err != nil {
		return nil, err
	}
	return gen.ReviewCameraProfile200JSONResponse(out), nil
}

func (h *Handlers) ApplyCameraProfile(ctx context.Context, r gen.ApplyCameraProfileRequestObject) (gen.ApplyCameraProfileResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	ps, err := h.mustProfileSvc()
	if err != nil {
		return nil, err
	}
	in, err := assignInput(r.Body)
	if err != nil {
		return nil, err
	}
	res, err := ps.ApplyCameraProfile(ctx, a, r.CameraId, in, h.inventoryLinker(a))
	if err != nil {
		return nil, err
	}
	type applyJSON struct {
		Result string           `json:"result"`
		State  camconfig.State  `json:"state"`
		Review camconfig.Review `json:"review"`
	}
	body := struct {
		Review profileReviewJSON `json:"review"`
		Apply  *applyJSON        `json:"apply,omitempty"`
	}{Review: profileReviewOf(res.Review)}
	if res.Apply != nil {
		body.Apply = &applyJSON{Result: res.Apply.Outcome.Result, State: res.Apply.State, Review: res.Apply.Review}
	}
	out, err := toGen[gen.CameraConfigProfileApplyResult](body)
	if err != nil {
		return nil, err
	}
	return gen.ApplyCameraProfile200JSONResponse(out), nil
}
