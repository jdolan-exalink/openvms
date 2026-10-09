package videobackend

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jdolan-exalink/openvms/internal/frigate"
)

// FrigateBackend adapts a Frigate NVR adapter to the VideoBackend interface.
type FrigateBackend struct {
	adapter frigate.Adapter
	baseURL string
}

// NewFrigateBackend wraps a frigate.Adapter with an optional base URL.
func NewFrigateBackend(adapter frigate.Adapter, baseURL string) *FrigateBackend {
	return &FrigateBackend{
		adapter: adapter,
		baseURL: strings.TrimRight(baseURL, "/"),
	}
}

func (b *FrigateBackend) Name() string {
	if b.adapter == nil {
		return "frigate"
	}
	return "frigate-" + b.adapter.Name()
}

func (b *FrigateBackend) Version(ctx context.Context) (string, error) {
	if b.adapter == nil {
		return "", fmt.Errorf("frigate adapter not initialized")
	}
	return b.adapter.Version(), nil
}

func (b *FrigateBackend) ListCameras(ctx context.Context) ([]BackendCamera, error) {
	if b.adapter == nil {
		return nil, fmt.Errorf("frigate adapter not initialized")
	}

	cams, err := b.adapter.ListCameras(ctx)
	if err != nil {
		return nil, err
	}

	out := make([]BackendCamera, 0, len(cams))
	for _, c := range cams {
		caps := []string{"live", "snapshots", "recordings"}
		if c.LPR {
			caps = append(caps, "lpr")
		}
		if len(c.Zones) > 0 {
			caps = append(caps, "zones")
		}

		out = append(out, BackendCamera{
			ID:           c.Name,
			Name:         c.Name,
			Enabled:      c.Enabled,
			Zones:        c.Zones,
			LPR:          c.LPR,
			MainStream:   c.HQStream,
			SubStream:    c.LiveStream,
			Capabilities: caps,
		})
	}

	return out, nil
}

func (b *FrigateBackend) GetLiveStream(ctx context.Context, camera string, profile StreamProfile) (LiveStreamInfo, error) {
	if camera == "" {
		return LiveStreamInfo{}, fmt.Errorf("camera name is required")
	}

	streamName := camera
	width, height, fps := 1920, 1080, 20
	if profile == ProfileSub || profile == ProfileMobile {
		width, height, fps = 640, 360, 15
	}

	streamURL := fmt.Sprintf("%s/live/webrtc/api/ws?src=%s", b.baseURL, url.QueryEscape(streamName))
	if b.baseURL == "" {
		streamURL = fmt.Sprintf("/live/webrtc/api/ws?src=%s", url.QueryEscape(streamName))
	}

	return LiveStreamInfo{
		StreamURL: streamURL,
		Protocol:  "webrtc",
		Codec:     "h264",
		Width:     width,
		Height:    height,
		FPS:       fps,
		Profile:   profile,
	}, nil
}

func (b *FrigateBackend) GetRecording(ctx context.Context, camera string, start, end time.Time) ([]RecordingSegment, error) {
	if b.adapter == nil {
		return nil, fmt.Errorf("frigate adapter not initialized")
	}

	after := float64(start.Unix())
	before := float64(end.Unix())
	segs, err := b.adapter.Recordings(ctx, camera, after, before)
	if err != nil {
		return nil, err
	}

	out := make([]RecordingSegment, 0, len(segs))
	for _, s := range segs {
		st := time.Unix(int64(s.StartTime), 0)
		et := time.Unix(int64(s.EndTime), 0)
		out = append(out, RecordingSegment{
			StartTime: st,
			EndTime:   et,
			Duration:  s.EndTime - s.StartTime,
		})
	}

	return out, nil
}

func (b *FrigateBackend) GetEvents(ctx context.Context, filter EventFilter) ([]BackendEvent, error) {
	if b.adapter == nil {
		return nil, fmt.Errorf("frigate adapter not initialized")
	}

	var after, before float64
	if !filter.After.IsZero() {
		after = float64(filter.After.Unix())
	}
	if !filter.Before.IsZero() {
		before = float64(filter.Before.Unix())
	}

	q := frigate.ReviewQuery{
		After:  after,
		Before: before,
	}
	if filter.CameraID != "" {
		q.Cameras = []string{filter.CameraID}
	}

	reviews, err := b.adapter.Reviews(ctx, q)
	if err != nil {
		return nil, err
	}

	out := make([]BackendEvent, 0, len(reviews))
	for _, r := range reviews {
		var topScore float64 = 0.85
		var label string
		if len(r.Data.Objects) > 0 {
			label = r.Data.Objects[0]
		}

		startTime := time.Unix(int64(r.StartTime), 0)
		endTime := startTime
		if r.EndTime != nil {
			endTime = time.Unix(int64(*r.EndTime), 0)
		}

		out = append(out, BackendEvent{
			ID:          r.ID,
			CameraID:    r.Camera,
			Label:       label,
			StartTime:   startTime,
			EndTime:     endTime,
			Score:       topScore,
			Zones:       r.Data.Zones,
			HasSnapshot: true,
			HasClip:     true,
		})

		if filter.Limit > 0 && len(out) >= filter.Limit {
			break
		}
	}

	return out, nil
}

func (b *FrigateBackend) GetSnapshot(ctx context.Context, camera string, height int) ([]byte, string, error) {
	if b.adapter == nil {
		return nil, "", fmt.Errorf("frigate adapter not initialized")
	}

	media := b.adapter.Media()
	if media == nil {
		return nil, "", fmt.Errorf("media gateway unavailable on frigate adapter")
	}

	query := url.Values{}
	if height > 0 {
		query.Set("h", strconv.Itoa(height))
	}

	path := fmt.Sprintf("/api/%s/latest.jpg", camera)
	resp, err := media.Open(ctx, path, query, nil)
	if err != nil {
		return nil, "", fmt.Errorf("failed to open snapshot: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, "", fmt.Errorf("frigate returned status %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("failed to read snapshot body: %w", err)
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "image/jpeg"
	}

	return data, contentType, nil
}

func (b *FrigateBackend) GetCameraCapabilities(ctx context.Context, camera string) (CameraCapabilities, error) {
	if b.adapter == nil {
		return CameraCapabilities{}, fmt.Errorf("frigate adapter not initialized")
	}

	caps, err := b.adapter.Capabilities(ctx)
	if err != nil {
		return CameraCapabilities{}, err
	}

	return CameraCapabilities{
		PTZ:             caps.PTZ,
		AudioIn:         caps.Audio,
		AudioOut:        false,
		LPR:             caps.LPR,
		FaceRecognition: caps.FaceRecognition,
		WebRTC:          true,
		MSE:             true,
	}, nil
}
