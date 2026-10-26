package inventory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/access"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/camconfig"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

// ActionCameraConfigProfileChanged records the life of a configuration profile and its
// assignment to cameras; details.change is created, updated, deleted, assigned or unassigned.
const ActionCameraConfigProfileChanged = "CAMERA_CONFIG_PROFILE_CHANGED"

// ProfileInUseError is returned when a profile that cameras still follow is deleted.
type ProfileInUseError struct{ Cameras int }

func (e *ProfileInUseError) Error() string {
	return fmt.Sprintf("the profile is used by %d camera(s); unlink them first", e.Cameras)
}

// ProfileView is a stored profile with its decoded patch and how many cameras follow it.
type ProfileView struct {
	db.CameraConfigProfile
	PatchDoc    camconfig.Patch
	CameraCount int
}

// ProfileInput creates or replaces a profile.
type ProfileInput struct {
	Name        string
	Description string
	Patch       camconfig.Patch
	// ExpectedVersion makes an update fail with ErrRevisionConflict when stale; 0 skips the check.
	ExpectedVersion int64
}

func decodeProfilePatch(raw []byte) camconfig.Patch {
	var p camconfig.Patch
	if err := json.Unmarshal(raw, &p); err != nil {
		return camconfig.Patch{}
	}
	return p
}

func profileView(p db.CameraConfigProfile, n int) ProfileView {
	return ProfileView{CameraConfigProfile: p, PatchDoc: decodeProfilePatch(p.Patch), CameraCount: n}
}

// profilePatchOf loads the patch of the profile a camera follows (empty when none).
func profilePatchOf(ctx context.Context, q *db.Queries, id *uuid.UUID) (camconfig.Patch, error) {
	if id == nil {
		return camconfig.Patch{}, nil
	}
	p, err := q.GetCameraConfigProfile(ctx, *id)
	if err != nil {
		return camconfig.Patch{}, notFoundOr(err)
	}
	return decodeProfilePatch(p.Patch), nil
}

func profileTenant(actor authz.Actor) (uuid.UUID, error) {
	if actor.TenantID == nil {
		return uuid.Nil, invalid("a tenant context is required to manage camera profiles")
	}
	return *actor.TenantID, nil
}

func prepareProfile(in ProfileInput) (string, []byte, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return "", nil, invalid("name is required")
	}
	if len([]rune(name)) > 120 {
		return "", nil, invalid("name is too long")
	}
	if err := camconfig.ValidateProfilePatch(in.Patch); err != nil {
		return "", nil, invalid("%s", err.Error())
	}
	raw, err := json.Marshal(in.Patch.Normalized())
	return name, raw, err
}

// ListConfigProfiles lists the tenant's profiles (cameras.view on the tenant).
func (s *Service) ListConfigProfiles(ctx context.Context, actor authz.Actor) ([]ProfileView, error) {
	tid, err := profileTenant(actor)
	if err != nil {
		return nil, err
	}
	var out []ProfileView
	err = s.tx(ctx, actor, func(q *db.Queries, c *access.Checker) error {
		if err := c.Require(authz.CamerasView, access.Tenant(tid)); err != nil {
			return err
		}
		rows, err := q.ListCameraConfigProfiles(ctx, tid)
		if err != nil {
			return err
		}
		for _, r := range rows {
			out = append(out, profileView(db.CameraConfigProfile{ID: r.ID, TenantID: r.TenantID, Name: r.Name,
				Description: r.Description, Patch: r.Patch, Version: r.Version, CreatedBy: r.CreatedBy,
				CreatedAt: r.CreatedAt, UpdatedBy: r.UpdatedBy, UpdatedAt: r.UpdatedAt}, int(r.CameraCount)))
		}
		return nil
	})
	return out, err
}

// ConfigProfileCameras is one camera following a profile and whether it has the latest version.
type ConfigProfileCamera struct {
	CameraID uuid.UUID
	Outdated bool
}

