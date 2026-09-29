package media

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	_ "image/jpeg" // logo format sniffing only
	_ "image/png"  // logo format sniffing only
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5"

	"github.com/jdolan-exalink/openvms/internal/access"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/branding"
	"github.com/jdolan-exalink/openvms/internal/frigate"
	"github.com/jdolan-exalink/openvms/internal/platform/httpx"
	"github.com/jdolan-exalink/openvms/internal/platform/logging"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
	"github.com/jdolan-exalink/openvms/internal/watermark"
)

// Audit actions for video access (PRD §66).
const (
	ActionLiveViewed     = "LIVE_VIEWED"
	ActionPlaybackViewed = "PLAYBACK_VIEWED"
	ActionExportCreated  = "EXPORT_CREATED"
	ActionExportDownload = "EXPORT_DOWNLOADED"
	ActionSnapshotViewed = "SNAPSHOT_DOWNLOADED"
	// ActionAccessDenied audits a denied (403) media request. The PRD's action catalog
	// names no such action, so this follows its existing PAST_TENSE naming convention.
	ActionAccessDenied = "ACCESS_DENIED"
)

// ActorFunc returns the authenticated actor of a request.
type ActorFunc func(ctx context.Context) (authz.Actor, bool)

// Gateway serves /media/v1. Routes:
//
//	GET /media/v1/cameras/{id}/snapshot.jpg                  latest frame        live.view
//	GET /media/v1/cameras/{id}/live?quality=sub|main         MSE over websocket  live.view
//	GET /media/v1/cameras/{id}/vod/{start}/{end}/{file}      HLS recordings      recordings.view
//	GET /media/v1/events/{id}/snapshot.jpg                   event snapshot       snapshots.view
//	GET /media/v1/lpr/reads/{id}/snapshot.jpg                plate read snapshot  snapshots.view + lpr.view
//	GET /media/v1/lpr/reads/{id}/snapshot.jpg?download=1      + watermarked download  + snapshots.download
//	GET /media/v1/lpr/reads/{id}/clip.mp4                    plate read clip      recordings.view + lpr.view
//	GET /media/v1/exports/{id}/download                      finished export     exports.download
type Gateway struct {
	Svc   *Service
	Actor ActorFunc
	// Branding resolves the owner name/logo burned into a plate detail photo download
	// (PDW-3). Nil disables watermarking (download=1 still 500s with a logged error rather
	// than silently shipping an un-watermarked file).
	Branding *branding.Service
	// AllowedOrigins lists extra origins accepted for websocket upgrades; same-origin
	// requests are always accepted.
	AllowedOrigins []string
	// Live tunes the live websocket (audit window, keepalive, revalidation); zero = defaults.
	Live LiveConfig
	// Session returns the revalidator of the request's credential (api.RevalidatorFrom) so a
	// live socket ends when its session or token is revoked. Nil skips the credential check
	// (the camera permission is still rechecked).
	Session func(ctx context.Context) func(context.Context) (bool, error)

	views viewTracker
}

func (g *Gateway) Routes() http.Handler {
	r := chi.NewRouter()
	r.Get("/cameras/{id}/snapshot.jpg", g.snapshot)
	r.Get("/cameras/{id}/live", g.live)
	r.Get("/cameras/{id}/vod/{start}/{end}/{file}", g.vod)
	r.Get("/events/{id}/snapshot.jpg", g.eventSnapshot)
	r.Get("/lpr/reads/{id}/snapshot.jpg", g.lprReadSnapshot)
	r.Get("/lpr/reads/{id}/clip.mp4", g.lprReadClip)
	r.Get("/exports/{id}/download", g.exportDownload)
	return r
}

func writeErr(w http.ResponseWriter, status int, code, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"code": code, "message": msg})
}

// fail maps service errors like the JSON API does. a is the authenticated actor, used to
// audit a denied (403) request; it is the zero Actor for errors that can never be Forbidden.
func (g *Gateway) fail(w http.ResponseWriter, r *http.Request, a authz.Actor, err error) {
	switch {
	case errors.Is(err, access.ErrForbidden):
		g.auditDenied(r.Context(), a, err, r.Method, r.URL.Path)
		writeErr(w, http.StatusForbidden, "forbidden", "you do not have permission for this camera")
	case errors.Is(err, store.ErrNotFound), errors.Is(err, frigate.ErrNotFound):
		writeErr(w, http.StatusNotFound, "not_found", "not found")
	case errors.Is(err, errBadRequest):
		writeErr(w, http.StatusBadRequest, "bad_request", err.Error())
	case errors.Is(err, errNoStream):
		writeErr(w, http.StatusUnprocessableEntity, "no_stream", "the Frigate server has no go2rtc restream for this camera")
	case errors.Is(err, context.Canceled):
	default:
		g.Svc.Log.WarnContext(r.Context(), "media gateway", "path", r.URL.Path, "error", err)
		writeErr(w, http.StatusBadGateway, "frigate_unreachable", "could not reach the camera's Frigate server")
	}
}

