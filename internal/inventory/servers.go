package inventory

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/access"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/frigate"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

// ProbeResult is what a connection test discovers.
type ProbeResult struct {
	Version      string
	Adapter      string
	Capabilities frigate.Capabilities
	Cameras      []frigate.Camera
}

type ConnInput struct {
	BaseURL       string
	AuthMode      frigate.AuthMode
	Username      string
	Password      string
	TLSSkipVerify bool
}

func (in ConnInput) mode() frigate.AuthMode {
	if in.AuthMode == "" {
		return frigate.AuthCredentials
	}
	return in.AuthMode
}

func (in ConnInput) info() frigate.ConnInfo {
	info := frigate.ConnInfo{BaseURL: strings.TrimSpace(in.BaseURL), AuthMode: in.mode(), TLSSkipVerify: in.TLSSkipVerify}
	if in.mode() == frigate.AuthCredentials {
		info.Username, info.Password = in.Username, in.Password
	}
	return info
}

func (in ConnInput) validate() error {
	switch in.mode() {
	case frigate.AuthCredentials, frigate.AuthNone:
	default:
		return invalid("auth_mode must be credentials or none")
	}
	if _, err := frigate.ValidateBaseURL(in.BaseURL, in.mode()); err != nil {
		return invalid("%s", err.Error())
	}
	if in.mode() == frigate.AuthCredentials && (in.Username == "" || in.Password == "") {
		return invalid("username and password are required unless auth_mode is none")
	}
	return nil
}

func (s *Service) discover(ctx context.Context, info frigate.ConnInfo) (ProbeResult, error) {
	a, err := s.Connect(ctx, info)
	if err != nil {
		return ProbeResult{}, &FrigateError{Err: err}
	}
	caps, err := a.Capabilities(ctx)
	if err != nil {
		return ProbeResult{}, &FrigateError{Err: err}
	}
	cams, err := a.ListCameras(ctx)
	if err != nil {
		return ProbeResult{}, &FrigateError{Err: err}
	}
	return ProbeResult{Version: a.Version(), Adapter: a.Name(), Capabilities: caps, Cameras: cams}, nil
}

// requireSite checks that the site exists for the actor and that they hold p on it.
func requireSite(ctx context.Context, q *db.Queries, c *access.Checker, p authz.Permission, siteID uuid.UUID) (db.GetSiteRow, error) {
	site, err := q.GetSite(ctx, siteID)
	if err != nil {
		return site, notFoundOr(err)
	}
	return site, c.Require(p, access.Site(site.TenantID, site.ID))
}

// ProbeServer tests a connection for the registration wizard without saving anything.
func (s *Service) ProbeServer(ctx context.Context, actor authz.Actor, siteID uuid.UUID, conn ConnInput) (ProbeResult, error) {
	if err := conn.validate(); err != nil {
		return ProbeResult{}, err
	}
	err := s.tx(ctx, actor, func(q *db.Queries, c *access.Checker) error {
		_, err := requireSite(ctx, q, c, authz.ServersManage, siteID)
		return err
	})
	if err != nil {
		return ProbeResult{}, err
	}
	return s.discover(ctx, conn.info())
}

type ServerView struct {
	db.GetServerRow
}

func (v ServerView) Caps() frigate.Capabilities {
	var c frigate.Capabilities
	_ = json.Unmarshal(v.Capabilities, &c)
	return c
}

// HealthStats is what the health poller stores in frigate_servers.stats.
type HealthStats struct {
	UptimeSeconds int64            `json:"uptime_seconds"`
	Recordings    *frigate.Storage `json:"recordings,omitempty"`
}

func (v ServerView) HealthStats() HealthStats {
	var h HealthStats
	_ = json.Unmarshal(v.Stats, &h)
	return h
}

func (s *Service) ListServers(ctx context.Context, actor authz.Actor, siteID *uuid.UUID) ([]ServerView, error) {
	var out []ServerView
	err := s.tx(ctx, actor, func(q *db.Queries, c *access.Checker) error {
		ids, err := c.ServerIDs(ctx, authz.ServersView)
		if err != nil {
			return err
		}
		rows, err := q.ListServers(ctx, db.ListServersParams{Ids: ids, SiteID: siteID})
		if err != nil {
			return err
		}
		for _, r := range rows {
			out = append(out, ServerView{db.GetServerRow(r)})
		}
		return nil
	})
	return out, err
}