// GetConfigProfile returns a profile and the cameras that follow it, limited to those the
// actor can view.
func (s *Service) GetConfigProfile(ctx context.Context, actor authz.Actor, id uuid.UUID) (ProfileView, []ConfigProfileCamera, error) {
	var out ProfileView
	var cams []ConfigProfileCamera
	err := s.tx(ctx, actor, func(q *db.Queries, c *access.Checker) error {
		p, err := q.GetCameraConfigProfile(ctx, id)
		if err != nil {
			return notFoundOr(err)
		}
		if err := c.Require(authz.CamerasView, access.Tenant(p.TenantID)); err != nil {
			return err
		}
		rows, err := q.ListCameraConfigProfileCameras(ctx, &id)
		if err != nil {
			return err
		}
		visible, err := s.visibleCameraSet(ctx, c)
		if err != nil {
			return err
		}
		for _, r := range rows {
			if visible[r.CameraID] {
				cams = append(cams, ConfigProfileCamera{CameraID: r.CameraID, Outdated: r.ProfileVersion == nil || *r.ProfileVersion < p.Version})
			}
		}
		out = profileView(p, len(rows))
		return nil
	})
	return out, cams, err
}

// CreateConfigProfile stores a new profile (cameras.manage on the tenant).
func (s *Service) CreateConfigProfile(ctx context.Context, actor authz.Actor, in ProfileInput) (ProfileView, error) {
	name, raw, err := prepareProfile(in)
	if err != nil {
		return ProfileView{}, err
	}
	tid, err := profileTenant(actor)
	if err != nil {
		return ProfileView{}, err
	}
	var out ProfileView
	err = s.tx(ctx, actor, func(q *db.Queries, c *access.Checker) error {
		if err := c.Require(authz.CamerasManage, access.Tenant(tid)); err != nil {
			return err
		}
		p, err := q.CreateCameraConfigProfile(ctx, db.CreateCameraConfigProfileParams{
			TenantID: tid, Name: name, Description: in.Description, Patch: raw, Actor: &actor.UserID})
		if err != nil {
			return store.Classify(err)
		}
		out = profileView(p, 0)
		return audit(ctx, q, actor, &tid, ActionCameraConfigProfileChanged, "camera_config_profile", p.ID,
			map[string]any{"change": "created", "name": p.Name, "version": p.Version, "patch": in.Patch.Normalized()})
	})
	return out, err
}

// UpdateConfigProfile replaces a profile. Cameras are not touched: re-applying is explicit.
func (s *Service) UpdateConfigProfile(ctx context.Context, actor authz.Actor, id uuid.UUID, in ProfileInput) (ProfileView, error) {
	name, raw, err := prepareProfile(in)
	if err != nil {
		return ProfileView{}, err
	}
	var out ProfileView
	err = s.tx(ctx, actor, func(q *db.Queries, c *access.Checker) error {
		cur, err := q.GetCameraConfigProfile(ctx, id)
		if err != nil {
			return notFoundOr(err)
		}
		if err := c.Require(authz.CamerasManage, access.Tenant(cur.TenantID)); err != nil {
			return err
		}
		expected := in.ExpectedVersion
		if expected == 0 {
			expected = cur.Version
		}
		p, err := q.UpdateCameraConfigProfile(ctx, db.UpdateCameraConfigProfileParams{
			ID: id, Name: name, Description: in.Description, Patch: raw, Actor: &actor.UserID, ExpectedVersion: expected})
		if errors.Is(store.Classify(err), store.ErrNotFound) {
			return ErrRevisionConflict
		}
		if err != nil {
			return store.Classify(err)
		}
		n, err := q.CountCameraConfigProfileCameras(ctx, &id)
		if err != nil {
			return err
		}
		out = profileView(p, int(n))
		return audit(ctx, q, actor, &p.TenantID, ActionCameraConfigProfileChanged, "camera_config_profile", p.ID,
			map[string]any{"change": "updated", "name": p.Name, "version": p.Version, "previous_version": cur.Version,
				"previous_patch": decodeProfilePatch(cur.Patch), "patch": in.Patch.Normalized()})
	})
	return out, err
}