var errBadRequest = errors.New("bad request")

// errNoStream means a camera has no go2rtc restream on its Frigate: discovery
// (frigate.pickStreams) found no go2rtc stream matching this camera, so LiveStream and
// HQStream are both empty. Dialing Frigate anyway would only fail with a generic,
// unhelpful go2rtc error ("stream not found"), so live() fails fast instead.
var errNoStream = errors.New("camera has no go2rtc restream")

func (g *Gateway) actorOr401(w http.ResponseWriter, r *http.Request) (authz.Actor, bool) {
	a, ok := g.Actor(r.Context())
	if !ok {
		writeErr(w, http.StatusUnauthorized, "unauthorized", "authentication required")
	}
	return a, ok
}

func (g *Gateway) camera(w http.ResponseWriter, r *http.Request, p authz.Permission) (authz.Actor, Camera, frigate.Adapter, bool) {
	a, ok := g.actorOr401(w, r)
	if !ok {
		return a, Camera{}, nil, false
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "invalid camera id")
		return a, Camera{}, nil, false
	}
	cam, err := g.Svc.Authorize(r.Context(), a, id, p)
	if err != nil {
		g.fail(w, r, a, err)
		return a, Camera{}, nil, false
	}
	ad, err := g.Svc.Adapters.Get(r.Context(), cam.Server)
	if err != nil {
		g.fail(w, r, a, err)
		return a, Camera{}, nil, false
	}
	return a, cam, ad, true
}

