package inventory

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/access"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/frigate"
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
	OnvifEndpoint      *string
	OnvifProfile       *string
	OnvifVersion       *string
	OnvifCapabilities  json.RawMessage
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
			OnvifEndpoint: in.OnvifEndpoint, OnvifProfile: in.OnvifProfile, OnvifVersion: in.OnvifVersion,
			OnvifCapabilities: in.OnvifCapabilities,
		}
		if in.Tags != nil {
			params.Tags = *in.Tags
		}
		if err := q.UpdateCamera(ctx, params); err != nil {
			return err
		}
		if err := q.RematerializeDynamicGroups(ctx, db.RematerializeDynamicGroupsParams{TenantID: cam.TenantID}); err != nil {
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

func (s *Service) UpdateCameraOnvif(ctx context.Context, actor authz.Actor, id uuid.UUID, endpoint, profile, version *string, capabilities json.RawMessage) (db.GetCameraRow, error) {
	err := s.tx(ctx, actor, func(q *db.Queries, c *access.Checker) error {
		cam, err := q.GetCamera(ctx, id)
		if err != nil {
			return notFoundOr(err)
		}
		if err := c.Require(authz.CamerasManage, cameraResource(cam)); err != nil {
			return err
		}
		if len(capabilities) == 0 {
			capabilities = json.RawMessage("{}")
		}
		if err := q.UpdateCameraOnvif(ctx, db.UpdateCameraOnvifParams{
			ID:                id,
			OnvifEndpoint:     endpoint,
			OnvifProfile:      profile,
			OnvifVersion:      version,
			OnvifCapabilities: capabilities,
		}); err != nil {
			return err
		}
		return audit(ctx, q, actor, &cam.TenantID, ActionCameraUpdated, "camera", id, map[string]any{
			"onvif_endpoint": endpoint, "onvif_profile": profile, "onvif_version": version,
		})
	})
	if err != nil {
		return db.GetCameraRow{}, err
	}
	return s.GetCamera(ctx, actor, id)
}

func (s *Service) ListServerCameras(ctx context.Context, actor authz.Actor, serverID uuid.UUID) ([]db.GetCameraRow, error) {
	var out []db.GetCameraRow
	err := s.tx(ctx, actor, func(q *db.Queries, c *access.Checker) error {
		srv, err := q.GetServer(ctx, serverID)
		if err != nil {
			return notFoundOr(err)
		}
		if err := c.Require(authz.ServersManage, access.Server(srv.TenantID, srv.SiteID, srv.ID)); err != nil {
			return err
		}
		cams, err := q.ListServerCameras(ctx, serverID)
		if err != nil {
			return err
		}
		for _, cam := range cams {
			row, err := q.GetCamera(ctx, cam.ID)
			if err == nil {
				out = append(out, row)
			}
		}
		return nil
	})
	return out, err
}

// CameraGroupView lists only members the caller can view.
type CameraGroupView struct {
	db.CameraGroup
	CameraIDs []uuid.UUID
	// DynamicFilter is nil for manual groups.
	DynamicFilter *CameraGroupFilter
	// MemberCountTotal counts every member in the tenant; MemberCountHidden those the caller cannot view.
	MemberCountTotal  int
	MemberCountHidden int
}

func groupView(g db.CameraGroup, seen, hidden []uuid.UUID) CameraGroupView {
	return CameraGroupView{CameraGroup: g, CameraIDs: seen, DynamicFilter: parseGroupFilter(g.Filter),
		MemberCountTotal: len(seen) + len(hidden), MemberCountHidden: len(hidden)}
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
			members, hidden, err := visibleMembers(ctx, q, g.ID, visible)
			if err != nil {
				return err
			}
			if len(members) > 0 || c.Can(authz.CamerasView, access.CameraGroup(g.TenantID, g.ID)) {
				out = append(out, groupView(g, members, hidden))
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
		members, hidden, err := visibleMembers(ctx, q, id, visible)
		if err != nil {
			return err
		}
		if len(members) == 0 && !c.Can(authz.CamerasView, access.CameraGroup(g.TenantID, g.ID)) {
			return access.ErrForbidden
		}
		out = groupView(g, members, hidden)
		return nil
	})
	return out, err
}

