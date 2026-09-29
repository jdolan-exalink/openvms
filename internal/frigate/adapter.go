package frigate

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// Adapter is the only way the VMS talks to a Frigate server.
type Adapter interface {
	// Name identifies the implementation, e.g. "v017".
	Name() string
	Version() string
	Capabilities(ctx context.Context) (Capabilities, error)
	ListCameras(ctx context.Context) ([]Camera, error)
	Reviews(ctx context.Context, q ReviewQuery) ([]Review, error)
	// TrackedObjects lists Frigate "events" (tracked objects), which carry plate reads.
	TrackedObjects(ctx context.Context, q ObjectQuery) ([]TrackedObject, error)
	// ReviewThumbnail downloads the thumbnail of a review item.
	ReviewThumbnail(ctx context.Context, r Review) ([]byte, string, error)
	Stats(ctx context.Context) (Stats, error)
	// Recordings lists recorded segments of a camera between two unix times.
	Recordings(ctx context.Context, camera string, after, before float64) ([]RecordingSegment, error)
	StartExport(ctx context.Context, camera string, start, end float64, name string) (string, error)
	Export(ctx context.Context, id string) (ExportInfo, error)
	// CameraFrigateConfig reads detection and analytics configuration from Frigate for one camera.
	GetCameraConfig(ctx context.Context, camera string) (CameraFrigateConfig, error)
	// UpdateCameraConfig updates editable configuration properties on Frigate for one camera.
	UpdateCameraConfig(ctx context.Context, camera string, update CameraFrigateConfigUpdate) (CameraFrigateConfig, error)
	// Restart triggers Frigate service restart via POST /api/restart.
	Restart(ctx context.Context) error
	// Media opens raw media resources for the media gateway.
	Media() Media
}

// CameraFrigateConfig holds detection, object tracking and LPR settings for a camera.
type CameraFrigateConfig struct {
	DetectEnabled  bool     `json:"detect_enabled"`
	TrackedObjects []string `json:"tracked_objects"`
	LPREnabled     bool     `json:"lpr_enabled"`
	Zones          []string `json:"zones"`
}

// CameraFrigateConfigUpdate specifies optional fields to update on Frigate.
type CameraFrigateConfigUpdate struct {
	DetectEnabled  *bool    `json:"detect_enabled,omitempty"`
	TrackedObjects []string `json:"tracked_objects,omitempty"`
	LPREnabled     *bool    `json:"lpr_enabled,omitempty"`
}

// Media is the raw access the media gateway needs: plain GETs and websockets on Frigate's
// own paths, authenticated with the server's credentials.
type Media interface {
	Open(ctx context.Context, path string, query url.Values, header http.Header) (*http.Response, error)
	WebSocket(ctx context.Context, path string, query url.Values) (string, http.Header, *tls.Config, error)
}

type Capabilities struct {
	Review          bool `json:"review"`
	Preview         bool `json:"preview"`
	Exports         bool `json:"exports"`
	LPR             bool `json:"lpr"`
	FaceRecognition bool `json:"face_recognition"`
	SemanticSearch  bool `json:"semantic_search"`
	Audio           bool `json:"audio"`
	PTZ             bool `json:"ptz"`
}

type Camera struct {
	Name    string
	Enabled bool
	Zones   []string
	LPR     bool
	// LiveStream is the go2rtc stream for grids (substream); HQStream for single view.
	// Both are empty when the camera has no go2rtc restream.
	LiveStream string
	HQStream   string
}

type ReviewQuery struct {
	Cameras []string
	After   float64
	Before  float64
	Limit   int
}

type ReviewData struct {
	Objects    []string `json:"objects"`
	SubLabels  []string `json:"sub_labels"`
	Zones      []string `json:"zones"`
	Audio      []string `json:"audio"`
	Detections []string `json:"detections"`
}

