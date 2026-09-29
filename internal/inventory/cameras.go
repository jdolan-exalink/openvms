package inventory

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/access"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

type CameraFilter struct {
	SiteID   *uuid.UUID
	ServerID *uuid.UUID
	GroupID  *uuid.UUID
	Query    *string
}

// ListCameras returns only cameras covered by cameras.view. Others are never counted
// or hinted at (PRD §31).
func (s *Service) ListCameras(ctx context.Context, actor authz.Actor, f CameraFilter) ([]db.GetCameraRow, error) {
	var out []db.GetCameraRow
	err := s.tx(ctx, actor, func(q *db.Queries, c *access.Checker) error {
		ids, err := c.CameraIDs(ctx, authz.CamerasView)
		if err != nil {
			return err
		}
		if f.Query != nil && strings.TrimSpace(*f.Query) == "" {
			f.Query = nil
		}
		rows, err := q.ListCameras(ctx, db.ListCamerasParams{Ids: ids, SiteID: f.SiteID, ServerID: f.ServerID, GroupID: f.GroupID, Q: f.Query})
		if err != nil {
			return err
		}
		for _, r := range rows {
			out = append(out, db.GetCameraRow(r))
		}
		return nil
	})
	return out, err
}

func cameraResource(c db.GetCameraRow) authz.Resource {
	return access.Camera(c.TenantID, c.SiteID, c.ServerID, c.ID, c.GroupIds)
}

func (s *Service) GetCamera(ctx context.Context, actor authz.Actor, id uuid.UUID) (db.GetCameraRow, error) {
	var out db.GetCameraRow
	err := s.tx(ctx, actor, func(q *db.Queries, c *access.Checker) error {
		cam, err := q.GetCamera(ctx, id)
		if err != nil {
			return notFoundOr(err)
		}
		if err := c.Require(authz.CamerasView, cameraResource(cam)); err != nil {
			return err
		}
		out = cam
		return nil
	})
	return out, err
}

type CameraUpdate struct {
	DisplayName        *string
	Enabled            *bool
	DefaultLiveQuality *string
	Description        *string
	Location           *string
	Tags               *[]string
}

// Limits for the VMS-side camera settings; they mirror maxLength/maxItems in the OpenAPI contract.
const (
	maxCameraDescription = 1000
	maxCameraLocation    = 200
	maxCameraTags        = 20
	maxCameraTagLength   = 40
)

// normalizeCameraUpdate validates the VMS-side settings in place: text is trimmed, the live
// quality must be "sub" or "main", and tags are trimmed, de-duplicated case-insensitively
// (first spelling wins) and bounded. display_name is handled by UpdateCamera itself.
func normalizeCameraUpdate(in *CameraUpdate) error {
	if in.DefaultLiveQuality != nil && *in.DefaultLiveQuality != "sub" && *in.DefaultLiveQuality != "main" {
		return invalid("default_live_quality must be sub or main")
	}
	if in.Description != nil {
		in.Description = ptr(strings.TrimSpace(*in.Description))
		if len(*in.Description) > maxCameraDescription {
			return invalid("description is too long (max %d characters)", maxCameraDescription)
		}
	}
	if in.Location != nil {
		in.Location = ptr(strings.TrimSpace(*in.Location))
		if len(*in.Location) > maxCameraLocation {
			return invalid("location is too long (max %d characters)", maxCameraLocation)
		}
	}
	if in.Tags != nil {
		seen := map[string]bool{}
		tags := []string{}
		for _, raw := range *in.Tags {
			tag := strings.TrimSpace(raw)
			key := strings.ToLower(tag)
			if tag == "" || seen[key] {
				continue
			}
			if len(tag) > maxCameraTagLength {
				return invalid("tag is too long (max %d characters)", maxCameraTagLength)
			}
			seen[key] = true
			tags = append(tags, tag)
		}
		if len(tags) > maxCameraTags {
			return invalid("too many tags (max %d)", maxCameraTags)
		}
		in.Tags = &tags
	}
	return nil
}

func (s *Service) UpdateCamera(ctx context.Context, actor authz.Actor, id uuid.UUID, in CameraUpdate) (db.GetCameraRow, error) {
	if in.DisplayName != nil {
		if in.DisplayName = ptr(strings.TrimSpace(*in.DisplayName)); *in.DisplayName == "" {
			return db.GetCameraRow{}, invalid("display_name cannot be empty")
		}
	}
	if err := normalizeCameraUpdate(&in); err != nil {
		return db.GetCameraRow{}, err
	}
	err := s.tx(ctx, actor, func(q *db.Queries, c *access.Checker) error {
		cam, err := q.GetCamera(ctx, id)
		if err != nil {
			return notFoundOr(err)
		}
		if err := c.Require(authz.CamerasManage, cameraResource(cam)); err != nil {
			return err
		}
		params := db.UpdateCameraParams{
			ID: id, DisplayName: in.DisplayName, Enabled: in.Enabled,
			DefaultLiveQuality: in.DefaultLiveQuality, Description: in.Description, Location: in.Location,
		}
		if in.Tags != nil {
			params.Tags = *in.Tags
		}
		if err := q.UpdateCamera(ctx, params); err != nil {
			return err
		}
		return audit(ctx, q, actor, &cam.TenantID, ActionCameraUpdated, "camera", id, map[string]any{
			"display_name": in.DisplayName, "enabled": in.Enabled,
			"default_live_quality": in.DefaultLiveQuality, "description": in.Description,
			"location": in.Location, "tags": in.Tags,
		})
	})
	if err != nil {
		return db.GetCameraRow{}, err
	}
	return s.GetCamera(ctx, actor, id)
}

