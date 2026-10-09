package videobackend

import (
	"context"
	"time"
)

// StreamProfile represents streaming quality profiles.
type StreamProfile string

const (
	ProfileMain   StreamProfile = "main"
	ProfileSub    StreamProfile = "sub"
	ProfileMobile StreamProfile = "mobile"
)

// LiveStreamInfo contains the resolved stream URL and transport properties.
type LiveStreamInfo struct {
	StreamURL string        `json:"stream_url"`
	Protocol  string        `json:"protocol"` // webrtc, rtsp, mse, hls
	Codec     string        `json:"codec"`
	Width     int           `json:"width"`
	Height    int           `json:"height"`
	FPS       int           `json:"fps"`
	Profile   StreamProfile `json:"profile"`
}

// RecordingSegment represents a discrete recorded video segment.
type RecordingSegment struct {
	StartTime time.Time `json:"start_time"`
	EndTime   time.Time `json:"end_time"`
	Duration  float64   `json:"duration_seconds"`
	Path      string    `json:"path,omitempty"`
}

// BackendCamera is the normalized camera model returned by any VMS video backend.
type BackendCamera struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Enabled      bool     `json:"enabled"`
	Zones        []string `json:"zones"`
	LPR          bool     `json:"lpr"`
	MainStream   string   `json:"main_stream"`
	SubStream    string   `json:"sub_stream"`
	Capabilities []string `json:"capabilities"`
}

// EventFilter specifies parameters for querying recorded backend events.
type EventFilter struct {
	CameraID string
	Label    string
	Zone     string
	After    time.Time
	Before   time.Time
	Limit    int
}

// BackendEvent represents a normalized detection or alarm event from the backend.
type BackendEvent struct {
	ID          string    `json:"id"`
	CameraID    string    `json:"camera_id"`
	Label       string    `json:"label"`
	StartTime   time.Time `json:"start_time"`
	EndTime     time.Time `json:"end_time"`
	Score       float64   `json:"score"`
	Zones       []string  `json:"zones"`
	HasSnapshot bool      `json:"has_snapshot"`
	HasClip     bool      `json:"has_clip"`
}

// CameraCapabilities specifies camera hardware and analytics features.
type CameraCapabilities struct {
	PTZ             bool `json:"ptz"`
	AudioIn         bool `json:"audio_in"`
	AudioOut        bool `json:"audio_out"`
	LPR             bool `json:"lpr"`
	FaceRecognition bool `json:"face_recognition"`
	WebRTC          bool `json:"webrtc"`
	MSE             bool `json:"mse"`
}

// VideoBackend abstracts disparate NVR/VMS/Edge backends (Frigate, ONVIF, RTSP Gateway, Milestone, etc.).
type VideoBackend interface {
	Name() string
	Version(ctx context.Context) (string, error)
	ListCameras(ctx context.Context) ([]BackendCamera, error)
	GetLiveStream(ctx context.Context, camera string, profile StreamProfile) (LiveStreamInfo, error)
	GetRecording(ctx context.Context, camera string, start, end time.Time) ([]RecordingSegment, error)
	GetEvents(ctx context.Context, filter EventFilter) ([]BackendEvent, error)
	GetSnapshot(ctx context.Context, camera string, height int) ([]byte, string, error)
	GetCameraCapabilities(ctx context.Context, camera string) (CameraCapabilities, error)
}
