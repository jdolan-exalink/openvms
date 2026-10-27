package inventory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/access"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/camconfig"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

// ErrRevisionConflict is returned when the caller's expected revision is no longer the
// camera's desired revision: somebody else changed it first.
var ErrRevisionConflict = errors.New("the camera configuration changed since it was read")

// ConfigApplyInput is a request to change a camera's normalized configuration.
type ConfigApplyInput struct {
	Patch camconfig.Patch
	// ExpectedRevision is the desired_revision the caller last read.
	ExpectedRevision int64
	// Origin says where the change comes from (camconfig.Origin*); empty means the UI.
	Origin string
	// RollbackOf is the audit id of the change this apply undoes; 0 for an ordinary change. It
	// is recorded in the audit detail so the history can tell a rollback from an edit.
	RollbackOf int64
}

// ConfigApplyResult is the outcome of one apply.
type ConfigApplyResult struct {
	Review  camconfig.Review
	Outcome camconfig.Outcome
	State   camconfig.State
}

// CameraConfigInventory is what OpenVMS itself knows about a camera, for the model's adapters.
func CameraConfigInventory(c db.GetCameraRow, serverName string) camconfig.Inventory {
	in := camconfig.Inventory{
		CameraID: c.ID.String(), Name: c.DisplayName, RemoteName: c.RemoteName,
		Description: c.Description, Location: c.Location, Tags: c.Tags, Enabled: c.Enabled, LPR: c.Lpr,
		LiveStream: c.LiveStream, HQStream: c.HqStream, LiveCodec: c.LiveCodec, HQCodec: c.HqCodec,
		DefaultLiveQuality: c.DefaultLiveQuality, ServerID: c.ServerID.String(), ServerName: serverName,
		ONVIFCapabilities: c.OnvifCapabilities,
	}
	if c.OnvifEndpoint != nil {
		in.ONVIFEndpoint = *c.OnvifEndpoint
	}
	if c.OnvifProfile != nil {
		in.ONVIFProfile = *c.OnvifProfile
	}
	if c.OnvifVersion != nil {
		in.ONVIFVersion = *c.OnvifVersion
	}
	return in
}

// StoredConfigState converts a stored row (nil: never configured) to the model's type.
func StoredConfigState(row *db.CameraConfigState) camconfig.Stored {
	if row == nil {
		return camconfig.Stored{SyncState: camconfig.SyncSynced}
	}
	var desired camconfig.Patch
	if err := json.Unmarshal(row.Desired, &desired); err != nil {
		// The column only ever holds what Merge produced; a broken document counts as empty
		// so reads keep working.
		desired = camconfig.Patch{}
	}
	return camconfig.Stored{
		Desired: desired, DesiredRevision: row.DesiredRevision, AppliedRevision: row.AppliedRevision,
		SyncState: row.SyncState, LastError: row.LastError,
	}
}

// sectionWriter sends section patches through PatchCameraFrigateConfig, the service behind the
// frigate-config endpoint, so its permission check, validation, revisions and audit apply.
type sectionWriter struct {
	s        *Service
	actor    authz.Actor
	cameraID uuid.UUID
}

func (w sectionWriter) PatchSections(ctx context.Context, sections map[string]any) error {
	// The normalized apply already required the permission of every section it edits
	// (requireSections); one normalized section can write several Frigate sections.
	_, err := w.s.PatchCameraFrigateConfig(withoutFrigateSectionCheck(ctx), w.actor, w.cameraID, sections)
	return err
}

// SectionsForbiddenError is a patch that touches sections the actor may not change. It matches
// access.ErrForbidden; Sections names only the denied sections, never their data.
type SectionsForbiddenError struct{ Sections []string }

func (e *SectionsForbiddenError) Error() string { return "forbidden" }
func (e *SectionsForbiddenError) Unwrap() error { return access.ErrForbidden }

// sectionPermission is the permission each patch section needs, on top of servers.config.
var sectionPermission = map[string]authz.Permission{
	"detection": authz.CamerasConfigEdit, "recording": authz.CamerasConfigRecording,
	"analytics": authz.CamerasConfigAnalytics, "video": authz.CamerasConfigVideo,
}

// requireSections requires the permission of EVERY section the patch touches and reports all
// the denied ones together.
func requireSections(c *access.Checker, cam db.GetCameraRow, patch camconfig.Patch) error {
	var denied []string
	for _, sec := range patch.Sections() {
		if !c.CanCompat(sectionPermission[sec], cameraResource(cam)) {
			denied = append(denied, sec)
		}
	}
	if len(denied) > 0 {
		return &SectionsForbiddenError{Sections: denied}
	}
	return nil
}

type configTarget struct {
	cam db.GetCameraRow
	srv db.GetServerRow
}