// CameraGroupView lists only members the caller can view.
type CameraGroupView struct {
	db.CameraGroup
	CameraIDs []uuid.UUID
}

func visibleMembers(ctx context.Context, q *db.Queries, groupID uuid.UUID, visible map[uuid.UUID]bool) ([]uuid.UUID, []uuid.UUID, error) {
	members, err := q.ListCameraGroupMembers(ctx, groupID)
	if err != nil {
		return nil, nil, err
	}
	var seen, hidden []uuid.UUID
	for _, m := range members {
		if visible[m] {
			seen = append(seen, m)
		} else {
			hidden = append(hidden, m)
		}
	}
	return seen, hidden, nil
}

func (s *Service) visibleCameraSet(ctx context.Context, c *access.Checker) (map[uuid.UUID]bool, error) {
	ids, err := c.CameraIDs(ctx, authz.CamerasView)
	if err != nil {
		return nil, err
	}
	set := make(map[uuid.UUID]bool, len(ids))
	for _, id := range ids {
		set[id] = true
	}
	return set, nil
}

func (s *Service) ListCameraGroups(ctx context.Context, actor authz.Actor) ([]CameraGroupView, error) {
	var out []CameraGroupView
	err := s.tx(ctx, actor, func(q *db.Queries, c *access.Checker) error {
		visible, err := s.visibleCameraSet(ctx, c)
		if err != nil {
			return err
		}
		groups, err := q.ListCameraGroups(ctx, actor.TenantID)
		if err != nil {
			return err
		}
		for _, g := range groups {
			members, _, err := visibleMembers(ctx, q, g.ID, visible)
			if err != nil {
				return err
			}
			if len(members) > 0 || c.Can(authz.CamerasView, access.CameraGroup(g.TenantID, g.ID)) {
				out = append(out, CameraGroupView{CameraGroup: g, CameraIDs: members})
			}
		}
		return nil
	})
	return out, err
}

func (s *Service) GetCameraGroup(ctx context.Context, actor authz.Actor, id uuid.UUID) (CameraGroupView, error) {
	var out CameraGroupView
	err := s.tx(ctx, actor, func(q *db.Queries, c *access.Checker) error {
		g, err := q.GetCameraGroup(ctx, id)
		if err != nil {
			return notFoundOr(err)
		}
		visible, err := s.visibleCameraSet(ctx, c)
		if err != nil {
			return err
		}
		members, _, err := visibleMembers(ctx, q, id, visible)
		if err != nil {
			return err
		}
		if len(members) == 0 && !c.Can(authz.CamerasView, access.CameraGroup(g.TenantID, g.ID)) {
			return access.ErrForbidden
		}
		out = CameraGroupView{CameraGroup: g, CameraIDs: members}
		return nil
	})
	return out, err
}

type CameraGroupInput struct {
	TenantID    uuid.UUID
	Name        string
	Description string
	CameraIDs   []uuid.UUID
}

// requireManageCameras checks cameras.manage on each camera and that all belong to tenantID.
func requireManageCameras(ctx context.Context, q *db.Queries, c *access.Checker, tenantID uuid.UUID, ids []uuid.UUID) error {
	for _, id := range ids {
		cam, err := q.GetCamera(ctx, id)
		if err != nil {
			if errors.Is(store.Classify(err), store.ErrNotFound) {
				return invalid("camera %s does not exist", id)
			}
			return err
		}
		if cam.TenantID != tenantID {
			return invalid("camera %s belongs to another tenant", id)
		}
		if err := c.Require(authz.CamerasManage, cameraResource(cam)); err != nil {
			return err
		}
	}
	return nil
}