func (s *Service) GetServer(ctx context.Context, actor authz.Actor, id uuid.UUID) (ServerView, error) {
	var out ServerView
	err := s.tx(ctx, actor, func(q *db.Queries, c *access.Checker) error {
		srv, err := q.GetServer(ctx, id)
		if err != nil {
			return notFoundOr(err)
		}
		if err := c.Require(authz.ServersView, access.Server(srv.TenantID, srv.SiteID, srv.ID)); err != nil {
			return err
		}
		out = ServerView{srv}
		return nil
	})
	return out, err
}

type CreateServerInput struct {
	SiteID        uuid.UUID
	Name          string
	Conn          ConnInput
	ImportCameras bool
}

// CreateServer registers a Frigate server after proving the credentials work.
func (s *Service) CreateServer(ctx context.Context, actor authz.Actor, in CreateServerInput) (ServerView, error) {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return ServerView{}, invalid("name is required")
	}
	if err := in.Conn.validate(); err != nil {
		return ServerView{}, err
	}
	// Authorize before contacting Frigate so an unauthorized caller cannot use the
	// VMS to probe arbitrary hosts.
	var site db.GetSiteRow
	err := s.tx(ctx, actor, func(q *db.Queries, c *access.Checker) error {
		var err error
		site, err = requireSite(ctx, q, c, authz.ServersManage, in.SiteID)
		return err
	})
	if err != nil {
		return ServerView{}, err
	}
	found, err := s.discover(ctx, in.Conn.info())
	if err != nil {
		return ServerView{}, err
	}

	id := uuid.New()
	info := in.Conn.info()
	sealed, err := s.seal(info, id)
	if err != nil {
		return ServerView{}, err
	}
	caps, _ := json.Marshal(found.Capabilities)
	err = s.tx(ctx, actor, func(q *db.Queries, c *access.Checker) error {
		// Re-check inside the write transaction: grants may have changed meanwhile.
		if _, err := requireSite(ctx, q, c, authz.ServersManage, in.SiteID); err != nil {
			return err
		}
		err := q.CreateServer(ctx, db.CreateServerParams{
			ID: id, TenantID: site.TenantID, SiteID: site.ID, Name: in.Name,
			BaseUrl: strings.TrimRight(info.BaseURL, "/"), AuthMode: string(info.AuthMode), Username: info.Username,
			PasswordSealed: sealed, TlsSkipVerify: in.Conn.TLSSkipVerify,
			FrigateVersion: found.Version, Capabilities: caps,
		})
		if err != nil {
			return store.Classify(err)
		}
		imported := 0
		if in.ImportCameras {
			for _, cam := range found.Cameras {
				if _, err := upsertCamera(ctx, q, site.TenantID, site.ID, id, cam); err != nil {
					return err
				}
				imported++
			}
		}
		return audit(ctx, q, actor, &site.TenantID, ActionServerAdded, "server", id, map[string]any{
			"name": in.Name, "base_url": in.Conn.BaseURL, "auth_mode": info.AuthMode, "frigate_version": found.Version, "cameras_imported": imported,
		})
	})
	if err != nil {
		return ServerView{}, err
	}
	return s.GetServer(ctx, actor, id)
}

func upsertCamera(ctx context.Context, q *db.Queries, tenantID, siteID, serverID uuid.UUID, cam frigate.Camera) (bool, error) {
	return q.UpsertCamera(ctx, db.UpsertCameraParams{
		TenantID: tenantID, SiteID: siteID, ServerID: serverID,
		RemoteName: cam.Name, DisplayName: displayName(cam.Name), Enabled: cam.Enabled,
		Zones: cam.Zones, Lpr: cam.LPR, LiveStream: cam.LiveStream, HqStream: cam.HQStream,
	})
}