// relay copies a Frigate response to the client with a safe subset of headers.
func relay(w http.ResponseWriter, resp *http.Response, cache string) {
	defer resp.Body.Close()
	for _, k := range []string{"Content-Type", "Content-Length", "Content-Range", "Accept-Ranges", "Last-Modified", "ETag"} {
		if v := resp.Header.Get(k); v != "" {
			w.Header().Set(k, v)
		}
	}
	if cache != "" {
		w.Header().Set("Cache-Control", cache)
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

func (g *Gateway) snapshot(w http.ResponseWriter, r *http.Request) {
	a, cam, ad, ok := g.camera(w, r, authz.LiveView)
	if !ok {
		return
	}
	q := url.Values{}
	if h, err := strconv.Atoi(r.URL.Query().Get("h")); err == nil && h > 0 && h <= 2160 {
		q.Set("h", strconv.Itoa(h))
	}
	resp, err := ad.Media().Open(r.Context(), "/api/"+url.PathEscape(cam.RemoteName)+"/latest.jpg", q, r.Header)
	if err != nil {
		g.fail(w, r, a, err)
		return
	}
	// The Live grid shows this frame as a poster while streams connect; a few seconds of private
	// caching avoids a request per tile on every layout change without serving a stale picture.
	cache := "private, no-store"
	if resp.StatusCode == http.StatusOK {
		cache = "private, max-age=5"
	}
	relay(w, resp, cache)
}

// timeRe accepts unix seconds with optional decimals, as Frigate's /vod paths expect.
var timeRe = regexp.MustCompile(`^\d{9,11}(\.\d{1,6})?$`)

// fileRe is what Frigate's HLS playlists reference: the master playlist, variant
// playlists, init segments and media fragments.
var fileRe = regexp.MustCompile(`^[A-Za-z0-9._-]{1,80}$`)

func (g *Gateway) vod(w http.ResponseWriter, r *http.Request) {
	start, end, file := chi.URLParam(r, "start"), chi.URLParam(r, "end"), chi.URLParam(r, "file")
	if !timeRe.MatchString(start) || !timeRe.MatchString(end) || !fileRe.MatchString(file) || strings.Contains(file, "..") {
		writeErr(w, http.StatusBadRequest, "bad_request", "invalid recording range or file")
		return
	}
	s, _ := strconv.ParseFloat(start, 64)
	e, _ := strconv.ParseFloat(end, 64)
	if e <= s || e-s > 24*3600 {
		writeErr(w, http.StatusBadRequest, "bad_request", "the range must be positive and at most 24 hours")
		return
	}
	a, cam, ad, ok := g.camera(w, r, authz.RecordingsView)
	if !ok {
		return
	}
	if file == "master.m3u8" {
		g.audit(r.Context(), a, cam.TenantID, ActionPlaybackViewed, "camera", cam.ID, map[string]any{
			"start": time.Unix(int64(s), 0).UTC(), "end": time.Unix(int64(e), 0).UTC(),
		})
	}
	path := "/vod/" + url.PathEscape(cam.RemoteName) + "/start/" + start + "/end/" + end + "/" + file
	resp, err := ad.Media().Open(r.Context(), path, nil, r.Header)
	if err != nil {
		g.fail(w, r, a, err)
		return
	}
	cache := "private, max-age=300"
	if strings.HasSuffix(file, ".m3u8") {
		cache = "private, no-cache"
	}
	relay(w, resp, cache)
}

func (g *Gateway) eventSnapshot(w http.ResponseWriter, r *http.Request) {
	a, ok := g.actorOr401(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "invalid event id")
		return
	}
	var cameraID uuid.UUID
	var detections []string
	err = g.Svc.Store.TxRaw(r.Context(), store.ScopeFor(a), func(tx pgx.Tx) error {
		return tx.QueryRow(r.Context(), `SELECT camera_id, detection_ids FROM events WHERE id = $1`, id).Scan(&cameraID, &detections)
	})
	if err != nil {
		g.fail(w, r, a, store.Classify(err))
		return
	}
	cam, err := g.Svc.Authorize(r.Context(), a, cameraID, authz.SnapshotsView)
	if err != nil {
		g.fail(w, r, a, err)
		return
	}
	if len(detections) == 0 {
		g.fail(w, r, a, store.ErrNotFound)
		return
	}
	ad, err := g.Svc.Adapters.Get(r.Context(), cam.Server)
	if err != nil {
		g.fail(w, r, a, err)
		return
	}
	resp, err := ad.Media().Open(r.Context(), "/api/events/"+url.PathEscape(detections[0])+"/snapshot.jpg", url.Values{"bbox": {"1"}}, r.Header)
	if err != nil {
		g.fail(w, r, a, err)
		return
	}
	if r.URL.Query().Get("download") == "1" {
		if _, err := g.Svc.Authorize(r.Context(), a, cameraID, authz.SnapshotsDownload); err != nil {
			resp.Body.Close()
			g.auditDenied(r.Context(), a, err, r.Method, r.URL.Path)
			writeErr(w, http.StatusForbidden, "forbidden", "you cannot download snapshots of this camera")
			return
		}
		w.Header().Set("Content-Disposition", `attachment; filename="snapshot-`+id.String()+`.jpg"`)
		g.audit(r.Context(), a, cam.TenantID, ActionSnapshotViewed, "event", id, nil)
	}
	relay(w, resp, "private, max-age=3600")
}

// lprReadCamera loads the camera and remote Frigate event id behind LPR read id, and
// authorizes p (a snapshot/recording permission) plus lpr.view — the same "both the media
// permission AND lpr.view" rule PDW-2 requires for every plate detail view, since the media
// itself reveals the plate. lpr.view is checked second and separately audited (a distinct
// permission from p) so a caller missing only lpr.view gets a message about it rather than
// the generic camera message.
// lprRead is what handlers need from an lpr_reads row beyond the camera.
type lprRead struct {
	ID            uuid.UUID
	RemoteEventID string
	SeenAt        time.Time
}

func (g *Gateway) lprReadCamera(w http.ResponseWriter, r *http.Request, p authz.Permission) (authz.Actor, Camera, lprRead, bool) {
	a, ok := g.actorOr401(w, r)
	if !ok {
		return a, Camera{}, lprRead{}, false
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "invalid read id")
		return a, Camera{}, lprRead{}, false
	}
	lr := lprRead{ID: id}
	var cameraID uuid.UUID
	err = g.Svc.Store.TxRaw(r.Context(), store.ScopeFor(a), func(tx pgx.Tx) error {
		return tx.QueryRow(r.Context(), `SELECT camera_id, remote_event_id, seen_at FROM lpr_reads WHERE id = $1`, id).
			Scan(&cameraID, &lr.RemoteEventID, &lr.SeenAt)
	})
	if err != nil {
		g.fail(w, r, a, store.Classify(err))
		return a, Camera{}, lprRead{}, false
	}
	cam, err := g.Svc.Authorize(r.Context(), a, cameraID, p)
	if err != nil {
		g.fail(w, r, a, err)
		return a, Camera{}, lprRead{}, false
	}
	if _, err := g.Svc.Authorize(r.Context(), a, cameraID, authz.LPRView); err != nil {
		g.auditDenied(r.Context(), a, err, r.Method, r.URL.Path)
		writeErr(w, http.StatusForbidden, "forbidden", "you do not have permission to view plate reads for this camera")
		return a, Camera{}, lprRead{}, false
	}
	return a, cam, lr, true
}