func dedupe(ids []uuid.UUID) []uuid.UUID {
	seen := make(map[uuid.UUID]bool, len(ids))
	out := ids[:0:0]
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

func (s *Service) CreateCameraGroup(ctx context.Context, actor authz.Actor, in CameraGroupInput) (CameraGroupView, error) {
	if in.Name = strings.TrimSpace(in.Name); in.Name == "" {
		return CameraGroupView{}, invalid("name is required")
	}
	in.CameraIDs = dedupe(in.CameraIDs)
	var id uuid.UUID
	err := s.tx(ctx, actor, func(q *db.Queries, c *access.Checker) error {
		if !actor.IsPlatform() && *actor.TenantID != in.TenantID {
			return store.ErrNotFound
		}
		if err := c.Require(authz.CamerasManage, access.Tenant(in.TenantID)); err != nil {
			return err
		}
		if err := requireManageCameras(ctx, q, c, in.TenantID, in.CameraIDs); err != nil {
			return err
		}
		g, err := q.CreateCameraGroup(ctx, db.CreateCameraGroupParams{TenantID: in.TenantID, Name: in.Name, Description: in.Description})
		if err != nil {
			return store.Classify(err)
		}
		id = g.ID
		if err := q.AddCameraGroupMembers(ctx, db.AddCameraGroupMembersParams{GroupID: id, CameraIds: in.CameraIDs, TenantID: in.TenantID}); err != nil {
			return err
		}
		return audit(ctx, q, actor, &in.TenantID, ActionCameraGroupChanged, "camera_group", id, map[string]any{"created": true, "members": len(in.CameraIDs)})
	})
	if err != nil {
		return CameraGroupView{}, err
	}
	return s.GetCameraGroup(ctx, actor, id)
}

// ReplaceCameraGroup sets name, description and members. Members the caller cannot view
// are kept as they are: nobody can remove cameras they are not allowed to see.
func (s *Service) ReplaceCameraGroup(ctx context.Context, actor authz.Actor, id uuid.UUID, in CameraGroupInput) (CameraGroupView, error) {
	if in.Name = strings.TrimSpace(in.Name); in.Name == "" {
		return CameraGroupView{}, invalid("name is required")
	}
	in.CameraIDs = dedupe(in.CameraIDs)
	err := s.tx(ctx, actor, func(q *db.Queries, c *access.Checker) error {
		g, err := q.GetCameraGroup(ctx, id)
		if err != nil {
			return notFoundOr(err)
		}
		if g.TenantID != in.TenantID {
			return invalid("tenant_id cannot change")
		}
		if err := c.Require(authz.CamerasManage, access.CameraGroup(g.TenantID, g.ID)); err != nil {
			return err
		}
		visible, err := s.visibleCameraSet(ctx, c)
		if err != nil {
			return err
		}
		current, hidden, err := visibleMembers(ctx, q, id, visible)
		if err != nil {
			return err
		}
		// Adding or removing a visible camera requires cameras.manage on it.
		want := make(map[uuid.UUID]bool, len(in.CameraIDs))
		for _, cid := range in.CameraIDs {
			want[cid] = true
		}
		var changed []uuid.UUID
		for _, cid := range in.CameraIDs {
			if !contains(current, cid) {
				changed = append(changed, cid)
			}
		}
		for _, cid := range current {
			if !want[cid] {
				changed = append(changed, cid)
			}
		}
		if err := requireManageCameras(ctx, q, c, g.TenantID, changed); err != nil {
			return err
		}
		if err := q.UpdateCameraGroup(ctx, db.UpdateCameraGroupParams{ID: id, Name: in.Name, Description: in.Description}); err != nil {
			return store.Classify(err)
		}
		if err := q.ClearCameraGroupMembers(ctx, id); err != nil {
			return err
		}
		members := append(append([]uuid.UUID{}, in.CameraIDs...), hidden...)
		if err := q.AddCameraGroupMembers(ctx, db.AddCameraGroupMembersParams{GroupID: id, CameraIds: members, TenantID: g.TenantID}); err != nil {
			return err
		}
		return audit(ctx, q, actor, &g.TenantID, ActionCameraGroupChanged, "camera_group", id, map[string]any{"members": len(members)})
	})
	if err != nil {
		return CameraGroupView{}, err
	}
	return s.GetCameraGroup(ctx, actor, id)
}

func (s *Service) DeleteCameraGroup(ctx context.Context, actor authz.Actor, id uuid.UUID) error {
	return s.tx(ctx, actor, func(q *db.Queries, c *access.Checker) error {
		g, err := q.GetCameraGroup(ctx, id)
		if err != nil {
			return notFoundOr(err)
		}
		if err := c.Require(authz.CamerasManage, access.CameraGroup(g.TenantID, g.ID)); err != nil {
			return err
		}
		if err := q.SoftDeleteCameraGroup(ctx, db.SoftDeleteCameraGroupParams{ID: id, DeletedBy: &actor.UserID}); err != nil {
			return err
		}
		return audit(ctx, q, actor, &g.TenantID, ActionCameraGroupChanged, "camera_group", id, map[string]any{"deleted": true, "name": g.Name})
	})
}

func contains(ids []uuid.UUID, id uuid.UUID) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}