// displayName turns "acceso_norte" into "Acceso norte" as a starting label.
func displayName(remote string) string {
	s := strings.ReplaceAll(strings.ReplaceAll(remote, "_", " "), "-", " ")
	if s == "" {
		return remote
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

type UpdateServerInput struct {
	Name          *string
	SiteID        *uuid.UUID
	BaseURL       *string
	AuthMode      *frigate.AuthMode
	Username      *string
	Password      *string
	TLSSkipVerify *bool
}

func (s *Service) UpdateServer(ctx context.Context, actor authz.Actor, id uuid.UUID, in UpdateServerInput) (ServerView, error) {
	var current db.GetServerRow
	err := s.tx(ctx, actor, func(q *db.Queries, c *access.Checker) error {
		var err error
		current, err = q.GetServer(ctx, id)
		if err != nil {
			return notFoundOr(err)
		}
		if err := c.Require(authz.ServersManage, access.Server(current.TenantID, current.SiteID, current.ID)); err != nil {
			return err
		}
		if in.SiteID != nil && *in.SiteID != current.SiteID {
			site, err := requireSite(ctx, q, c, authz.ServersManage, *in.SiteID)
			if err != nil {
				return err
			}
			if site.TenantID != current.TenantID {
				return invalid("a server cannot move to another tenant")
			}
		}
		return nil
	})
	if err != nil {
		return ServerView{}, err
	}

	stored, err := s.connInfo(db.FrigateServer{ID: id, BaseUrl: current.BaseUrl, AuthMode: current.AuthMode, Username: current.Username, PasswordSealed: current.PasswordSealed, TlsSkipVerify: current.TlsSkipVerify})
	if err != nil {
		return ServerView{}, err
	}
	conn := ConnInput{BaseURL: stored.BaseURL, AuthMode: stored.AuthMode, Username: stored.Username, Password: stored.Password, TLSSkipVerify: stored.TLSSkipVerify}
	connChanged := false
	if in.BaseURL != nil {
		conn.BaseURL, connChanged = *in.BaseURL, true
	}
	if in.AuthMode != nil {
		conn.AuthMode, connChanged = *in.AuthMode, true
	}
	if in.Username != nil {
		conn.Username, connChanged = *in.Username, true
	}
	if in.Password != nil {
		conn.Password, connChanged = *in.Password, true
	}
	if in.TLSSkipVerify != nil {
		conn.TLSSkipVerify, connChanged = *in.TLSSkipVerify, true
	}
	if connChanged {
		if err := conn.validate(); err != nil {
			return ServerView{}, err
		}
		if _, err := s.discover(ctx, conn.info()); err != nil {
			return ServerView{}, err
		}
	}
	info := conn.info()
	sealed, err := s.seal(info, id)
	if err != nil {
		return ServerView{}, err
	}
	name, siteID := current.Name, current.SiteID
	if in.Name != nil {
		if name = strings.TrimSpace(*in.Name); name == "" {
			return ServerView{}, invalid("name cannot be empty")
		}
	}
	if in.SiteID != nil {
		siteID = *in.SiteID
	}
	err = s.tx(ctx, actor, func(q *db.Queries, c *access.Checker) error {
		if err := c.Require(authz.ServersManage, access.Server(current.TenantID, current.SiteID, current.ID)); err != nil {
			return err
		}
		err := q.UpdateServer(ctx, db.UpdateServerParams{
			ID: id, Name: name, SiteID: siteID, BaseUrl: strings.TrimRight(info.BaseURL, "/"), AuthMode: string(info.AuthMode),
			Username: info.Username, PasswordSealed: sealed, TlsSkipVerify: conn.TLSSkipVerify,
		})
		if err != nil {
			return store.Classify(err)
		}
		if siteID != current.SiteID {
			if err := q.MoveServerCameras(ctx, db.MoveServerCamerasParams{ServerID: id, SiteID: siteID}); err != nil {
				return err
			}
		}
		return audit(ctx, q, actor, &current.TenantID, ActionServerUpdated, "server", id, map[string]any{
			"renamed": name != current.Name, "moved": siteID != current.SiteID, "connection_changed": connChanged,
		})
	})
	if err != nil {
		return ServerView{}, err
	}
	return s.GetServer(ctx, actor, id)
}

// DeleteServer removes the server and its cameras from the VMS. Frigate is untouched.
func (s *Service) DeleteServer(ctx context.Context, actor authz.Actor, id uuid.UUID) error {
	return s.tx(ctx, actor, func(q *db.Queries, c *access.Checker) error {
		srv, err := q.GetServer(ctx, id)
		if err != nil {
			return notFoundOr(err)
		}
		if err := c.Require(authz.ServersManage, access.Server(srv.TenantID, srv.SiteID, srv.ID)); err != nil {
			return err
		}
		if err := q.SoftDeleteServerCameras(ctx, db.SoftDeleteServerCamerasParams{ServerID: id, DeletedBy: &actor.UserID}); err != nil {
			return err
		}
		if err := q.SoftDeleteServer(ctx, db.SoftDeleteServerParams{ID: id, DeletedBy: &actor.UserID}); err != nil {
			return err
		}
		return audit(ctx, q, actor, &srv.TenantID, ActionServerRemoved, "server", id, map[string]any{"name": srv.Name})
	})
}

type SyncResult struct {
	Server  ServerView
	Added   int
	Updated int
	Missing int
}

// SyncServer rediscovers version, capabilities and cameras. Cameras that disappeared
// from Frigate are flagged, never deleted, so history and permissions survive.
func (s *Service) SyncServer(ctx context.Context, actor authz.Actor, id uuid.UUID) (SyncResult, error) {
	var srv db.GetServerRow
	err := s.tx(ctx, actor, func(q *db.Queries, c *access.Checker) error {
		var err error
		srv, err = q.GetServer(ctx, id)
		if err != nil {
			return notFoundOr(err)
		}
		return c.Require(authz.ServersManage, access.Server(srv.TenantID, srv.SiteID, srv.ID))
	})
	if err != nil {
		return SyncResult{}, err
	}
	info, err := s.connInfo(db.FrigateServer{ID: srv.ID, BaseUrl: srv.BaseUrl, AuthMode: srv.AuthMode, Username: srv.Username, PasswordSealed: srv.PasswordSealed, TlsSkipVerify: srv.TlsSkipVerify})
	if err != nil {
		return SyncResult{}, err
	}
	found, err := s.discover(ctx, info)
	if err != nil {
		return SyncResult{}, err
	}
	var res SyncResult
	err = s.tx(ctx, actor, func(q *db.Queries, c *access.Checker) error {
		if err := c.Require(authz.ServersManage, access.Server(srv.TenantID, srv.SiteID, srv.ID)); err != nil {
			return err
		}
		caps, _ := json.Marshal(found.Capabilities)
		if err := q.UpdateServerDiscovery(ctx, db.UpdateServerDiscoveryParams{ID: id, FrigateVersion: found.Version, Capabilities: caps}); err != nil {
			return err
		}
		present := make([]string, 0, len(found.Cameras))
		for _, cam := range found.Cameras {
			inserted, err := upsertCamera(ctx, q, srv.TenantID, srv.SiteID, id, cam)
			if err != nil {
				return err
			}
			if inserted {
				res.Added++
			} else {
				res.Updated++
			}
			present = append(present, cam.Name)
		}
		missing, err := q.MarkMissingCameras(ctx, db.MarkMissingCamerasParams{ServerID: id, Present: present})
		if err != nil {
			return err
		}
		res.Missing = int(missing)
		return audit(ctx, q, actor, &srv.TenantID, ActionServerSynced, "server", id, map[string]any{
			"frigate_version": found.Version, "added": res.Added, "missing": res.Missing,
		})
	})
	if err != nil {
		return SyncResult{}, err
	}
	res.Server, err = s.GetServer(ctx, actor, id)
	return res, err
}

// ConnInfo decrypts the stored connection of a server. It is exported for the event
// syncer and the media gateway, which run outside a user's request.
func (s *Service) ConnInfo(srv db.FrigateServer) (frigate.ConnInfo, error) {
	return s.connInfo(srv)
}

func (s *Service) connInfo(srv db.FrigateServer) (frigate.ConnInfo, error) {
	info := frigate.ConnInfo{BaseURL: srv.BaseUrl, AuthMode: frigate.AuthMode(srv.AuthMode), TLSSkipVerify: srv.TlsSkipVerify}
	if info.Mode() == frigate.AuthNone {
		info.AuthMode = frigate.AuthNone
		return info, nil
	}
	info.AuthMode = frigate.AuthCredentials
	pw, err := s.Sealer.Open(srv.PasswordSealed, srv.ID[:])
	if err != nil {
		return frigate.ConnInfo{}, err
	}
	info.Username, info.Password = srv.Username, string(pw)
	return info, nil
}

// seal encrypts the password bound to the server id. Servers without credentials store
// nothing.
func (s *Service) seal(info frigate.ConnInfo, id uuid.UUID) ([]byte, error) {
	if info.Mode() == frigate.AuthNone {
		return []byte{}, nil
	}
	return s.Sealer.Seal([]byte(info.Password), id[:])
}

// RestartServer calls Frigate's POST /api/restart endpoint after verifying servers.restart permission.
func (s *Service) RestartServer(ctx context.Context, actor authz.Actor, id uuid.UUID) error {
	var srv db.GetServerRow
	err := s.tx(ctx, actor, func(q *db.Queries, c *access.Checker) error {
		var err error
		srv, err = q.GetServer(ctx, id)
		if err != nil {
			return notFoundOr(err)
		}
		return c.Require(authz.ServersRestart, access.Server(srv.TenantID, srv.SiteID, srv.ID))
	})
	if err != nil {
		return err
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
		return err
	}
	adapter, err := s.Connect(ctx, info)
	if err != nil {
		return &FrigateError{Err: err}
	}
	if err := adapter.Restart(ctx); err != nil {
		return &FrigateError{Err: err}
	}

	return s.tx(ctx, actor, func(q *db.Queries, _ *access.Checker) error {
		return audit(ctx, q, actor, &srv.TenantID, ActionServerRestarted, "server", id, map[string]any{
			"server_id": srv.ID,
			"name":      srv.Name,
		})
	})
}