type Review struct {
	ID              string     `json:"id"`
	Camera          string     `json:"camera"`
	StartTime       float64    `json:"start_time"`
	EndTime         *float64   `json:"end_time"`
	HasBeenReviewed bool       `json:"has_been_reviewed"`
	Severity        string     `json:"severity"`
	ThumbPath       string     `json:"thumb_path"`
	Data            ReviewData `json:"data"`
}

type ObjectQuery struct {
	Cameras []string
	After   float64
	Before  float64
	Limit   int
}

// TrackedObject is a Frigate event (one tracked object).
type TrackedObject struct {
	ID        string   `json:"id"`
	Camera    string   `json:"camera"`
	Label     string   `json:"label"`
	SubLabel  string   `json:"-"`
	Zones     []string `json:"zones"`
	StartTime float64  `json:"start_time"`
	EndTime   *float64 `json:"end_time"`
	Plate     string   `json:"-"`
	// PlateScore is Frigate's confidence for Plate, 0..1.
	PlateScore *float64 `json:"-"`
	TopScore   *float64 `json:"-"`
	// HasSnapshot mirrors Frigate's own Event.has_snapshot (PRD §44 "Has snapshot" filter).
	HasSnapshot bool `json:"-"`
}

// RecordingSegment is a stretch of continuous recording.
type RecordingSegment struct {
	StartTime float64 `json:"start_time"`
	EndTime   float64 `json:"end_time"`
	Motion    int     `json:"motion"`
	Objects   int     `json:"objects"`
}

// ExportInfo is the state of an export job inside Frigate.
type ExportInfo struct {
	ID         string
	Camera     string
	Name       string
	InProgress bool
	Failed     bool
	Error      string
	// VideoPath is the path to download the finished file from (e.g. /exports/x.mp4).
	VideoPath string
	Progress  float64
}

type CameraStats struct {
	CameraFPS    float64
	DetectionFPS float64
}

type Storage struct {
	TotalMB float64 `json:"total_mb"`
	UsedMB  float64 `json:"used_mb"`
	FreeMB  float64 `json:"free_mb"`
}

type Stats struct {
	Version       string
	UptimeSeconds int64
	Cameras       map[string]CameraStats
	// Recordings is the storage backing /media/frigate/recordings, when reported.
	Recordings *Storage
}

// MediaURLPath turns a filesystem path Frigate reports (/media/frigate/clips/x.webp) into
// the URL path it serves the file at (/clips/x.webp).
func MediaURLPath(p string) (string, error) {
	rest, ok := strings.CutPrefix(p, "/media/frigate/")
	if !ok || strings.Contains(rest, "..") {
		return "", fmt.Errorf("unexpected frigate media path %q", p)
	}
	return "/" + rest, nil
}

// Connect logs in, reads the version and returns the adapter for it.
func Connect(ctx context.Context, info ConnInfo) (Adapter, error) {
	c, err := newClient(info)
	if err != nil {
		return nil, err
	}
	raw, err := c.get(ctx, "/api/version", nil)
	if err != nil {
		return nil, err
	}
	version := strings.TrimSpace(string(raw))
	major, minor, ok := parseVersion(version)
	switch {
	case !ok:
		return nil, fmt.Errorf("unrecognised Frigate version %q", version)
	case major == 0 && minor < 14:
		return nil, fmt.Errorf("frigate %s is not supported: review items require 0.14 or later", version)
	case major == 0 && minor >= 18, major > 0:
		return &v018{v017{c: c, version: version}}, nil
	default:
		return &v017{c: c, version: version}, nil
	}
}

// parseVersion reads "0.17.2-abc123" as (0, 17).
func parseVersion(v string) (major, minor int, ok bool) {
	parts := strings.SplitN(strings.SplitN(v, "-", 2)[0], ".", 3)
	if len(parts) < 2 {
		return 0, 0, false
	}
	var err1, err2 error
	major, err1 = strconv.Atoi(parts[0])
	minor, err2 = strconv.Atoi(parts[1])
	return major, minor, err1 == nil && err2 == nil
}
