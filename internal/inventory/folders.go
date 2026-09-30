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

// Audit actions for the shared camera folders (LV-13).
const (
	ActionFolderCreated    = "FOLDER_CREATED"
	ActionFolderRenamed    = "FOLDER_RENAMED"
	ActionFolderDeleted    = "FOLDER_DELETED"
	ActionCamerasReordered = "CAMERAS_REORDERED"
)

const (
	maxFolderName    = 80
	maxReorderCams   = 1000
	maxReorderFolder = 200
)

// FolderList is the tenant's shared folder tree as one caller may see it.
type FolderList struct {
	Folders []db.CameraFolder
	// ManageableServerIDs are the servers on which the caller holds cameras.manage.
	ManageableServerIDs []uuid.UUID
}

func folderServer(f db.GetCameraFolderRow) authz.Resource {
	return access.Server(f.TenantID, f.SiteID, f.ServerID)
}

// ListCameraFolders returns folders holding at least one camera the caller can view, plus every
// folder of the servers the caller can manage. Others are never counted or hinted at (PRD §31).
func (s *Service) ListCameraFolders(ctx context.Context, actor authz.Actor) (FolderList, error) {
	out := FolderList{Folders: []db.CameraFolder{}, ManageableServerIDs: []uuid.UUID{}}
	err := s.tx(ctx, actor, func(q *db.Queries, c *access.Checker) error {
		ids, err := c.CameraIDs(ctx, authz.CamerasView)
		if err != nil {
			return err
		}
		cams, err := q.ListCameras(ctx, db.ListCamerasParams{Ids: ids})
		if err != nil {
			return err
		}
		used := map[uuid.UUID]bool{}
		for _, cam := range cams {
			if cam.FolderID != nil {
				used[*cam.FolderID] = true
			}
		}
		servers, err := q.ListAllActiveServers(ctx)
		if err != nil {
			return err
		}
		manage := map[uuid.UUID]bool{}
		for _, srv := range servers {
			if c.Can(authz.CamerasManage, access.Server(srv.TenantID, srv.SiteID, srv.ID)) {
				manage[srv.ID] = true
				out.ManageableServerIDs = append(out.ManageableServerIDs, srv.ID)
			}
		}
		rows, err := q.ListCameraFolders(ctx)
		if err != nil {
			return err
		}
		for _, r := range rows {
			if used[r.ID] || manage[r.ServerID] {
				out.Folders = append(out.Folders, db.CameraFolder{
					ID: r.ID, TenantID: r.TenantID, ServerID: r.ServerID, Name: r.Name,
					SortOrder: r.SortOrder, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
				})
			}
		}
		return nil
	})
	return out, err
}

func normalizeFolderName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", invalid("name is required")
	}
	if len([]rune(name)) > maxFolderName {
		return "", invalid("name is too long (max %d characters)", maxFolderName)
	}
	return name, nil
}

// CreateCameraFolder adds a folder at the end of a server's folder list.
func (s *Service) CreateCameraFolder(ctx context.Context, actor authz.Actor, serverID uuid.UUID, name string) (db.CameraFolder, error) {
	name, err := normalizeFolderName(name)
	if err != nil {
		return db.CameraFolder{}, err
	}
	var out db.CameraFolder
	err = s.tx(ctx, actor, func(q *db.Queries, c *access.Checker) error {
		srv, err := q.GetServer(ctx, serverID)
		if err != nil {
			return notFoundOr(err)
		}
		if err := c.Require(authz.CamerasManage, access.Server(srv.TenantID, srv.SiteID, srv.ID)); err != nil {
			return err
		}
		out, err = q.CreateCameraFolder(ctx, db.CreateCameraFolderParams{TenantID: srv.TenantID, ServerID: srv.ID, Name: name})
		if err != nil {
			return store.Classify(err)
		}
		return audit(ctx, q, actor, &srv.TenantID, ActionFolderCreated, "camera_folder", out.ID, map[string]any{"server_id": srv.ID, "name": name})
	})
	return out, err
}