// DeleteConfigProfile removes a profile that no camera follows.
func (s *Service) DeleteConfigProfile(ctx context.Context, actor authz.Actor, id uuid.UUID) error {
	return s.tx(ctx, actor, func(q *db.Queries, c *access.Checker) error {
		cur, err := q.GetCameraConfigProfile(ctx, id)
		if err != nil {
			return notFoundOr(err)
		}
		if err := c.Require(authz.CamerasManage, access.Tenant(cur.TenantID)); err != nil {
			return err
		}
		n, err := q.CountCameraConfigProfileCameras(ctx, &id)
		if err != nil {
			return err
		}
		if n > 0 {
			return &ProfileInUseError{Cameras: int(n)}
		}
		if _, err := q.DeleteCameraConfigProfile(ctx, id); err != nil {
			return store.Classify(err)
		}
		return audit(ctx, q, actor, &cur.TenantID, ActionCameraConfigProfileChanged, "camera_config_profile", id,
			map[string]any{"change": "deleted", "name": cur.Name, "version": cur.Version})
	})
}

// CameraProfileContext returns the profile a camera follows and its patch (nil when none). It
// needs only what viewing the camera needs.
func (s *Service) CameraProfileContext(ctx context.Context, actor authz.Actor, cameraID uuid.UUID) (*camconfig.ProfileRef, camconfig.Patch, error) {
	var ref *camconfig.ProfileRef
	var patch camconfig.Patch
	err := s.tx(ctx, actor, func(q *db.Queries, c *access.Checker) error {
		cam, err := q.GetCamera(ctx, cameraID)
		if err != nil {
			return notFoundOr(err)
		}
		if err := c.Require(authz.CamerasView, cameraResource(cam)); err != nil {
			return err
		}
		st, err := q.GetCameraConfigState(ctx, cameraID)
		if err != nil {
			if errors.Is(store.Classify(err), store.ErrNotFound) {
				return nil
			}
			return err
		}
		if st.ProfileID == nil {
			return nil
		}
		p, err := q.GetCameraConfigProfile(ctx, *st.ProfileID)
		if err != nil {
			return notFoundOr(err)
		}
		ver := int64(0)
		if st.ProfileVersion != nil {
			ver = *st.ProfileVersion
		}
		ref = &camconfig.ProfileRef{ID: p.ID.String(), Name: p.Name, Version: p.Version, Outdated: ver < p.Version}
		patch = decodeProfilePatch(p.Patch)
		return nil
	})
	return ref, patch, err
}

// ProfileReview is what assigning (or unlinking) a profile would do to one camera.
type ProfileReview struct {
	Profile *camconfig.ProfileRef
	// Review lists the changes that would reach the engine: the profile's fields not shadowed
	// by the camera's overrides, compared with the engine's current values.
	Review camconfig.Review
	// Parameters is the number of changes ("Se modificarán N parámetros").
	Parameters int
	// Shadowed are profile fields the camera's own overrides keep winning.
	Shadowed []string
	// Converted are the paths that become overrides when unlinking with keep_values.
	Converted []string
	// Dropped are the profile-defined paths that stop being desired when unlinking without it.
	Dropped []string
}

// ProfileAssignInput assigns a profile to a camera (ProfileID) or unlinks it (nil).
type ProfileAssignInput struct {
	ProfileID *uuid.UUID
	// KeepValues, only for unlinking, turns the profile's current values into overrides.
	KeepValues bool
}

// ReviewCameraProfile previews an assignment or unlink. It has no side effects.
func (s *Service) ReviewCameraProfile(ctx context.Context, actor authz.Actor, cameraID uuid.UUID, in ProfileAssignInput, link VideoLinker) (ProfileReview, error) {
	rv, _, _, err := s.reviewProfile(ctx, actor, cameraID, in, link)
	return rv, err
}

