package inventory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/access"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/frigate"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

// Revision kinds stored in frigate_config_revisions.kind.
const (
	RevisionCameraPatch = "camera_patch"
	RevisionRawSave     = "raw_save"
	RevisionRollback    = "rollback"
)

// FrigateCameraConfigDoc is the full effective configuration of one camera.
type FrigateCameraConfigDoc struct {
	CameraID       uuid.UUID
	CameraName     string
	ServerID       uuid.UUID
	FrigateVersion string
	// Editable is true when the server version allows edits and the actor has servers.config.
	Editable bool
	// SecretsVisible is true when Config carries unmasked credentials.
	SecretsVisible bool
	Config         map[string]any
}

// FrigatePatchResult is the outcome of a camera patch.
type FrigatePatchResult struct {
	RevisionID      uuid.UUID
	Sections        []frigate.SectionResult
	RestartRequired bool
}

// FrigateRawSaveResult is the outcome of a raw save or a rollback.
type FrigateRawSaveResult struct {
	RevisionID      uuid.UUID
	RestartRequired bool
}

// FrigateConfigRevision is one stored configuration change. BeforeYAML / AfterYAML are nil
// unless the actor holds servers.config.secrets, and Patch is always redacted for actors
// without it.
type FrigateConfigRevision struct {
	ID          uuid.UUID
	ServerID    uuid.UUID
	CameraID    *uuid.UUID
	ActorUserID *uuid.UUID
	ActorName   string
	Kind        string
	Sections    []string
	Patch       map[string]any
	BeforeYAML  *string
	AfterYAML   *string
	CreatedAt   time.Time
}

func serverRow(srv db.GetServerRow) db.FrigateServer {
	return db.FrigateServer{
		ID: srv.ID, BaseUrl: srv.BaseUrl, AuthMode: srv.AuthMode, Username: srv.Username,
		PasswordSealed: srv.PasswordSealed, TlsSkipVerify: srv.TlsSkipVerify,
	}
}

func (s *Service) frigateAdapter(ctx context.Context, srv db.GetServerRow) (frigate.Adapter, error) {
	info, err := s.connInfo(serverRow(srv))
	if err != nil {
		return nil, err
	}
	a, err := s.Connect(ctx, info)
	if err != nil {
		return nil, &FrigateError{Err: err}
	}
	return a, nil
}

// mapConfigErr turns adapter errors of the config API into service errors.
func mapConfigErr(err error) error {
	var cv *frigate.ConfigValidationError
	switch {
	case err == nil:
		return nil
	case errors.As(err, &cv):
		return invalid("%s", cv.Message)
	case errors.Is(err, frigate.ErrConfigEditUnsupported), errors.Is(err, frigate.ErrNotFound):
		return err
	}
	return &FrigateError{Err: err}
}

// GetCameraFrigateConfigFull reads the full effective camera section from Frigate.
func (s *Service) GetCameraFrigateConfigFull(ctx context.Context, actor authz.Actor, cameraID uuid.UUID) (FrigateCameraConfigDoc, error) {
	var cam db.GetCameraRow
	var srv db.GetServerRow
	var canConfig, canSecrets bool
	err := s.tx(ctx, actor, func(q *db.Queries, c *access.Checker) error {
		var err error
		if cam, err = q.GetCamera(ctx, cameraID); err != nil {
			return notFoundOr(err)
		}
		if err := c.Require(authz.CamerasView, cameraResource(cam)); err != nil {
			return err
		}
		if srv, err = q.GetServer(ctx, cam.ServerID); err != nil {
			return notFoundOr(err)
		}
		res := access.Server(srv.TenantID, srv.SiteID, srv.ID)
		canConfig = c.Can(authz.ServersConfig, res)
		canSecrets = c.Can(authz.ServersConfigSecrets, res)
		return nil
	})
	if err != nil {
		return FrigateCameraConfigDoc{}, err
	}
	a, err := s.frigateAdapter(ctx, srv)
	if err != nil {
		return FrigateCameraConfigDoc{}, err
	}
	doc := FrigateCameraConfigDoc{
		CameraID: cam.ID, CameraName: cam.RemoteName, ServerID: cam.ServerID,
		FrigateVersion: a.Version(), Editable: canConfig && a.ConfigEditable(),
	}
	if canSecrets {
		cfg, err := a.CameraConfig(ctx, cam.RemoteName, true)
		if err == nil {
			doc.Config, doc.SecretsVisible = cfg, true
			return doc, nil
		}
		// Frigate's admin-only endpoints may be unavailable to the service account: serve the
		// masked view instead of failing the whole read.
		slog.WarnContext(ctx, "frigate: secrets view unavailable, serving masked config", "error", err)
	}
	cfg, err := a.CameraConfig(ctx, cam.RemoteName, false)
	if err != nil {
		return FrigateCameraConfigDoc{}, &FrigateError{Err: err}
	}
	doc.Config = cfg
	return doc, nil
}