// lprReadSnapshot serves the Frigate tracked-object snapshot for one LPR read
// (lpr_reads.remote_event_id), full frame with the detection bounding box (bbox=1 unless
// ?bbox=0 is sent) at the highest quality Frigate offers (quality=100, no downscale), so
// the plate photo shown on hover in the Plates page, and at full size in the plate detail
// modal (PDW-2), shows the actual read instead of another object from the same event (which
// the event's own /events/{id}/snapshot.jpg endpoint would, since it always uses
// detections[0]).
func (g *Gateway) lprReadSnapshot(w http.ResponseWriter, r *http.Request) {
	a, cam, lr, ok := g.lprReadCamera(w, r, authz.SnapshotsView)
	if !ok {
		return
	}
	ad, err := g.Svc.Adapters.Get(r.Context(), cam.Server)
	if err != nil {
		g.fail(w, r, a, err)
		return
	}
	q := url.Values{"quality": {"100"}}
	if r.URL.Query().Get("bbox") != "0" {
		q.Set("bbox", "1")
	}
	resp, err := ad.Media().Open(r.Context(), "/api/events/"+url.PathEscape(lr.RemoteEventID)+"/snapshot.jpg", q, r.Header)
	if err != nil {
		g.fail(w, r, a, err)
		return
	}
	if r.URL.Query().Get("download") != "1" {
		relay(w, resp, "private, max-age=3600")
		return
	}
	g.lprReadSnapshotDownload(w, r, a, cam, lr, resp)
}