func (s *Service) reviewProfile(ctx context.Context, actor authz.Actor, cameraID uuid.UUID, in ProfileAssignInput, link VideoLinker) (ProfileReview, camconfig.Patch, db.CameraConfigState, error) {
	var t configTarget
	var st db.CameraConfigState
	var target, current *db.CameraConfigProfile
	err := s.tx(ctx, actor, func(q *db.Queries, c *access.Checker) error {
		var err error
		if t, err = s.loadConfigTarget(ctx, cameraID, q, c); err != nil {
			return err
		}
		if err = c.Require(authz.CamerasManage, cameraResource(t.cam)); err != nil {
			return err
		}
		if err = q.EnsureCameraConfigState(ctx, db.EnsureCameraConfigStateParams{CameraID: cameraID, TenantID: t.cam.TenantID}); err != nil {
			return err
		}
		if st, err = q.GetCameraConfigState(ctx, cameraID); err != nil {
			return err
		}
		if in.ProfileID != nil {
			p, err := q.GetCameraConfigProfile(ctx, *in.ProfileID)
			if err != nil {
				return notFoundOr(err)
			}
			if p.TenantID != t.cam.TenantID {
				return store.ErrNotFound
			}
			target = &p
		}
		if st.ProfileID != nil {
			p, err := q.GetCameraConfigProfile(ctx, *st.ProfileID)
			if err != nil {
				return notFoundOr(err)
			}
			current = &p
		}
		return nil
	})
	if err != nil {
		return ProfileReview{}, camconfig.Patch{}, st, err
	}
	overrides := StoredConfigState(&st).Desired
	out := ProfileReview{Review: camconfig.Review{Changes: []camconfig.Change{}, Impact: []camconfig.ImpactItem{}, Unsupported: []string{}}}
	if target == nil {
		if current != nil {
			cur := decodeProfilePatch(current.Patch)
			paths := sortedKeys(camconfig.Leaves(camconfig.Subtract(cur, overrides)))
			if in.KeepValues {
				out.Converted = paths
			} else {
				out.Dropped = paths
			}
		}
		return out, camconfig.Patch{}, st, nil
	}
	pp := decodeProfilePatch(target.Patch)
	out.Profile = &camconfig.ProfileRef{ID: target.ID.String(), Name: target.Name, Version: target.Version}
	inherited := camconfig.Subtract(pp, overrides)
	for path := range camconfig.Leaves(pp) {
		if _, ok := camconfig.Leaves(overrides)[path]; ok {
			out.Shadowed = append(out.Shadowed, path)
		}
	}
	sortStrings(out.Shadowed)
	if inherited.Empty() {
		return out, inherited, st, nil
	}
	ad := s.configAdapter(ctx, actor, t)
	if err := linkVideo(ctx, &ad, t, inherited, link); err != nil {
		return ProfileReview{}, inherited, st, err
	}
	cur, err := ad.Read(ctx, CameraConfigInventory(t.cam, t.srv.Name))
	if err != nil {
		return ProfileReview{}, inherited, st, err
	}
	if out.Review, err = ad.Plan(ctx, cur, inherited); err != nil {
		return ProfileReview{}, inherited, st, err
	}
	out.Parameters = len(out.Review.Changes)
	return out, inherited, st, nil
}

// ProfileApplyResult is the outcome of an assignment or unlink.
type ProfileApplyResult struct {
	Review ProfileReview
	// Apply is set when the engine had to change (origin profile, normal pipeline).
	Apply *ConfigApplyResult
}

