package inventory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/access"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/frigate"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

// RevisionServerPatch is the revision kind of a structured server-wide section change.
const RevisionServerPatch = "server_patch"

// faceReadTimeout bounds the live read of one server's face_recognition settings.
const faceReadTimeout = 8 * time.Second

// FaceCameraView is the face recognition state of one camera.
type FaceCameraView struct {
	CameraID uuid.UUID
	Name     string
	// Enabled is the effective value: the camera's own switch, else the global one.
	Enabled         bool
	EnabledOverride *bool
	MinArea         *int
}

// FaceServerConfig is the face recognition configuration of one server. Settings is nil when
// the server could not be read (Error says why in a short, address-free sentence).
type FaceServerConfig struct {
	ServerID       uuid.UUID
	ServerName     string
	SiteName       string
	FrigateVersion string
	CanEdit        bool
	Error          string
	Settings       *frigate.FaceRecognitionSettings
	Cameras        []FaceCameraView
}

// ListFaceConfig reads the running face recognition settings of every server of the tenant the
// actor may see (servers.view), requiring faces.view. A server that cannot be read is reported,
// not fatal. CanEdit says whether the actor may change it (faces.manage and servers.config).
func (s *Service) ListFaceConfig(ctx context.Context, actor authz.Actor, tenantID uuid.UUID) ([]FaceServerConfig, error) {
	var out []FaceServerConfig
	var rows []db.GetServerRow
	err := s.tx(ctx, actor, func(q *db.Queries, c *access.Checker) error {
		if err := c.Require(authz.FacesView, access.Tenant(tenantID)); err != nil {
			return err
		}
		ids, err := c.ServerIDs(ctx, authz.ServersView)
		if err != nil {
			return err
		}
		list, err := q.ListServers(ctx, db.ListServersParams{Ids: ids})
		if err != nil {
			return err
		}
		for _, r := range list {
			if r.TenantID != tenantID {
				continue
			}
			srv := db.GetServerRow(r)
			rows = append(rows, srv)
			cfg := FaceServerConfig{ServerID: srv.ID, ServerName: srv.Name, FrigateVersion: srv.FrigateVersion,
				CanEdit: c.Can(authz.FacesManage, access.Tenant(tenantID)) && c.Can(authz.ServersConfig, access.Server(srv.TenantID, srv.SiteID, srv.ID))}
			if site, err := q.GetSite(ctx, srv.SiteID); err == nil {
				cfg.SiteName = site.Name
			}
			cams, err := q.ListServerCameras(ctx, srv.ID)
			if err != nil {
				return err
			}
			for _, cam := range cams {
				cfg.Cameras = append(cfg.Cameras, FaceCameraView{CameraID: cam.ID, Name: cam.RemoteName})
			}
			sort.Slice(cfg.Cameras, func(i, j int) bool { return cfg.Cameras[i].Name < cfg.Cameras[j].Name })
			out = append(out, cfg)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	var wg sync.WaitGroup
	for i := range out {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.fillFaceSettings(ctx, rows[i], &out[i])
		}()
	}
	wg.Wait()
	sort.SliceStable(out, func(i, j int) bool { return out[i].ServerName < out[j].ServerName })
	return out, nil
}

func (s *Service) fillFaceSettings(ctx context.Context, srv db.GetServerRow, cfg *FaceServerConfig) {
	cctx, cancel := context.WithTimeout(ctx, faceReadTimeout)
	defer cancel()
	a, err := s.frigateAdapter(cctx, srv)
	var st frigate.FaceRecognitionSettings
	if err == nil {
		st, err = a.FaceRecognitionSettings(cctx)
	}
	if err != nil {
		cfg.Error = "the server could not be reached"
		if errors.Is(err, context.DeadlineExceeded) {
			cfg.Error = "the server did not answer in time"
		}
		return
	}
	cfg.Settings = &st
	for i := range cfg.Cameras {
		cs := st.Cameras[cfg.Cameras[i].Name]
		cfg.Cameras[i].EnabledOverride = cs.Enabled
		cfg.Cameras[i].MinArea = cs.MinArea
		cfg.Cameras[i].Enabled = st.Enabled
		if cs.Enabled != nil {
			cfg.Cameras[i].Enabled = *cs.Enabled
		}
	}
}

// FaceCameraPatch changes the per-camera face_recognition block; nil fields stay as they are.
type FaceCameraPatch struct {
	CameraID uuid.UUID
	Enabled  *bool
	MinArea  *int
}

// FaceConfigPatch changes the global face_recognition section and/or camera overrides; nil
// fields stay as they are.
type FaceConfigPatch struct {
	Enabled              *bool
	ModelSize            *string
	DetectionThreshold   *float64
	RecognitionThreshold *float64
	UnknownScore         *float64
	MinFaces             *int
	MinArea              *int
	SaveAttempts         *int
	BlurConfidenceFilter *bool
	Cameras              []FaceCameraPatch
}

// FaceConfigResult lists the revisions written; RestartRequired is true whenever anything
// changed, because Frigate only reads face_recognition at start.
type FaceConfigResult struct {
	RevisionIDs     []uuid.UUID
	RestartRequired bool
}

// Documented Frigate ranges (docs.frigate.video/configuration/face_recognition; bounds from
// the Frigate config models, unverified against 0.18).
const (
	maxFaceMinFaces     = 6
	maxFaceMinArea      = 1_000_000
	maxFaceSaveAttempts = 10_000
)

func unitRange(name string, v *float64) error {
	if v != nil && (math.IsNaN(*v) || *v < 0 || *v > 1) {
		return invalid("%s must be between 0 and 1", name)
	}
	return nil
}

func intRange(name string, v *int, lo, hi int) error {
	if v != nil && (*v < lo || *v > hi) {
		return invalid("%s must be between %d and %d", name, lo, hi)
	}
	return nil
}

// globalSection validates the global fields and returns the face_recognition patch.
func (p FaceConfigPatch) globalSection() (map[string]any, error) {
	if p.ModelSize != nil && *p.ModelSize != "small" && *p.ModelSize != "large" {
		return nil, invalid("model_size must be small or large")
	}
	for _, err := range []error{
		unitRange("detection_threshold", p.DetectionThreshold),
		unitRange("recognition_threshold", p.RecognitionThreshold),
		unitRange("unknown_score", p.UnknownScore),
		intRange("min_faces", p.MinFaces, 1, maxFaceMinFaces),
		intRange("min_area", p.MinArea, 0, maxFaceMinArea),
		intRange("save_attempts", p.SaveAttempts, 0, maxFaceSaveAttempts),
	} {
		if err != nil {
			return nil, err
		}
	}
	out := map[string]any{}
	for k, v := range map[string]any{
		"enabled": p.Enabled, "model_size": p.ModelSize, "detection_threshold": p.DetectionThreshold,
		"recognition_threshold": p.RecognitionThreshold, "unknown_score": p.UnknownScore, "min_faces": p.MinFaces,
		"min_area": p.MinArea, "save_attempts": p.SaveAttempts, "blur_confidence_filter": p.BlurConfidenceFilter,
	} {
		switch x := v.(type) {
		case *bool:
			if x != nil {
				out[k] = *x
			}
		case *string:
			if x != nil {
				out[k] = *x
			}
		case *float64:
			if x != nil {
				out[k] = *x
			}
		case *int:
			if x != nil {
				out[k] = *x
			}
		}
	}
	return out, nil
}

// PatchFaceConfig enables or tunes face recognition on one server through the Frigate config
// editor: the global section is written as a server_patch revision, each camera override
// through PatchCameraFrigateConfig (a camera_patch revision). It requires faces.manage and
// servers.config; nothing is applied live, so the caller must offer a Frigate restart.
func (s *Service) PatchFaceConfig(ctx context.Context, actor authz.Actor, tenantID, serverID uuid.UUID, in FaceConfigPatch) (FaceConfigResult, error) {
	global, err := in.globalSection()
	if err != nil {
		return FaceConfigResult{}, err
	}
	if len(global) == 0 && len(in.Cameras) == 0 {
		return FaceConfigResult{}, invalid("patch is empty")
	}
	seen := map[uuid.UUID]bool{}
	for _, c := range in.Cameras {
		if seen[c.CameraID] {
			return FaceConfigResult{}, invalid("camera listed twice")
		}
		seen[c.CameraID] = true
		if c.Enabled == nil && c.MinArea == nil {
			return FaceConfigResult{}, invalid("a camera entry changes nothing")
		}
		if err := intRange("min_area", c.MinArea, 0, maxFaceMinArea); err != nil {
			return FaceConfigResult{}, err
		}
	}
	var srv db.GetServerRow
	err = s.tx(ctx, actor, func(q *db.Queries, c *access.Checker) error {
		var err error
		if srv, err = q.GetServer(ctx, serverID); err != nil {
			return notFoundOr(err)
		}
		if srv.TenantID != tenantID {
			return store.ErrNotFound
		}
		res := access.Server(srv.TenantID, srv.SiteID, srv.ID)
		if err := c.Require(authz.ServersView, res); err != nil {
			return err
		}
		if err := c.Require(authz.FacesManage, access.Tenant(tenantID)); err != nil {
			return err
		}
		if err := c.Require(authz.ServersConfig, res); err != nil {
			return err
		}
		cams, err := q.ListServerCameras(ctx, serverID)
		if err != nil {
			return err
		}
		own := map[uuid.UUID]bool{}
		for _, cam := range cams {
			own[cam.ID] = true
		}
		for _, cp := range in.Cameras {
			if !own[cp.CameraID] {
				return store.ErrNotFound
			}
		}
		return nil
	})
	if err != nil {
		return FaceConfigResult{}, err
	}

	var out FaceConfigResult
	if len(global) > 0 {
		revID, err := s.patchServerSection(ctx, actor, srv, "face_recognition", global)
		if err != nil {
			return out, err
		}
		out.RevisionIDs = append(out.RevisionIDs, revID)
		out.RestartRequired = true
	}
	for _, cp := range in.Cameras {
		block := map[string]any{}
		if cp.Enabled != nil {
			block["enabled"] = *cp.Enabled
		}
		if cp.MinArea != nil {
			block["min_area"] = *cp.MinArea
		}
		res, err := s.PatchCameraFrigateConfig(ctx, actor, cp.CameraID, map[string]any{"face_recognition": block})
		if res.RevisionID != uuid.Nil {
			out.RevisionIDs = append(out.RevisionIDs, res.RevisionID)
			out.RestartRequired = true
		}
		if err != nil {
			return out, err
		}
	}
	// Best effort: a server that applied the change live (or was restarted by hand) starts
	// syncing as soon as its capability flag flips.
	s.RefreshCapabilities(ctx, srv)
	return out, nil
}

// patchServerSection applies a global section patch and stores its revision and audit row. Like
// the camera patch it fails closed when config.yml cannot be read first (no rollback copy).
func (s *Service) patchServerSection(ctx context.Context, actor authz.Actor, srv db.GetServerRow, section string, value map[string]any) (uuid.UUID, error) {
	a, err := s.frigateAdapter(ctx, srv)
	if err != nil {
		return uuid.Nil, err
	}
	before, err := a.RawConfig(ctx)
	if err != nil {
		return uuid.Nil, &FrigateError{Err: fmt.Errorf("cannot read config.yml to keep a rollback copy, refusing to edit: %w", err)}
	}
	if _, err := a.ApplyServerPatch(ctx, section, value); err != nil {
		return uuid.Nil, mapConfigErr(err)
	}
	after := s.readAfter(ctx, a)
	patchJSON, _ := json.Marshal(map[string]any{section: value})
	var revID uuid.UUID
	err = s.tx(ctx, actor, func(q *db.Queries, _ *access.Checker) error {
		rev, err := q.InsertFrigateConfigRevision(ctx, db.InsertFrigateConfigRevisionParams{
			TenantID: srv.TenantID, ServerID: srv.ID, ActorUserID: &actor.UserID, ActorName: actor.Username,
			Kind: RevisionServerPatch, Sections: []string{section}, BeforeYaml: before, AfterYaml: after, Patch: patchJSON,
		})
		if err != nil {
			return err
		}
		revID = rev.ID
		return audit(ctx, q, actor, &srv.TenantID, ActionFrigateConfigPatched, "server", srv.ID, map[string]any{
			"revision_id": rev.ID, "server_id": srv.ID, "sections": []string{section}, "restart_required": true,
		})
	})
	return revID, err
}

// RefreshCapabilities re-reads a server's capability flags and stores them when they changed. It
// is best effort (a failure is logged): the periodic refresh in the health poller catches up.
func (s *Service) RefreshCapabilities(ctx context.Context, srv db.GetServerRow) {
	if err := s.refreshCapabilities(ctx, srv.ID, func(ctx context.Context) (frigate.Adapter, error) { return s.frigateAdapter(ctx, srv) }); err != nil && s.Log != nil {
		s.Log.WarnContext(ctx, "inventory: capability refresh failed", "server_id", srv.ID, "error", err)
	}
}

func (s *Service) refreshCapabilities(ctx context.Context, serverID uuid.UUID, adapter func(context.Context) (frigate.Adapter, error)) error {
	cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), faceReadTimeout)
	defer cancel()
	a, err := adapter(cctx)
	if err != nil {
		return err
	}
	caps, err := a.Capabilities(cctx)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(caps)
	if err != nil {
		return err
	}
	return s.Store.Tx(cctx, store.AllTenants, func(q *db.Queries) error {
		return q.UpdateServerCapabilities(cctx, db.UpdateServerCapabilitiesParams{ID: serverID, Capabilities: raw})
	})
}