// RenameCameraFolder changes a folder's name.
func (s *Service) RenameCameraFolder(ctx context.Context, actor authz.Actor, id uuid.UUID, name string) (db.CameraFolder, error) {
	name, err := normalizeFolderName(name)
	if err != nil {
		return db.CameraFolder{}, err
	}
	var out db.CameraFolder
	err = s.tx(ctx, actor, func(q *db.Queries, c *access.Checker) error {
		f, err := q.GetCameraFolder(ctx, id)
		if err != nil {
			return notFoundOr(err)
		}
		if err := c.Require(authz.CamerasManage, folderServer(f)); err != nil {
			return err
		}
		if err := q.RenameCameraFolder(ctx, db.RenameCameraFolderParams{ID: id, Name: name}); err != nil {
			return store.Classify(err)
		}
		out = db.CameraFolder{ID: f.ID, TenantID: f.TenantID, ServerID: f.ServerID, Name: name, SortOrder: f.SortOrder, CreatedAt: f.CreatedAt, UpdatedAt: f.UpdatedAt}
		return audit(ctx, q, actor, &f.TenantID, ActionFolderRenamed, "camera_folder", id, map[string]any{"server_id": f.ServerID, "from": f.Name, "to": name})
	})
	return out, err
}

// DeleteCameraFolder removes a folder; its cameras fall back to the server root.
func (s *Service) DeleteCameraFolder(ctx context.Context, actor authz.Actor, id uuid.UUID) error {
	return s.tx(ctx, actor, func(q *db.Queries, c *access.Checker) error {
		f, err := q.GetCameraFolder(ctx, id)
		if err != nil {
			return notFoundOr(err)
		}
		if err := c.Require(authz.CamerasManage, folderServer(f)); err != nil {
			return err
		}
		n, err := q.CountFolderCameras(ctx, &id)
		if err != nil {
			return err
		}
		if err := q.DeleteCameraFolder(ctx, id); err != nil {
			return err
		}
		return audit(ctx, q, actor, &f.TenantID, ActionFolderDeleted, "camera_folder", id, map[string]any{"server_id": f.ServerID, "name": f.Name, "cameras_moved_to_root": n})
	})
}

// CameraPlacement puts a camera in a folder of its own server (nil = server root) at a position.
type CameraPlacement struct {
	CameraID  uuid.UUID
	FolderID  *uuid.UUID
	SortOrder int32
}

// FolderPosition sets a folder's position among its server's folders.
type FolderPosition struct {
	FolderID  uuid.UUID
	SortOrder int32
}

// ReorderCameraFolders applies every placement and folder position in one transaction, so a rejected
// item leaves the tree untouched. A camera may only move to a folder of its own server.
func (s *Service) ReorderCameraFolders(ctx context.Context, actor authz.Actor, cams []CameraPlacement, folders []FolderPosition) error {
	if len(cams) > maxReorderCams || len(folders) > maxReorderFolder {
		return invalid("too many items in one reorder")
	}
	return s.tx(ctx, actor, func(q *db.Queries, c *access.Checker) error {
		var tenantID *uuid.UUID
		for _, p := range cams {
			cam, err := q.GetCamera(ctx, p.CameraID)
			if err != nil {
				if errors.Is(notFoundOr(err), store.ErrNotFound) {
					return invalid("camera %s does not exist", p.CameraID)
				}
				return err
			}
			if err := c.Require(authz.CamerasManage, cameraResource(cam)); err != nil {
				return err
			}
			if p.FolderID != nil {
				f, err := q.GetCameraFolder(ctx, *p.FolderID)
				if err != nil {
					if errors.Is(notFoundOr(err), store.ErrNotFound) {
						return invalid("folder %s does not exist", *p.FolderID)
					}
					return err
				}
				if f.TenantID != cam.TenantID || f.ServerID != cam.ServerID {
					return invalid("camera %s can only be moved to a folder of its own server", p.CameraID)
				}
			}
			if err := q.SetCameraPlacement(ctx, db.SetCameraPlacementParams{ID: p.CameraID, FolderID: p.FolderID, SortOrder: p.SortOrder}); err != nil {
				return err
			}
			tenantID = &cam.TenantID
		}
		for _, p := range folders {
			f, err := q.GetCameraFolder(ctx, p.FolderID)
			if err != nil {
				if errors.Is(notFoundOr(err), store.ErrNotFound) {
					return invalid("folder %s does not exist", p.FolderID)
				}
				return err
			}
			if err := c.Require(authz.CamerasManage, folderServer(f)); err != nil {
				return err
			}
			if err := q.SetCameraFolderOrder(ctx, db.SetCameraFolderOrderParams{ID: p.FolderID, SortOrder: p.SortOrder}); err != nil {
				return err
			}
			tenantID = &f.TenantID
		}
		if tenantID == nil {
			return nil
		}
		return audit(ctx, q, actor, tenantID, ActionCamerasReordered, "tenant", *tenantID, map[string]any{"cameras": len(cams), "folders": len(folders)})
	})
}