// lprReadSnapshotDownload finishes a ?download=1 request (PDW-3): it needs
// snapshots.download in addition to what lprReadCamera already checked, burns the watermark
// (date/time + owner branding) into the photo, and audits SNAPSHOT_DOWNLOADED. resp's body
// is always closed by this function.
func (g *Gateway) lprReadSnapshotDownload(w http.ResponseWriter, r *http.Request, a authz.Actor, cam Camera, lr lprRead, resp *http.Response) {
	defer resp.Body.Close()
	if _, err := g.Svc.Authorize(r.Context(), a, cam.ID, authz.SnapshotsDownload); err != nil {
		g.auditDenied(r.Context(), a, err, r.Method, r.URL.Path)
		writeErr(w, http.StatusForbidden, "forbidden", "you cannot download snapshots of this camera")
		return
	}
	if resp.StatusCode != http.StatusOK {
		relay(w, resp, "private, no-store")
		return
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		g.Svc.Log.WarnContext(r.Context(), "read snapshot for watermark", "error", err)
		writeErr(w, http.StatusBadGateway, "frigate_unreachable", "could not read the camera's snapshot")
		return
	}
	out, err := g.burnPhotoWatermark(r.Context(), a, cam.TenantID, lr, body)
	if err != nil {
		g.Svc.Log.WarnContext(r.Context(), "burn photo watermark", "error", err)
		writeErr(w, http.StatusInternalServerError, "internal", "could not prepare the watermarked photo")
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Content-Disposition", `attachment; filename="plate-`+lr.ID.String()+`.jpg"`)
	w.Header().Set("Cache-Control", "private, no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(out) //nolint:gosec // binary image/jpeg body with an explicit Content-Type, not HTML; no XSS surface
	g.audit(r.Context(), a, cam.TenantID, ActionSnapshotViewed, "lpr_read", lr.ID, nil)
}

// burnPhotoWatermark loads tenantID's branding (owner name + optional logo) and burns
// watermark.Text(lr.SeenAt, ownerName) into jpegBytes via watermark.BurnPhoto. A branding
// read/logo-decode failure is non-fatal to the owner name (an empty name still yields a
// correct, just less specific, watermark with the date/time), but a nil Branding service is
// a configuration error, not something to silently degrade.
func (g *Gateway) burnPhotoWatermark(ctx context.Context, a authz.Actor, tenantID uuid.UUID, lr lprRead, jpegBytes []byte) ([]byte, error) {
	if g.Branding == nil {
		return nil, errors.New("media gateway: Branding service not configured")
	}
	b, err := g.Branding.Get(ctx, a, tenantID)
	if err != nil {
		g.Svc.Log.WarnContext(ctx, "load branding for watermark", "error", err)
		b = branding.Branding{}
	}
	var logo image.Image
	if b.HasLogo {
		data, _, err := g.Branding.Logo(ctx, a, tenantID)
		if err != nil {
			g.Svc.Log.WarnContext(ctx, "load branding logo for watermark", "error", err)
		} else if img, _, err := image.Decode(bytes.NewReader(data)); err != nil {
			g.Svc.Log.WarnContext(ctx, "decode branding logo for watermark", "error", err)
		} else {
			logo = img
		}
	}
	return watermark.BurnPhoto(jpegBytes, watermark.Text(lr.SeenAt, b.OwnerName, branding.ResolveLocation(b.Timezone)), logo)
}

// lprReadClip proxies the Frigate tracked-object clip for one LPR read (PDW-2), needing
// recordings.view (in addition to lpr.view, see lprReadCamera) since it exposes recorded
// video, the same permission /media/v1/cameras/{id}/vod requires. The incoming Range header
// travels through r.Header into Open, and relay copies Content-Range/Accept-Ranges back, so
// the browser's <video> element can seek without downloading the whole clip.
func (g *Gateway) lprReadClip(w http.ResponseWriter, r *http.Request) {
	a, cam, lr, ok := g.lprReadCamera(w, r, authz.RecordingsView)
	if !ok {
		return
	}
	ad, err := g.Svc.Adapters.Get(r.Context(), cam.Server)
	if err != nil {
		g.fail(w, r, a, err)
		return
	}
	resp, err := ad.Media().Open(r.Context(), "/api/events/"+url.PathEscape(lr.RemoteEventID)+"/clip.mp4", nil, r.Header)
	if err != nil {
		g.fail(w, r, a, err)
		return
	}
	relay(w, resp, "private, max-age=3600")
}

func (g *Gateway) exportDownload(w http.ResponseWriter, r *http.Request) {
	a, ok := g.actorOr401(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "invalid export id")
		return
	}
	ex, err := g.Svc.GetExport(r.Context(), a, id)
	if err != nil {
		g.fail(w, r, a, err)
		return
	}
	if ex.Status != "ready" || ex.RemotePath == "" {
		writeErr(w, http.StatusConflict, "not_ready", "the export is not ready yet")
		return
	}
	cam, err := g.Svc.Authorize(r.Context(), a, ex.CameraID, authz.ExportsDownload)
	if err != nil {
		g.fail(w, r, a, err)
		return
	}
	ad, err := g.Svc.Adapters.Get(r.Context(), cam.Server)
	if err != nil {
		g.fail(w, r, a, err)
		return
	}
	resp, err := ad.Media().Open(r.Context(), ex.RemotePath, nil, r.Header)
	if err != nil {
		g.fail(w, r, a, err)
		return
	}
	if r.Header.Get("Range") == "" {
		g.audit(r.Context(), a, cam.TenantID, ActionExportDownload, "export", id, map[string]any{"name": ex.Name})
	}
	w.Header().Set("Content-Disposition", `attachment; filename="`+safeFilename(ex.Name)+`.mp4"`)
	relay(w, resp, "private, no-store")
}

var unsafeChars = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func safeFilename(s string) string {
	s = strings.Trim(unsafeChars.ReplaceAllString(s, "_"), "_")
	if s == "" {
		return "export"
	}
	if len(s) > 80 {
		s = s[:80]
	}
	return s
}

func (g *Gateway) audit(ctx context.Context, a authz.Actor, tenantID uuid.UUID, action, targetType string, targetID uuid.UUID, details map[string]any) {
	if err := g.auditTx(ctx, a, tenantID, action, targetType, targetID, details); err != nil {
		g.Svc.Log.ErrorContext(ctx, "media audit", "action", action, "error", err)
	}
}

// auditTx writes one audit row detached from the request's transaction and returns the error.
func (g *Gateway) auditTx(ctx context.Context, a authz.Actor, tenantID uuid.UUID, action, targetType string, targetID uuid.UUID, details map[string]any) error {
	if details == nil {
		details = map[string]any{}
	}
	b, _ := json.Marshal(details)
	return g.Svc.Store.Tx(context.WithoutCancel(ctx), store.ScopeFor(a), func(q *db.Queries) error {
		return q.InsertAudit(ctx, db.InsertAuditParams{
			TenantID: &tenantID, ActorID: &a.UserID, ActorName: a.Username, Action: action,
			TargetType: targetType, TargetID: &targetID, RequestID: logging.RequestID(ctx), Ip: httpx.ClientIP(ctx), Details: b,
		})
	})
}

// auditDenied records one ACCESS_DENIED row (PRD §66) for a request an authenticated actor
// was refused. It runs in a transaction detached from the request's own, the same way audit
// does, so the row survives regardless of the denied operation's outcome. It never fails the
// request: a write failure is only logged. Unauthenticated (401) requests never reach here.
func (g *Gateway) auditDenied(ctx context.Context, a authz.Actor, err error, method, path string) {
	details := map[string]any{"method": method, "path": path}
	tenantID := a.TenantID
	var targetType string
	var targetID *uuid.UUID
	var fe *access.ForbiddenError
	if errors.As(err, &fe) {
		details["permission"] = string(fe.Permission)
		if fe.Resource.Kind != "" {
			targetType = string(fe.Resource.Kind)
		}
		if fe.Resource.ID != uuid.Nil {
			id := fe.Resource.ID
			targetID = &id
		}
		if tenantID == nil && fe.Resource.TenantID != uuid.Nil {
			t := fe.Resource.TenantID
			tenantID = &t
		}
	}
	b, marshalErr := json.Marshal(details)
	if marshalErr != nil {
		g.Svc.Log.ErrorContext(ctx, "access-denied audit", "error", marshalErr)
		return
	}
	writeErr := g.Svc.Store.Tx(context.WithoutCancel(ctx), store.ScopeFor(a), func(q *db.Queries) error {
		return q.InsertAudit(ctx, db.InsertAuditParams{
			TenantID: tenantID, ActorID: &a.UserID, ActorName: a.Username, Action: ActionAccessDenied,
			TargetType: targetType, TargetID: targetID, RequestID: logging.RequestID(ctx), Ip: httpx.ClientIP(ctx), Details: b,
		})
	})
	if writeErr != nil {
		g.Svc.Log.ErrorContext(ctx, "access-denied audit", "error", writeErr)
	}
}

// --- live video --------------------------------------------------------------------

func (g *Gateway) checkOrigin(r *http.Request) bool {
	return httpx.OriginAllowed(r, g.AllowedOrigins)
}

// liveStream picks the go2rtc stream name for a live view request from the camera's
// discovered LiveStream/HQStream (frigate.pickStreams). It never falls back to the
// Frigate camera name: since that discovery already resolved both against go2rtc, a name
// that isn't there would only fail at go2rtc with a generic, unhelpful error.
func liveStream(cam Camera, quality string) (string, error) {
	stream := cam.LiveStream
	if quality == "main" || stream == "" {
		stream = cam.HQStream
	}
	if stream == "" {
		return "", errNoStream
	}
	return stream, nil
}

// liveEnd is why a live session stopped. A non-empty code makes the gateway tell the browser
// with an error frame before closing; an empty code is a plain peer disconnect.
type liveEnd struct{ code, msg string }

// live relays Frigate's go2rtc MSE websocket (/live/mse/api/ws?src=stream). The browser
// speaks the go2rtc protocol end to end; the gateway authorizes, copies frames, keeps both
// legs alive, revalidates the caller while the socket lives, and reports failures to the
// browser as a JSON error frame (see errorFrame) instead of a bare close.
func (g *Gateway) live(w http.ResponseWriter, r *http.Request) {
	if !websocket.IsWebSocketUpgrade(r) {
		// Plain HTTP callers still get the JSON error contract.
		if _, _, _, ok := g.camera(w, r, authz.LiveView); ok {
			writeErr(w, http.StatusBadRequest, "bad_request", "websocket upgrade required")
		}
		return
	}
	cfg := g.Live
	up := websocket.Upgrader{ReadBufferSize: 4 << 10, WriteBufferSize: 64 << 10, CheckOrigin: g.checkOrigin}
	raw, err := up.Upgrade(w, r, nil)
	if err != nil {
		return // Upgrade already answered the client
	}
	client := &wsConn{c: raw, timeout: cfg.writeTimeout()}
	defer raw.Close()

	// reject reports a pre-stream failure to the browser and closes.
	reject := func(code, msg string) {
		g.sendLiveError(client, code, msg)
	}
	a, ok := g.Actor(r.Context())
	if !ok {
		reject(CodeUnauthorized, "authentication required")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		reject(CodeCameraOffline, "invalid camera id")
		return
	}
	cam, err := g.Svc.Authorize(r.Context(), a, id, authz.LiveView)
	if err != nil {
		code, msg := g.liveAuthError(r, a, err)
		if code != "" {
			reject(code, msg)
		}
		return
	}
	ad, err := g.Svc.Adapters.Get(r.Context(), cam.Server)
	if err != nil {
		g.Svc.Log.WarnContext(r.Context(), "live gateway", "camera", id, "error", err)
		reject(CodeUpstreamUnreachable, "could not reach the camera's Frigate server")
		return
	}
	quality := qualityLabel(r.URL.Query().Get("quality"))
	stream, err := liveStream(cam, quality)
	if err != nil {
		reject(CodeCameraOffline, "the camera has no live stream available")
		return
	}
	target, header, tlsCfg, err := ad.Media().WebSocket(r.Context(), "/live/mse/api/ws", url.Values{"src": {stream}})
	if err != nil {
		g.Svc.Log.WarnContext(r.Context(), "live gateway", "camera", id, "error", err)
		reject(CodeUpstreamUnreachable, "could not reach the camera's Frigate server")
		return
	}
	dialer := websocket.Dialer{HandshakeTimeout: 10 * time.Second, TLSClientConfig: tlsCfg, ReadBufferSize: 64 << 10}
	upConn, resp, err := dialer.DialContext(r.Context(), target, header)
	if err != nil {
		rejected := resp != nil // Frigate answered but refused the stream
		if resp != nil {
			resp.Body.Close()
		}
		if r.Context().Err() != nil {
			return
		}
		g.Svc.Log.WarnContext(r.Context(), "live gateway", "camera", id, "error", err)
		if rejected {
			reject(CodeCameraOffline, "the camera stream is not available")
		} else {
			reject(CodeUpstreamUnreachable, "could not reach the camera's Frigate server")
		}
		return
	}
	upstream := &wsConn{c: upConn, timeout: cfg.writeTimeout()}
	defer upConn.Close()

	// One audit row per viewing session: a reconnect within the window is the same session.
	key := viewKey{a.UserID, cam.ID}
	if g.views.begin(key, time.Now(), cfg.auditWindow()) {
		if err := g.auditTx(r.Context(), a, cam.TenantID, ActionLiveViewed, "camera", cam.ID, map[string]any{"stream": stream}); err != nil {
			g.Svc.Log.ErrorContext(r.Context(), "media audit", "action", ActionLiveViewed, "error", err)
			g.views.forget(key) // do not lose a genuinely new view
		}
	} else {
		liveReconnects.Inc()
	}
	liveOpened.WithLabelValues(quality).Inc()
	liveActive.WithLabelValues(quality).Inc()
	defer liveActive.WithLabelValues(quality).Dec()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	done := make(chan liveEnd, 4)
	finish := func(e liveEnd) {
		select {
		case done <- e:
		default:
		}
	}

	pongWait := cfg.pongWait()
	for _, c := range []*websocket.Conn{raw, upConn} {
		c := c
		_ = c.SetReadDeadline(time.Now().Add(pongWait))
		c.SetPongHandler(func(string) error { return c.SetReadDeadline(time.Now().Add(pongWait)) })
	}
	raw.SetReadLimit(clientReadLimit)
	upConn.SetReadLimit(upstreamReadLimit)

	// browser -> Frigate
	go func() {
		for {
			mt, data, err := raw.ReadMessage()
			if err != nil {
				finish(liveEnd{})
				return
			}
			_ = raw.SetReadDeadline(time.Now().Add(pongWait))
			if err := upstream.write(mt, data); err != nil {
				finish(liveEnd{CodeCameraOffline, "lost the connection to the camera stream"})
				return
			}
			liveBytes.WithLabelValues("up").Add(float64(len(data)))
		}
	}()
	// Frigate -> browser. Messages are copied one at a time under a write deadline, so a slow
	// browser stalls (and is then dropped) instead of buffering without bound.
	go func() {
		for {
			mt, data, err := upConn.ReadMessage()
			if err != nil {
				finish(liveEnd{CodeCameraOffline, "the camera stream ended"})
				return
			}
			_ = upConn.SetReadDeadline(time.Now().Add(pongWait))
			if mt == websocket.TextMessage {
				if e, isErr := upstreamError(data); isErr {
					finish(e)
					return
				}
			}
			if err := client.write(mt, data); err != nil {
				finish(liveEnd{})
				return
			}
			liveBytes.WithLabelValues("down").Add(float64(len(data)))
		}
	}()
	// keepalive on both legs
	go func() {
		t := time.NewTicker(cfg.pingEvery())
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				dl := time.Now().Add(cfg.writeTimeout())
				if err := raw.WriteControl(websocket.PingMessage, nil, dl); err != nil {
					finish(liveEnd{})
					return
				}
				if err := upConn.WriteControl(websocket.PingMessage, nil, dl); err != nil {
					finish(liveEnd{CodeCameraOffline, "lost the connection to the camera stream"})
					return
				}
			}
		}
	}()
	// authorization revalidation for the socket's whole life
	go func() {
		var check func(context.Context) (bool, error)
		if g.Session != nil {
			check = g.Session(r.Context())
		}
		if e := g.watchAccess(ctx, r, a, id, check, cfg.revalidateEvery()); e.code != "" {
			finish(e)
		}
	}()

	e := <-done
	cancel()
	if e.code != "" {
		g.sendLiveError(client, e.code, e.msg)
	} else {
		client.closeWith(websocket.CloseNormalClosure, "")
	}
	upstream.closeWith(websocket.CloseNormalClosure, "")
}

