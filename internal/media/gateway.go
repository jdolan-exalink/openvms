package media

import (
	"context"
	"encoding/json"
	"errors"
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
	"github.com/jdolan-exalink/openvms/internal/frigate"
	"github.com/jdolan-exalink/openvms/internal/platform/httpx"
	"github.com/jdolan-exalink/openvms/internal/platform/logging"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

// Audit actions for video access (PRD §66).
const (
	ActionLiveViewed     = "LIVE_VIEWED"
	ActionPlaybackViewed = "PLAYBACK_VIEWED"
	ActionExportCreated  = "EXPORT_CREATED"
	ActionExportDownload = "EXPORT_DOWNLOADED"
	ActionSnapshotViewed = "SNAPSHOT_DOWNLOADED"
)

// ActorFunc returns the authenticated actor of a request.
type ActorFunc func(ctx context.Context) (authz.Actor, bool)

// Gateway serves /media/v1. Routes:
//
//	GET /media/v1/cameras/{id}/snapshot.jpg                  latest frame        live.view
//	GET /media/v1/cameras/{id}/live?quality=sub|main         MSE over websocket  live.view
//	GET /media/v1/cameras/{id}/vod/{start}/{end}/{file}      HLS recordings      recordings.view
//	GET /media/v1/events/{id}/snapshot.jpg                   event snapshot      snapshots.view
//	GET /media/v1/exports/{id}/download                      finished export     exports.download
type Gateway struct {
	Svc   *Service
	Actor ActorFunc
	// AllowedOrigins lists extra origins accepted for websocket upgrades; same-origin
	// requests are always accepted.
	AllowedOrigins []string
}

func (g *Gateway) Routes() http.Handler {
	r := chi.NewRouter()
	r.Get("/cameras/{id}/snapshot.jpg", g.snapshot)
	r.Get("/cameras/{id}/live", g.live)
	r.Get("/cameras/{id}/vod/{start}/{end}/{file}", g.vod)
	r.Get("/events/{id}/snapshot.jpg", g.eventSnapshot)
	r.Get("/exports/{id}/download", g.exportDownload)
	return r
}

func writeErr(w http.ResponseWriter, status int, code, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"code": code, "message": msg})
}

// fail maps service errors like the JSON API does.
func (g *Gateway) fail(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, access.ErrForbidden):
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
		g.fail(w, r, err)
		return a, Camera{}, nil, false
	}
	ad, err := g.Svc.Adapters.Get(r.Context(), cam.Server)
	if err != nil {
		g.fail(w, r, err)
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
	_, cam, ad, ok := g.camera(w, r, authz.LiveView)
	if !ok {
		return
	}
	q := url.Values{}
	if h, err := strconv.Atoi(r.URL.Query().Get("h")); err == nil && h > 0 && h <= 2160 {
		q.Set("h", strconv.Itoa(h))
	}
	resp, err := ad.Media().Open(r.Context(), "/api/"+url.PathEscape(cam.RemoteName)+"/latest.jpg", q, r.Header)
	if err != nil {
		g.fail(w, r, err)
		return
	}
	relay(w, resp, "private, no-store")
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
		g.fail(w, r, err)
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
		g.fail(w, r, store.Classify(err))
		return
	}
	cam, err := g.Svc.Authorize(r.Context(), a, cameraID, authz.SnapshotsView)
	if err != nil {
		g.fail(w, r, err)
		return
	}
	if len(detections) == 0 {
		g.fail(w, r, store.ErrNotFound)
		return
	}
	ad, err := g.Svc.Adapters.Get(r.Context(), cam.Server)
	if err != nil {
		g.fail(w, r, err)
		return
	}
	resp, err := ad.Media().Open(r.Context(), "/api/events/"+url.PathEscape(detections[0])+"/snapshot.jpg", url.Values{"bbox": {"1"}}, r.Header)
	if err != nil {
		g.fail(w, r, err)
		return
	}
	if r.URL.Query().Get("download") == "1" {
		if !g.can(r.Context(), a, cameraID, authz.SnapshotsDownload) {
			resp.Body.Close()
			writeErr(w, http.StatusForbidden, "forbidden", "you cannot download snapshots of this camera")
			return
		}
		w.Header().Set("Content-Disposition", `attachment; filename="snapshot-`+id.String()+`.jpg"`)
		g.audit(r.Context(), a, cam.TenantID, ActionSnapshotViewed, "event", id, nil)
	}
	relay(w, resp, "private, max-age=3600")
}