// GetServerFrigateSchema returns Frigate's configuration schema trimmed to CameraConfig.
func (s *Service) GetServerFrigateSchema(ctx context.Context, actor authz.Actor, serverID uuid.UUID) (json.RawMessage, error) {
	var srv db.GetServerRow
	err := s.tx(ctx, actor, func(q *db.Queries, c *access.Checker) error {
		var err error
		if srv, err = q.GetServer(ctx, serverID); err != nil {
			return notFoundOr(err)
		}
		return c.Require(authz.ServersView, access.Server(srv.TenantID, srv.SiteID, srv.ID))
	})
	if err != nil {
		return nil, err
	}
	a, err := s.frigateAdapter(ctx, srv)
	if err != nil {
		return nil, err
	}
	full, err := a.ConfigSchema(ctx)
	if err != nil {
		return nil, &FrigateError{Err: err}
	}
	out, err := frigate.TrimCameraSchema(full)
	if err != nil {
		return nil, &FrigateError{Err: err}
	}
	return out, nil
}

// PatchCameraFrigateConfig applies a patch (top-level section -> value) to one camera. It
// stores a revision (full config.yml before and after) and an audit row. The write fails
// closed when config.yml cannot be read first, because then it could not be rolled back.
func (s *Service) PatchCameraFrigateConfig(ctx context.Context, actor authz.Actor, cameraID uuid.UUID, patch map[string]any) (FrigatePatchResult, error) {
	var cam db.GetCameraRow
	var srv db.GetServerRow
	err := s.tx(ctx, actor, func(q *db.Queries, c *access.Checker) error {
		var err error
		if cam, err = q.GetCamera(ctx, cameraID); err != nil {
			return notFoundOr(err)
		}
		if srv, err = q.GetServer(ctx, cam.ServerID); err != nil {
			return notFoundOr(err)
		}
		res := access.Server(srv.TenantID, srv.SiteID, srv.ID)
		if err := c.Require(authz.ServersConfig, res); err != nil {
			return err
		}
		if frigate.TouchesSecrets(patch) {
			return c.Require(authz.ServersConfigSecrets, res)
		}
		return nil
	})
	if err != nil {
		return FrigatePatchResult{}, err
	}
	if len(patch) == 0 {
		return FrigatePatchResult{}, invalid("patch is empty")
	}
	if p, bad := frigate.FindRedacted(patch); bad {
		return FrigatePatchResult{}, invalid("%s holds a redacted value; redacted values are never written back", p)
	}
	a, err := s.frigateAdapter(ctx, srv)
	if err != nil {
		return FrigatePatchResult{}, err
	}
	if !a.ConfigEditable() {
		return FrigatePatchResult{}, frigate.ErrConfigEditUnsupported
	}
	before, err := a.RawConfig(ctx)
	if err != nil {
		return FrigatePatchResult{}, &FrigateError{Err: fmt.Errorf("cannot read config.yml to keep a rollback copy, refusing to edit: %w", err)}
	}
	results, applyErr := a.ApplyCameraPatch(ctx, cam.RemoteName, patch)
	if len(results) == 0 {
		return FrigatePatchResult{}, mapConfigErr(applyErr)
	}
	after := s.readAfter(ctx, a)
	sections := make([]string, 0, len(results))
	restart := false
	for _, r := range results {
		sections = append(sections, r.Section)
		restart = restart || r.RequiresRestart
	}
	patchJSON, _ := json.Marshal(patch)
	var revID uuid.UUID
	err = s.tx(ctx, actor, func(q *db.Queries, _ *access.Checker) error {
		rev, err := q.InsertFrigateConfigRevision(ctx, db.InsertFrigateConfigRevisionParams{
			TenantID: srv.TenantID, ServerID: srv.ID, CameraID: &cam.ID, ActorUserID: &actor.UserID, ActorName: actor.Username,
			Kind: RevisionCameraPatch, Sections: sections, BeforeYaml: before, AfterYaml: after, Patch: patchJSON,
		})
		if err != nil {
			return err
		}
		revID = rev.ID
		return audit(ctx, q, actor, &srv.TenantID, ActionFrigateConfigPatched, "server", srv.ID, map[string]any{
			"revision_id": rev.ID, "camera_id": cam.ID, "camera_name": cam.RemoteName, "server_id": srv.ID,
			"sections": sections, "restart_required": restart,
		})
	})
	if err != nil {
		return FrigatePatchResult{}, err
	}
	if applyErr != nil {
		// Some sections were applied and recorded; report the failure of the rest.
		return FrigatePatchResult{RevisionID: revID, Sections: results, RestartRequired: restart}, mapConfigErr(applyErr)
	}
	return FrigatePatchResult{RevisionID: revID, Sections: results, RestartRequired: restart}, nil
}