// sendLiveError writes the error frame, then a close frame, and counts it.
func (g *Gateway) sendLiveError(c *wsConn, code, msg string) {
	liveErrors.WithLabelValues(code).Inc()
	_ = c.write(websocket.TextMessage, newErrorFrame(code, msg))
	closeCode := websocket.CloseInternalServerErr
	if code == CodeUnauthorized || code == CodeForbidden {
		closeCode = websocket.ClosePolicyViolation
	}
	c.closeWith(closeCode, code)
}

// liveAuthError maps an Authorize failure to an error frame; an empty code means the caller
// went away and nothing should be sent.
func (g *Gateway) liveAuthError(r *http.Request, a authz.Actor, err error) (code, msg string) {
	switch {
	case errors.Is(err, access.ErrForbidden):
		g.auditDenied(r.Context(), a, err, r.Method, r.URL.Path)
		return CodeForbidden, "you do not have permission for this camera"
	case errors.Is(err, store.ErrNotFound), errors.Is(err, frigate.ErrNotFound):
		return CodeCameraOffline, "camera not found"
	case errors.Is(err, context.Canceled):
		return "", ""
	default:
		g.Svc.Log.WarnContext(r.Context(), "live gateway", "path", r.URL.Path, "error", err)
		return CodeServerError, "internal error"
	}
}

