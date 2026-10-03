package inventory

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/jdolan-exalink/openvms/internal/access"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
	"github.com/jdolan-exalink/openvms/internal/vehicle"
)

// ClassifyServer is one server's fine-classifier switch. Missing rows are on.
type ClassifyServer struct {
	ID      uuid.UUID
	Enabled bool
}

// ClassifyCamera is one camera's switch and whether it actually runs.
type ClassifyCamera struct {
	ID        uuid.UUID
	ServerID  uuid.UUID
	Enabled   bool
	Effective bool
}

// ClassifyPolicy lists the switches the actor can see.
func (s *Service) ClassifyPolicy(ctx context.Context, actor authz.Actor) ([]ClassifyServer, []ClassifyCamera, error) {
	servers, err := s.ListServers(ctx, actor, nil)
	if err != nil {
		return nil, nil, err
	}
	cameras, err := s.ListCameras(ctx, actor, CameraFilter{})
	if err != nil {
		return nil, nil, err
	}
	serverIDs := make([]uuid.UUID, 0, len(servers))
	for _, srv := range servers {
		serverIDs = append(serverIDs, srv.ID)
	}
	cameraIDs := make([]uuid.UUID, 0, len(cameras))
	for _, cam := range cameras {
		cameraIDs = append(cameraIDs, cam.ID)
	}
	serverFlags, cameraFlags, err := loadClassifyFlags(ctx, s.Store, actor, serverIDs, cameraIDs)
	if err != nil {
		return nil, nil, err
	}
	outServers := make([]ClassifyServer, len(servers))
	for i, srv := range servers {
		enabled := true
		if v, ok := serverFlags[srv.ID]; ok {
			enabled = v
		}
		outServers[i] = ClassifyServer{ID: srv.ID, Enabled: enabled}
	}
	outCameras := make([]ClassifyCamera, len(cameras))
	for i, cam := range cameras {
		serverOn, cameraOn := true, true
		if v, ok := serverFlags[cam.ServerID]; ok {
			serverOn = v
		}
		if v, ok := cameraFlags[cam.ID]; ok {
			cameraOn = v
		}
		outCameras[i] = ClassifyCamera{
			ID: cam.ID, ServerID: cam.ServerID, Enabled: cameraOn,
			Effective: vehicle.Effective(&serverOn, &cameraOn),
		}
	}
	return outServers, outCameras, nil
}

// SetServerBodyClassify turns the crop classifier on or off for a server.
func (s *Service) SetServerBodyClassify(ctx context.Context, actor authz.Actor, id uuid.UUID, enabled bool) error {
	var tenantID uuid.UUID
	err := s.tx(ctx, actor, func(q *db.Queries, c *access.Checker) error {
		srv, err := q.GetServer(ctx, id)
		if err != nil {
			return notFoundOr(err)
		}
		if err := c.Require(authz.ServersManage, access.Server(srv.TenantID, srv.SiteID, srv.ID)); err != nil {
			return err
		}
		tenantID = srv.TenantID
		return nil
	})
	if err != nil {
		return err
	}
	return s.Store.TxRaw(ctx, store.ScopeFor(actor), func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO body_classify_servers (server_id, tenant_id, enabled)
			VALUES ($1, $2, $3)
			ON CONFLICT (server_id) DO UPDATE SET enabled = excluded.enabled`, id, tenantID, enabled)
		return err
	})
}

// SetCameraBodyClassify turns the crop classifier on or off for one camera.
func (s *Service) SetCameraBodyClassify(ctx context.Context, actor authz.Actor, id uuid.UUID, enabled bool) error {
	var tenantID uuid.UUID
	err := s.tx(ctx, actor, func(q *db.Queries, c *access.Checker) error {
		cam, err := q.GetCamera(ctx, id)
		if err != nil {
			return notFoundOr(err)
		}
		if err := c.Require(authz.CamerasManage, cameraResource(cam)); err != nil {
			return err
		}
		tenantID = cam.TenantID
		return nil
	})
	if err != nil {
		return err
	}
	return s.Store.TxRaw(ctx, store.ScopeFor(actor), func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO body_classify_cameras (camera_id, tenant_id, enabled)
			VALUES ($1, $2, $3)
			ON CONFLICT (camera_id) DO UPDATE SET enabled = excluded.enabled`, id, tenantID, enabled)
		return err
	})
}

func loadClassifyFlags(ctx context.Context, st *store.Store, actor authz.Actor, serverIDs, cameraIDs []uuid.UUID) (map[uuid.UUID]bool, map[uuid.UUID]bool, error) {
	servers := map[uuid.UUID]bool{}
	cameras := map[uuid.UUID]bool{}
	err := st.TxRaw(ctx, store.ScopeFor(actor), func(tx pgx.Tx) error {
		if len(serverIDs) > 0 {
			rows, err := tx.Query(ctx, `SELECT server_id, enabled FROM body_classify_servers WHERE server_id = ANY($1)`, serverIDs)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var id uuid.UUID
				var enabled bool
				if err := rows.Scan(&id, &enabled); err != nil {
					return err
				}
				servers[id] = enabled
			}
			if err := rows.Err(); err != nil {
				return err
			}
		}
		if len(cameraIDs) > 0 {
			rows, err := tx.Query(ctx, `SELECT camera_id, enabled FROM body_classify_cameras WHERE camera_id = ANY($1)`, cameraIDs)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var id uuid.UUID
				var enabled bool
				if err := rows.Scan(&id, &enabled); err != nil {
					return err
				}
				cameras[id] = enabled
			}
			if err := rows.Err(); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil && isMissingClassifyTable(err) {
		return map[uuid.UUID]bool{}, map[uuid.UUID]bool{}, nil
	}
	return servers, cameras, err
}

func isMissingClassifyTable(err error) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && pg.Code == "42P01"
}