func (s *Service) readAfter(ctx context.Context, a frigate.Adapter) string {
	after, err := a.RawConfig(ctx)
	if err != nil {
		slog.WarnContext(ctx, "frigate: could not read config.yml after the change", "error", err)
		return ""
	}
	return after
}

// GetServerFrigateRaw returns config.yml with secrets. Requires servers.config.secrets.
func (s *Service) GetServerFrigateRaw(ctx context.Context, actor authz.Actor, serverID uuid.UUID) (string, error) {
	srv, err := s.secretsServer(ctx, actor, serverID, false)
	if err != nil {
		return "", err
	}
	a, err := s.frigateAdapter(ctx, srv)
	if err != nil {
		return "", err
	}
	text, err := a.RawConfig(ctx)
	if err != nil {
		return "", &FrigateError{Err: err}
	}
	return text, nil
}

// secretsServer loads the server and requires servers.config.secrets (and servers.config for
// writes).
func (s *Service) secretsServer(ctx context.Context, actor authz.Actor, serverID uuid.UUID, write bool) (db.GetServerRow, error) {
	var srv db.GetServerRow
	err := s.tx(ctx, actor, func(q *db.Queries, c *access.Checker) error {
		var err error
		if srv, err = q.GetServer(ctx, serverID); err != nil {
			return notFoundOr(err)
		}
		res := access.Server(srv.TenantID, srv.SiteID, srv.ID)
		if write {
			if err := c.Require(authz.ServersConfig, res); err != nil {
				return err
			}
		}
		return c.Require(authz.ServersConfigSecrets, res)
	})
	return srv, err
}

// SaveServerFrigateRaw validates (through Frigate) and writes a full config.yml, storing a
// revision. Requires servers.config and servers.config.secrets.
func (s *Service) SaveServerFrigateRaw(ctx context.Context, actor authz.Actor, serverID uuid.UUID, yamlText string, restart bool) (FrigateRawSaveResult, error) {
	srv, err := s.secretsServer(ctx, actor, serverID, true)
	if err != nil {
		return FrigateRawSaveResult{}, err
	}
	if yamlText == "" {
		return FrigateRawSaveResult{}, invalid("config is empty")
	}
	a, err := s.frigateAdapter(ctx, srv)
	if err != nil {
		return FrigateRawSaveResult{}, err
	}
	if !a.ConfigEditable() {
		return FrigateRawSaveResult{}, frigate.ErrConfigEditUnsupported
	}
	return s.writeRaw(ctx, actor, srv, a, yamlText, restart, RevisionRawSave, ActionFrigateConfigRawSaved, nil)
}

// writeRaw is the shared save path of raw saves and rollbacks.
func (s *Service) writeRaw(ctx context.Context, actor authz.Actor, srv db.GetServerRow, a frigate.Adapter, yamlText string, restart bool, kind, action string, extra map[string]any) (FrigateRawSaveResult, error) {
	before, err := a.RawConfig(ctx)
	if err != nil {
		return FrigateRawSaveResult{}, &FrigateError{Err: fmt.Errorf("cannot read config.yml to keep a rollback copy, refusing to edit: %w", err)}
	}
	if err := a.SaveRawConfig(ctx, yamlText, restart); err != nil {
		return FrigateRawSaveResult{}, mapConfigErr(err)
	}
	after := s.readAfter(ctx, a)
	var revID uuid.UUID
	err = s.tx(ctx, actor, func(q *db.Queries, _ *access.Checker) error {
		rev, err := q.InsertFrigateConfigRevision(ctx, db.InsertFrigateConfigRevisionParams{
			TenantID: srv.TenantID, ServerID: srv.ID, ActorUserID: &actor.UserID, ActorName: actor.Username,
			Kind: kind, Sections: []string{"raw"}, BeforeYaml: before, AfterYaml: after, Patch: json.RawMessage("{}"),
		})
		if err != nil {
			return err
		}
		revID = rev.ID
		details := map[string]any{"revision_id": rev.ID, "server_id": srv.ID, "sections": []string{"raw"}, "restart_required": !restart}
		for k, v := range extra {
			details[k] = v
		}
		return audit(ctx, q, actor, &srv.TenantID, action, "server", srv.ID, details)
	})
	if err != nil {
		return FrigateRawSaveResult{}, err
	}
	// A saved file is only read by Frigate on start, unless this save restarted it.
	return FrigateRawSaveResult{RevisionID: revID, RestartRequired: !restart}, nil
}