type CameraGroupInput struct {
	TenantID    uuid.UUID
	Name        string
	Description string
	CameraIDs   []uuid.UUID
	// Filter makes the group dynamic: members are computed from it and CameraIDs must be empty.
	Filter *CameraGroupFilter
	// ExpectedVersion, when set, makes ReplaceCameraGroup fail with ErrCameraGroupVersionConflict if stale.
	ExpectedVersion *int32
}

// prepareGroupFilter validates the dynamic/manual shape of the input and returns the encoded filter.
func prepareGroupFilter(in *CameraGroupInput) ([]byte, error) {
	if in.Filter == nil {
		return nil, nil
	}
	if len(in.CameraIDs) > 0 {
		return nil, invalid("camera_ids cannot be set on a dynamic group; its members come from the filter")
	}
	f, err := normalizeCameraGroupFilter(*in.Filter)
	if err != nil {
		return nil, err
	}
	in.Filter = &f
	return f.marshal()
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
	filter, err := prepareGroupFilter(&in)
	if err != nil {
		return CameraGroupView{}, err
	}
	var id uuid.UUID
	err = s.tx(ctx, actor, func(q *db.Queries, c *access.Checker) error {
		if !actor.IsPlatform() && *actor.TenantID != in.TenantID {
			return store.ErrNotFound
		}
		if err := c.Require(authz.CamerasManage, access.Tenant(in.TenantID)); err != nil {
			return err
		}
		if err := requireManageCameras(ctx, q, c, in.TenantID, in.CameraIDs); err != nil {
			return err
		}
		g, err := q.CreateCameraGroup(ctx, db.CreateCameraGroupParams{TenantID: in.TenantID, Name: in.Name, Description: in.Description, Filter: filter})
		if err != nil {
			return store.Classify(err)
		}
		id = g.ID
		if filter != nil {
			if err := q.RematerializeDynamicGroups(ctx, db.RematerializeDynamicGroupsParams{TenantID: in.TenantID, GroupID: &id}); err != nil {
				return err
			}
			return audit(ctx, q, actor, &in.TenantID, ActionCameraGroupChanged, "camera_group", id, map[string]any{"created": true, "dynamic": true})
		}
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
	filter, err := prepareGroupFilter(&in)
	if err != nil {
		return CameraGroupView{}, err
	}
	err = s.tx(ctx, actor, func(q *db.Queries, c *access.Checker) error {
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
		// A group is manual or dynamic for its whole life.
		if (g.Filter != nil) != (filter != nil) {
			if g.Filter != nil {
				return invalid("a dynamic group requires a filter; its members cannot be edited by hand")
			}
			return invalid("a manual group cannot become dynamic; create a new group with a filter")
		}
		if filter != nil {
			// The filter can pull in cameras the caller does not manage one by one: require tenant-wide manage.
			if err := c.Require(authz.CamerasManage, access.Tenant(g.TenantID)); err != nil {
				return err
			}
			n, err := q.UpdateCameraGroup(ctx, db.UpdateCameraGroupParams{ID: id, Name: in.Name, Description: in.Description, Filter: filter, ExpectedVersion: in.ExpectedVersion})
			if err != nil {
				return store.Classify(err)
			}
			if n == 0 {
				return ErrCameraGroupVersionConflict
			}
			if err := q.RematerializeDynamicGroups(ctx, db.RematerializeDynamicGroupsParams{TenantID: g.TenantID, GroupID: &id}); err != nil {
				return err
			}
			return audit(ctx, q, actor, &g.TenantID, ActionCameraGroupChanged, "camera_group", id, map[string]any{"dynamic": true})
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
		n, err := q.UpdateCameraGroup(ctx, db.UpdateCameraGroupParams{ID: id, Name: in.Name, Description: in.Description, ExpectedVersion: in.ExpectedVersion})
		if err != nil {
			return store.Classify(err)
		}
		if n == 0 {
			return ErrCameraGroupVersionConflict
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

// CameraFrigateConfigView presents detection, tracked objects and LPR configuration for a camera.
type CameraFrigateConfigView struct {
	CameraID       uuid.UUID `json:"camera_id"`
	CameraName     string    `json:"camera_name"`
	ServerID       uuid.UUID `json:"server_id"`
	DetectEnabled  bool      `json:"detect_enabled"`
	TrackedObjects []string  `json:"tracked_objects"`
	LPREnabled     bool      `json:"lpr_enabled"`
	Zones          []string  `json:"zones"`
}

// GetCameraFrigateConfig reads the camera's detection and analytics config directly from Frigate.
func (s *Service) GetCameraFrigateConfig(ctx context.Context, actor authz.Actor, cameraID uuid.UUID) (CameraFrigateConfigView, error) {
	var cam db.GetCameraRow
	var srv db.GetServerRow
	err := s.tx(ctx, actor, func(q *db.Queries, c *access.Checker) error {
		var err error
		cam, err = q.GetCamera(ctx, cameraID)
		if err != nil {
			return notFoundOr(err)
		}
		if err := c.Require(authz.CamerasView, cameraResource(cam)); err != nil {
			return err
		}
		srv, err = q.GetServer(ctx, cam.ServerID)
		if err != nil {
			return notFoundOr(err)
		}
		return nil
	})
	if err != nil {
		return CameraFrigateConfigView{}, err
	}

	info, err := s.connInfo(db.FrigateServer{
		ID:             srv.ID,
		BaseUrl:        srv.BaseUrl,
		AuthMode:       srv.AuthMode,
		Username:       srv.Username,
		PasswordSealed: srv.PasswordSealed,
		TlsSkipVerify:  srv.TlsSkipVerify,
	})
	if err != nil {
		return CameraFrigateConfigView{}, err
	}
	adapter, err := s.Connect(ctx, info)
	if err != nil {
		return CameraFrigateConfigView{}, &FrigateError{Err: err}
	}
	cfg, err := adapter.GetCameraConfig(ctx, cam.RemoteName)
	if err != nil {
		return CameraFrigateConfigView{}, &FrigateError{Err: err}
	}

	return CameraFrigateConfigView{
		CameraID:       cam.ID,
		CameraName:     cam.RemoteName,
		ServerID:       cam.ServerID,
		DetectEnabled:  cfg.DetectEnabled,
		TrackedObjects: cfg.TrackedObjects,
		LPREnabled:     cfg.LPREnabled,
		Zones:          cfg.Zones,
	}, nil
}

// UpdateCameraFrigateConfig updates Frigate config (detect, tracked objects, LPR) and logs SERVER_CONFIG_UPDATED audit.
func (s *Service) UpdateCameraFrigateConfig(ctx context.Context, actor authz.Actor, cameraID uuid.UUID, update frigate.CameraFrigateConfigUpdate) (CameraFrigateConfigView, error) {
	var cam db.GetCameraRow
	var srv db.GetServerRow
	err := s.tx(ctx, actor, func(q *db.Queries, c *access.Checker) error {
		var err error
		cam, err = q.GetCamera(ctx, cameraID)
		if err != nil {
			return notFoundOr(err)
		}
		srv, err = q.GetServer(ctx, cam.ServerID)
		if err != nil {
			return notFoundOr(err)
		}
		// servers.config is required on the server to alter its configuration
		return c.Require(authz.ServersConfig, access.Server(srv.TenantID, srv.SiteID, srv.ID))
	})
	if err != nil {
		return CameraFrigateConfigView{}, err
	}

	info, err := s.connInfo(db.FrigateServer{
		ID:             srv.ID,
		BaseUrl:        srv.BaseUrl,
		AuthMode:       srv.AuthMode,
		Username:       srv.Username,
		PasswordSealed: srv.PasswordSealed,
		TlsSkipVerify:  srv.TlsSkipVerify,
	})
	if err != nil {
		return CameraFrigateConfigView{}, err
	}
	adapter, err := s.Connect(ctx, info)
	if err != nil {
		return CameraFrigateConfigView{}, &FrigateError{Err: err}
	}

	before, err := adapter.GetCameraConfig(ctx, cam.RemoteName)
	if err != nil {
		return CameraFrigateConfigView{}, &FrigateError{Err: err}
	}

	// Same path as the generic editor: applied live where Frigate allows it, with a
	// stored revision to roll back to.
	if patch := frigate.LegacyPatch(update); len(patch) > 0 {
		if _, err := s.PatchCameraFrigateConfig(ctx, actor, cameraID, patch); err != nil {
			return CameraFrigateConfigView{}, err
		}
	}
	after, err := adapter.GetCameraConfig(ctx, cam.RemoteName)
	if err != nil {
		return CameraFrigateConfigView{}, &FrigateError{Err: err}
	}

	err = s.tx(ctx, actor, func(q *db.Queries, _ *access.Checker) error {
		return audit(ctx, q, actor, &srv.TenantID, ActionServerConfigUpdated, "server", srv.ID, map[string]any{
			"camera_id":   cam.ID,
			"camera_name": cam.RemoteName,
			"server_id":   srv.ID,
			"before":      before,
			"after":       after,
		})
	})
	if err != nil {
		return CameraFrigateConfigView{}, err
	}

	return CameraFrigateConfigView{
		CameraID:       cam.ID,
		CameraName:     cam.RemoteName,
		ServerID:       cam.ServerID,
		DetectEnabled:  after.DetectEnabled,
		TrackedObjects: after.TrackedObjects,
		LPREnabled:     after.LPREnabled,
		Zones:          after.Zones,
	}, nil
}

// DeleteCamera removes a camera from OpenVMS without touching its history. The row is kept
// (soft delete) so events, alarms and exports stay attached, and a tombstone stops the Frigate sync
// from adding the same remote_name again. retainDays is the camera's recording window, kept for the
// purge of its history; nil means unknown. configRevisionID is the Frigate config revision that took the
// camera out of Frigate, kept so a restore can put its block back. It needs servers.manage on the
// camera's server. This only changes OpenVMS; RemoveCamera also removes the camera from Frigate.
func (s *Service) DeleteCamera(ctx context.Context, actor authz.Actor, id uuid.UUID, retainDays *int32, configRevisionID *uuid.UUID) error {
	return s.tx(ctx, actor, func(q *db.Queries, c *access.Checker) error {
		cam, err := q.GetCamera(ctx, id)
		if err != nil {
			return notFoundOr(err)
		}
		if err := c.Require(authz.ServersManage, access.Server(cam.TenantID, cam.SiteID, cam.ServerID)); err != nil {
			return err
		}
		n, err := q.SoftDeleteCamera(ctx, db.SoftDeleteCameraParams{ID: id, DeletedBy: &actor.UserID})
		if err != nil {
			return err
		}
		if n == 0 {
			return store.ErrNotFound
		}
		if err := q.InsertCameraTombstone(ctx, db.InsertCameraTombstoneParams{
			CameraID: cam.ID, TenantID: cam.TenantID, ServerID: cam.ServerID, RemoteName: cam.RemoteName,
			RetainDays: retainDays, ConfigRevisionID: configRevisionID, DeletedBy: &actor.UserID,
		}); err != nil {
			return store.Classify(err)
		}
		if _, err := q.ClearCameraViewCells(ctx, id.String()); err != nil {
			return err
		}
		if err := q.RematerializeDynamicGroups(ctx, db.RematerializeDynamicGroupsParams{TenantID: cam.TenantID}); err != nil {
			return err
		}
		return audit(ctx, q, actor, &cam.TenantID, ActionCameraRemoved, "camera", id, map[string]any{
			"remote_name": cam.RemoteName, "display_name": cam.DisplayName, "server_id": cam.ServerID, "retain_days": retainDays,
			"config_revision_id": configRevisionID,
		})
	})
}

// RestoreCamera brings a deleted camera back as the same row, so the history it already has joins
// the recordings that come next. It lifts the tombstone, and is refused with store.ErrConflict when a
// live camera already has the same remote_name on that server.
func (s *Service) RestoreCamera(ctx context.Context, actor authz.Actor, id uuid.UUID) (db.GetCameraRow, error) {
	err := s.tx(ctx, actor, func(q *db.Queries, c *access.Checker) error {
		cam, err := q.GetDeletedCamera(ctx, id)
		if err != nil {
			return notFoundOr(err)
		}
		if err := c.Require(authz.ServersManage, access.Server(cam.TenantID, cam.SiteID, cam.ServerID)); err != nil {
			return err
		}
		n, err := q.RestoreCamera(ctx, id)
		if err != nil {
			return store.Classify(err)
		}
		if n == 0 {
			return store.ErrNotFound
		}
		if _, err := q.DeleteCameraTombstone(ctx, id); err != nil {
			return err
		}
		if err := q.RematerializeDynamicGroups(ctx, db.RematerializeDynamicGroupsParams{TenantID: cam.TenantID}); err != nil {
			return err
		}
		return audit(ctx, q, actor, &cam.TenantID, ActionCameraRestored, "camera", id, map[string]any{
			"remote_name": cam.RemoteName, "display_name": cam.DisplayName, "server_id": cam.ServerID,
		})
	})
	if err != nil {
		return db.GetCameraRow{}, err
	}
	return s.GetCamera(ctx, actor, id)
}