// ApplyCameraProfile links (or unlinks) the profile. Assigning applies the profile's fields
// that differ and are not overridden through the normal desired/effective/audit pipeline with
// origin profile; the camera's overrides are never rewritten. Unlinking changes no engine
// value: it only decides whether the profile's values stay as overrides.
func (s *Service) ApplyCameraProfile(ctx context.Context, actor authz.Actor, cameraID uuid.UUID, in ProfileAssignInput, link VideoLinker) (ProfileApplyResult, error) {
	rv, inherited, st, err := s.reviewProfile(ctx, actor, cameraID, in, link)
	if err != nil {
		return ProfileApplyResult{}, err
	}
	res := ProfileApplyResult{Review: rv}
	if in.ProfileID == nil {
		err = s.unlinkProfile(ctx, actor, cameraID, st, in.KeepValues)
		return res, err
	}
	pid, ver := *in.ProfileID, rv.Profile.Version
	var rec db.CameraConfigState
	err = s.tx(ctx, actor, func(q *db.Queries, _ *access.Checker) error {
		var err error
		if rec, err = q.LinkCameraConfigProfile(ctx, db.LinkCameraConfigProfileParams{
			CameraID: cameraID, ProfileID: &pid, ProfileVersion: &ver, UpdatedBy: &actor.UserID}); err != nil {
			return err
		}
		return audit(ctx, q, actor, &rec.TenantID, ActionCameraConfigProfileChanged, "camera", cameraID, map[string]any{
			"change": "assigned", "camera_id": cameraID, "profile_id": pid, "profile_name": rv.Profile.Name,
			"profile_version": ver, "previous_profile_id": st.ProfileID, "parameters": rv.Parameters,
		})
	})
	if err != nil {
		return res, err
	}
	if inherited.Empty() || rv.Parameters == 0 {
		return res, nil
	}
	ap, err := s.ApplyCameraConfigWith(ctx, actor, cameraID, ConfigApplyInput{
		Patch: inherited, ExpectedRevision: rec.DesiredRevision, Origin: camconfig.OriginProfile}, link)
	if err != nil {
		return res, err
	}
	res.Apply = &ap
	return res, nil
}

func (s *Service) unlinkProfile(ctx context.Context, actor authz.Actor, cameraID uuid.UUID, st db.CameraConfigState, keep bool) error {
	if st.ProfileID == nil {
		return nil
	}
	return s.tx(ctx, actor, func(q *db.Queries, _ *access.Checker) error {
		cur, err := q.GetCameraConfigState(ctx, cameraID)
		if err != nil {
			return err
		}
		p, err := q.GetCameraConfigProfile(ctx, *st.ProfileID)
		if err != nil {
			return notFoundOr(err)
		}
		desired := StoredConfigState(&cur).Desired
		if keep {
			desired = camconfig.Merge(decodeProfilePatch(p.Patch), desired)
		}
		raw, err := json.Marshal(desired)
		if err != nil {
			return err
		}
		if _, err := q.SetCameraConfigProfile(ctx, db.SetCameraConfigProfileParams{
			CameraID: cameraID, Desired: raw, UpdatedBy: &actor.UserID, ExpectedRevision: cur.DesiredRevision}); err != nil {
			if errors.Is(store.Classify(err), store.ErrNotFound) {
				return ErrRevisionConflict
			}
			return err
		}
		return audit(ctx, q, actor, &cur.TenantID, ActionCameraConfigProfileChanged, "camera", cameraID, map[string]any{
			"change": "unassigned", "camera_id": cameraID, "profile_id": p.ID, "profile_name": p.Name, "keep_values": keep,
		})
	})
}

// GroupCameraIDs returns the members of a camera group the actor can view.
func (s *Service) GroupCameraIDs(ctx context.Context, actor authz.Actor, groupID uuid.UUID) ([]uuid.UUID, error) {
	g, err := s.GetCameraGroup(ctx, actor, groupID)
	if err != nil {
		return nil, err
	}
	return g.CameraIDs, nil
}

func sortedKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sortStrings(out)
	return out
}

func sortStrings(s []string) { slices.Sort(s) }