// RollbackServerFrigateConfig re-applies a revision's before_yaml. It is a raw write, so it
// needs servers.config and servers.config.secrets, and always needs a Frigate restart.
func (s *Service) RollbackServerFrigateConfig(ctx context.Context, actor authz.Actor, serverID, revisionID uuid.UUID) (FrigateRawSaveResult, error) {
	srv, err := s.secretsServer(ctx, actor, serverID, true)
	if err != nil {
		return FrigateRawSaveResult{}, err
	}
	var rev db.FrigateConfigRevision
	err = s.tx(ctx, actor, func(q *db.Queries, _ *access.Checker) error {
		var err error
		rev, err = q.GetFrigateConfigRevision(ctx, db.GetFrigateConfigRevisionParams{ID: revisionID, ServerID: serverID})
		return notFoundOr(err)
	})
	if err != nil {
		return FrigateRawSaveResult{}, err
	}
	a, err := s.frigateAdapter(ctx, srv)
	if err != nil {
		return FrigateRawSaveResult{}, err
	}
	if !a.ConfigEditable() {
		return FrigateRawSaveResult{}, frigate.ErrConfigEditUnsupported
	}
	return s.writeRaw(ctx, actor, srv, a, rev.BeforeYaml, false, RevisionRollback, ActionFrigateConfigRolledBack,
		map[string]any{"rolled_back_revision_id": rev.ID})
}

// ListFrigateConfigRevisions pages a server's revisions, newest first. Secrets (YAML and
// credentials inside patches) are only included for actors with servers.config.secrets.
func (s *Service) ListFrigateConfigRevisions(ctx context.Context, actor authz.Actor, serverID uuid.UUID, cameraID *uuid.UUID, before *time.Time, limit int, includeYAML bool) ([]FrigateConfigRevision, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	maxRows := int32(limit) //nolint:gosec // bounded to 1..200 above
	var out []FrigateConfigRevision
	err := s.tx(ctx, actor, func(q *db.Queries, c *access.Checker) error {
		srv, err := q.GetServer(ctx, serverID)
		if err != nil {
			return notFoundOr(err)
		}
		res := access.Server(srv.TenantID, srv.SiteID, srv.ID)
		if err := c.Require(authz.ServersView, res); err != nil {
			return err
		}
		secrets := c.Can(authz.ServersConfigSecrets, res)
		rows, err := q.ListFrigateConfigRevisions(ctx, db.ListFrigateConfigRevisionsParams{
			ServerID: serverID, CameraID: cameraID, Before: before, MaxRows: maxRows,
		})
		if err != nil {
			return err
		}
		for _, r := range rows {
			rev := FrigateConfigRevision{
				ID: r.ID, ServerID: r.ServerID, CameraID: r.CameraID, ActorUserID: r.ActorUserID, ActorName: r.ActorName,
				Kind: r.Kind, Sections: r.Sections, CreatedAt: r.CreatedAt,
			}
			var patch map[string]any
			_ = json.Unmarshal(r.Patch, &patch)
			if !secrets {
				redactPatch(patch)
			}
			rev.Patch = patch
			if secrets && includeYAML {
				full, err := q.GetFrigateConfigRevision(ctx, db.GetFrigateConfigRevisionParams{ID: r.ID, ServerID: serverID})
				if err != nil {
					return err
				}
				rev.BeforeYAML, rev.AfterYAML = &full.BeforeYaml, &full.AfterYaml
			}
			out = append(out, rev)
		}
		return nil
	})
	return out, err
}

var urlCredentials = regexp.MustCompile(`://[^/@\s]*@`)

// redactPatch masks credentials inside a stored patch in place: URL user:pass@ parts and
// the ONVIF user/password.
func redactPatch(v any) {
	switch t := v.(type) {
	case map[string]any:
		if on, ok := t["onvif"].(map[string]any); ok {
			for _, k := range []string{"user", "password"} {
				if _, has := on[k]; has {
					on[k] = "*"
				}
			}
		}
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if str, ok := t[k].(string); ok {
				t[k] = urlCredentials.ReplaceAllString(str, "://*:*@")
				continue
			}
			redactPatch(t[k])
		}
	case []any:
		for i, e := range t {
			if str, ok := e.(string); ok {
				t[i] = urlCredentials.ReplaceAllString(str, "://*:*@")
				continue
			}
			redactPatch(e)
		}
	}
}