// loadConfigTarget loads the camera and its server and requires the permission the Frigate
// config PATCH needs (servers.config on the server) plus one permission per patch section.
func (s *Service) loadConfigTarget(ctx context.Context, id uuid.UUID, q *db.Queries, c *access.Checker, patch camconfig.Patch) (configTarget, error) {
	var t configTarget
	var err error
	if t.cam, err = q.GetCamera(ctx, id); err != nil {
		return t, notFoundOr(err)
	}
	if t.srv, err = q.GetServer(ctx, t.cam.ServerID); err != nil {
		return t, notFoundOr(err)
	}
	if err = c.Require(authz.ServersConfig, access.Server(t.srv.TenantID, t.srv.SiteID, t.srv.ID)); err != nil {
		return t, err
	}
	return t, requireSections(c, t.cam, patch)
}

// configAdapter builds the Frigate adapter of a camera; an unreachable server yields an
// adapter without engine, which reads as "unverifiable" and fails writes.
func (s *Service) configAdapter(ctx context.Context, actor authz.Actor, t configTarget) camconfig.FrigateAdapter {
	ad := camconfig.FrigateAdapter{Writer: sectionWriter{s: s, actor: actor, cameraID: t.cam.ID}}
	if a, err := s.frigateAdapter(ctx, t.srv); err == nil && a != nil {
		ad.Engine = a
	} else if err != nil {
		slog.WarnContext(ctx, "camera config: engine not reachable", "camera_id", t.cam.ID)
	}
	return ad
}

// VideoLinker resolves how a camera's own video (ONVIF) is reached for the actor: the API
// supplies it because it owns the credential lookup and its permission check. An error (such as
// access.ErrForbidden) refuses the request; an unreachable camera is a link with a Reason.
type VideoLinker func(ctx context.Context, cam db.GetCameraRow) (camconfig.VideoLink, error)

// linkVideo connects the adapter to the camera's video when the patch changes it.
func linkVideo(ctx context.Context, ad *camconfig.FrigateAdapter, t configTarget, patch camconfig.Patch, link VideoLinker) error {
	if link == nil || !patch.HasVideo() {
		return nil
	}
	l, err := link(ctx, t.cam)
	if err != nil {
		return err
	}
	ad.Video = &l
	return nil
}

// ReviewCameraConfig describes what a patch would change and affect. It has no side effects:
// nothing is written to the engine or the database.
func (s *Service) ReviewCameraConfig(ctx context.Context, actor authz.Actor, id uuid.UUID, patch camconfig.Patch) (camconfig.Review, error) {
	return s.ReviewCameraConfigWith(ctx, actor, id, patch, nil)
}

// ReviewCameraConfigWith is ReviewCameraConfig with access to the camera's video, needed to
// review stream changes. Reading the device is the only thing it does besides the Frigate read.
func (s *Service) ReviewCameraConfigWith(ctx context.Context, actor authz.Actor, id uuid.UUID, patch camconfig.Patch, link VideoLinker) (camconfig.Review, error) {
	if err := patch.Validate(); err != nil {
		return camconfig.Review{}, invalid("%s", err.Error())
	}
	var t configTarget
	err := s.tx(ctx, actor, func(q *db.Queries, c *access.Checker) error {
		var err error
		t, err = s.loadConfigTarget(ctx, id, q, c, patch.Normalized())
		return err
	})
	if err != nil {
		return camconfig.Review{}, err
	}
	ad := s.configAdapter(ctx, actor, t)
	if err := linkVideo(ctx, &ad, t, patch, link); err != nil {
		return camconfig.Review{}, err
	}
	current, err := ad.Read(ctx, CameraConfigInventory(t.cam, t.srv.Name))
	if err != nil {
		return camconfig.Review{}, err
	}
	return ad.Plan(ctx, current, patch)
}

// ApplyCameraConfig records the desired state, writes it to the engine, reads the camera back
// and stores the verified state. The desired state is recorded first (sync_state pending) so a
// crash never leaves a change that looks synced; the final state and the audit row are written
// together once the outcome is known.
func (s *Service) ApplyCameraConfig(ctx context.Context, actor authz.Actor, id uuid.UUID, in ConfigApplyInput) (ConfigApplyResult, error) {
	return s.ApplyCameraConfigWith(ctx, actor, id, in, nil)
}