// upstreamError recognises go2rtc's own {"type":"error","value":"..."} frame and maps it to
// the gateway's structured codes.
func upstreamError(data []byte) (liveEnd, bool) {
	if len(data) > 4<<10 || !bytes.Contains(data, []byte(`"error"`)) {
		return liveEnd{}, false
	}
	var m struct{ Type, Value string }
	if json.Unmarshal(data, &m) != nil || m.Type != "error" {
		return liveEnd{}, false
	}
	if strings.Contains(strings.ToLower(m.Value), "codec") {
		return liveEnd{CodeCodecUnsupported, "the camera codec is not supported"}, true
	}
	return liveEnd{CodeCameraOffline, "the camera stream is not available"}, true
}

// watchAccess re-checks the caller's credential and camera permission every interval until
// ctx ends and returns why the socket must close. One undecidable check is tolerated; two in
// a row fail closed, like the realtime feed. The camera check goes through Svc.Authorize, so
// a revoked grant takes effect within its cache TTL plus one interval.
func (g *Gateway) watchAccess(ctx context.Context, r *http.Request, a authz.Actor, camID uuid.UUID, check func(context.Context) (bool, error), every time.Duration) liveEnd {
	t := time.NewTicker(every)
	defer t.Stop()
	failures := 0
	for {
		select {
		case <-ctx.Done():
			return liveEnd{}
		case <-t.C:
		}
		if check != nil {
			valid, err := check(ctx)
			if ctx.Err() != nil {
				return liveEnd{}
			}
			switch {
			case err != nil:
				if failures++; failures >= maxRevalidateFailures {
					return liveEnd{CodeServerError, "could not verify the session"}
				}
				continue
			case !valid:
				return liveEnd{CodeUnauthorized, "session ended"}
			}
		}
		_, err := g.Svc.Authorize(ctx, a, camID, authz.LiveView)
		switch {
		case ctx.Err() != nil:
			return liveEnd{}
		case err == nil:
			failures = 0
		case errors.Is(err, access.ErrForbidden):
			g.auditDenied(ctx, a, err, r.Method, r.URL.Path)
			return liveEnd{CodeForbidden, "you no longer have permission for this camera"}
		case errors.Is(err, store.ErrNotFound):
			return liveEnd{CodeForbidden, "camera no longer available"}
		default:
			if failures++; failures >= maxRevalidateFailures {
				return liveEnd{CodeServerError, "could not verify access"}
			}
		}
	}
}