func (g *Gateway) can(ctx context.Context, a authz.Actor, cameraID uuid.UUID, p authz.Permission) bool {
	_, err := g.Svc.Authorize(ctx, a, cameraID, p)
	return err == nil
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
		g.fail(w, r, err)
		return
	}
	if ex.Status != "ready" || ex.RemotePath == "" {
		writeErr(w, http.StatusConflict, "not_ready", "the export is not ready yet")
		return
	}
	cam, err := g.Svc.Authorize(r.Context(), a, ex.CameraID, authz.ExportsDownload)
	if err != nil {
		g.fail(w, r, err)
		return
	}
	ad, err := g.Svc.Adapters.Get(r.Context(), cam.Server)
	if err != nil {
		g.fail(w, r, err)
		return
	}
	resp, err := ad.Media().Open(r.Context(), ex.RemotePath, nil, r.Header)
	if err != nil {
		g.fail(w, r, err)
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
	if details == nil {
		details = map[string]any{}
	}
	b, _ := json.Marshal(details)
	err := g.Svc.Store.Tx(context.WithoutCancel(ctx), store.ScopeFor(a), func(q *db.Queries) error {
		return q.InsertAudit(ctx, db.InsertAuditParams{
			TenantID: &tenantID, ActorID: &a.UserID, ActorName: a.Username, Action: action,
			TargetType: targetType, TargetID: &targetID, RequestID: logging.RequestID(ctx), Ip: httpx.ClientIP(ctx), Details: b,
		})
	})
	if err != nil {
		g.Svc.Log.ErrorContext(ctx, "media audit", "action", action, "error", err)
	}
}

// --- live video --------------------------------------------------------------------

func (g *Gateway) checkOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true // not a browser
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	if strings.EqualFold(u.Host, r.Host) {
		return true
	}
	for _, o := range g.AllowedOrigins {
		if strings.EqualFold(o, origin) {
			return true
		}
	}
	return false
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

// live relays Frigate's go2rtc MSE websocket (/live/mse/api/ws?src=stream). The browser
// speaks the go2rtc protocol end to end; the gateway only authorizes and copies frames.
func (g *Gateway) live(w http.ResponseWriter, r *http.Request) {
	a, cam, ad, ok := g.camera(w, r, authz.LiveView)
	if !ok {
		return
	}
	stream, err := liveStream(cam, r.URL.Query().Get("quality"))
	if err != nil {
		g.fail(w, r, err)
		return
	}
	target, header, tlsCfg, err := ad.Media().WebSocket(r.Context(), "/live/mse/api/ws", url.Values{"src": {stream}})
	if err != nil {
		g.fail(w, r, err)
		return
	}
	dialer := websocket.Dialer{HandshakeTimeout: 10 * time.Second, TLSClientConfig: tlsCfg, ReadBufferSize: 64 << 10}
	upstream, resp, err := dialer.DialContext(r.Context(), target, header)
	if err != nil {
		if resp != nil {
			resp.Body.Close()
		}
		g.fail(w, r, err)
		return
	}
	defer upstream.Close()

	up := websocket.Upgrader{ReadBufferSize: 4 << 10, WriteBufferSize: 64 << 10, CheckOrigin: g.checkOrigin}
	client, err := up.Upgrade(w, r, nil)
	if err != nil {
		return // Upgrade already answered the client
	}
	defer client.Close()
	g.audit(r.Context(), a, cam.TenantID, ActionLiveViewed, "camera", cam.ID, map[string]any{"stream": stream})

	done := make(chan struct{}, 2)
	pipe := func(dst, src *websocket.Conn) {
		defer func() { done <- struct{}{} }()
		for {
			mt, data, err := src.ReadMessage()
			if err != nil {
				return
			}
			_ = dst.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := dst.WriteMessage(mt, data); err != nil {
				return
			}
		}
	}
	go pipe(upstream, client)
	go pipe(client, upstream)
	<-done
}