// ApplyCameraConfigWith is ApplyCameraConfig with access to the camera's video, needed to apply
// stream changes. A change of the video of a camera that cannot be reached is refused before
// anything is recorded; one the device then rejects ends as a sync_state error like any other
// failed apply.
func (s *Service) ApplyCameraConfigWith(ctx context.Context, actor authz.Actor, id uuid.UUID, in ConfigApplyInput, link VideoLinker) (ConfigApplyResult, error) {
	if err := in.Patch.Validate(); err != nil {
		return ConfigApplyResult{}, invalid("%s", err.Error())
	}
	origin := in.Origin
	if origin == "" {
		origin = camconfig.OriginUI
	}
	if !camconfig.ValidOrigin(origin) {
		return ConfigApplyResult{}, invalid("unknown origin %q", origin)
	}
	patch := in.Patch.Normalized()

	var video *camconfig.VideoLink
	if patch.HasVideo() && link != nil {
		var pre configTarget
		if err := s.tx(ctx, actor, func(q *db.Queries, c *access.Checker) error {
			var err error
			pre, err = s.loadConfigTarget(ctx, id, q, c, patch)
			return err
		}); err != nil {
			return ConfigApplyResult{}, err
		}
		l, err := link(ctx, pre.cam)
		if err != nil {
			return ConfigApplyResult{}, err
		}
		if l.Device == nil {
			return ConfigApplyResult{}, invalid("no se puede cambiar el video de la cámara: %s", camconfig.VideoReasonText(l.Reason))
		}
		video = &l
	}

	var t configTarget
	var rec db.CameraConfigState
	var inherited camconfig.Patch
	err := s.tx(ctx, actor, func(q *db.Queries, c *access.Checker) error {
		var err error
		if t, err = s.loadConfigTarget(ctx, id, q, c, patch); err != nil {
			return err
		}
		if err = q.EnsureCameraConfigState(ctx, db.EnsureCameraConfigStateParams{CameraID: t.cam.ID, TenantID: t.cam.TenantID}); err != nil {
			return err
		}
		cur, err := q.GetCameraConfigState(ctx, t.cam.ID)
		if err != nil {
			return err
		}
		if inherited, err = profilePatchOf(ctx, q, cur.ProfileID); err != nil {
			return err
		}
		// A change that comes from the camera's profile is not an override of the camera.
		next := camconfig.Merge(StoredConfigState(&cur).Desired, patch)
		if origin == camconfig.OriginProfile {
			next = StoredConfigState(&cur).Desired
		}
		merged, err := json.Marshal(next)
		if err != nil {
			return err
		}
		rec, err = q.RecordCameraConfigDesired(ctx, db.RecordCameraConfigDesiredParams{
			CameraID: t.cam.ID, Desired: merged, ExpectedRevision: in.ExpectedRevision, UpdatedBy: &actor.UserID,
		})
		if errors.Is(store.Classify(err), store.ErrNotFound) {
			return ErrRevisionConflict
		}
		return err
	})
	if err != nil {
		return ConfigApplyResult{}, err
	}
	// The camera's effective desired state: its profile overlaid with its own overrides.
	desired := camconfig.Merge(inherited, StoredConfigState(&rec).Desired)

	ad := s.configAdapter(ctx, actor, t)
	ad.Video = video
	invRow := CameraConfigInventory(t.cam, t.srv.Name)
	before, err := ad.Read(ctx, invRow)
	if err != nil {
		return ConfigApplyResult{}, err
	}
	review, _ := ad.Plan(ctx, before, patch)
	applyErr := ad.Apply(ctx, patch)
	after, err := ad.Read(ctx, invRow)
	if err != nil {
		return ConfigApplyResult{}, err
	}
	out := camconfig.DecideOutcome(applyErr, after, patch, desired)

	var fin db.CameraConfigState
	err = s.tx(ctx, actor, func(q *db.Queries, _ *access.Checker) error {
		var err error
		fin, err = q.FinishCameraConfigApply(ctx, db.FinishCameraConfigApplyParams{
			CameraID: t.cam.ID, DesiredRevision: rec.DesiredRevision, SyncState: out.SyncState,
			LastError: out.LastError, Applied: out.Applied,
		})
		if errors.Is(store.Classify(err), store.ErrNotFound) {
			// A newer apply took over the revision; it records its own outcome.
			fin, err = rec, nil
		}
		if err != nil {
			return err
		}
		details := map[string]any{
			"camera_id": t.cam.ID, "camera_name": t.cam.RemoteName, "server_id": t.srv.ID,
			"origin": origin, "result": out.Result, "error": out.LastError, "sync_state": out.SyncState,
			"expected_revision": in.ExpectedRevision, "desired_revision": rec.DesiredRevision,
			"previous_values": changeValues(review.Changes, true), "new_values": changeValues(review.Changes, false),
			"patch": patch,
		}
		if in.RollbackOf != 0 {
			details["rollback_of"] = in.RollbackOf
		}
		if out.Plain.Message != "" {
			details["error_plain"] = out.Plain
		}
		return audit(ctx, q, actor, &t.cam.TenantID, ActionCameraConfigChanged, "camera", t.cam.ID, details)
	})
	if err != nil {
		return ConfigApplyResult{}, fmt.Errorf("camera config: recording the outcome: %w", err)
	}
	// The state reported is the one just recorded, never a recomputed one: a failed apply must
	// not read as synced even if the engine happens to agree with the desired values.
	state := camconfig.State{
		DesiredRevision: fin.DesiredRevision, AppliedRevision: fin.AppliedRevision,
		SyncState: out.SyncState, LastError: out.LastError, Drift: []camconfig.Drift{},
	}
	if camconfig.EngineReadable(after) {
		state.Drift = camconfig.Diff(desired, after)
	}
	return ConfigApplyResult{Review: review, Outcome: out, State: state}, nil
}

// changeValues maps each changed path to its previous (from) or new (to) value.
func changeValues(changes []camconfig.Change, from bool) map[string]any {
	m := make(map[string]any, len(changes))
	for _, c := range changes {
		if from {
			m[c.Path] = c.From
		} else {
			m[c.Path] = c.To
		}
	}
	return m
}
