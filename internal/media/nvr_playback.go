package media

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/inventory"
	"github.com/jdolan-exalink/openvms/internal/provisionnvr"
)

// offsetCacheTTL is how long the recorder's clock offset is remembered. It moves only when someone changes
// the recorder's time zone or its daylight saving.
const offsetCacheTTL = 10 * time.Minute

// maxReplaySpan bounds one replay session; the player asks for a window around the instant being watched.
const maxReplaySpan = 24 * time.Hour

// recorderSession is a logged-in client for one recorder, reused across requests so the timeline of seven
// cameras does not mean seven logins a minute. It is replaced when the stored connection changes.
type recorderSession struct {
	sealed      []byte
	base        string
	user        string
	client      *provisionnvr.Client
	offset      time.Duration
	offsetUntil time.Time
}

// password unseals the recorder's stored password for one call; it is never kept.
func (n *NVRVideo) password(cam Camera) (string, error) {
	pw, err := n.Sealer.Open(cam.Server.PasswordSealed, cam.Server.ID[:])
	if err != nil {
		return "", &NVRVideoError{Code: CodeServerError, Msg: "the recorder's stored password cannot be read"}
	}
	return string(pw), nil
}

func (n *NVRVideo) session(cam Camera) (*recorderSession, error) {
	srv := cam.Server
	n.mu.Lock()
	defer n.mu.Unlock()
	if s, ok := n.sessions[srv.ID]; ok && string(s.sealed) == string(srv.PasswordSealed) && s.base == srv.BaseUrl && s.user == srv.Username {
		return s, nil
	}
	pw, err := n.password(cam)
	if err != nil {
		return nil, err
	}
	client, err := provisionnvr.NewClient(srv.BaseUrl, provisionnvr.NewCredentials(srv.Username, pw), provisionnvr.Config{})
	if err != nil {
		return nil, &NVRVideoError{Code: CodeServerError, Msg: "the recorder has no valid address"}
	}
	if n.sessions == nil {
		n.sessions = map[uuid.UUID]*recorderSession{}
	}
	s := &recorderSession{sealed: srv.PasswordSealed, base: srv.BaseUrl, user: srv.Username, client: client}
	n.sessions[srv.ID] = s
	return s, nil
}

// forgetSession makes the next request log in again (after a failure that may mean a stale session).
func (n *NVRVideo) forgetSession(id uuid.UUID) {
	n.mu.Lock()
	delete(n.sessions, id)
	n.mu.Unlock()
}

// utcOffset returns the recorder's clock offset, asking at most once per offsetCacheTTL.
func (n *NVRVideo) utcOffset(ctx context.Context, s *recorderSession) (time.Duration, error) {
	n.mu.Lock()
	if n.now().Before(s.offsetUntil) {
		off := s.offset
		n.mu.Unlock()
		return off, nil
	}
	n.mu.Unlock()
	off, err := s.client.UTCOffset(ctx)
	if err != nil {
		return 0, err
	}
	n.mu.Lock()
	s.offset, s.offsetUntil = off, n.now().Add(offsetCacheTTL)
	n.mu.Unlock()
	return off, nil
}

// recorderFailure maps a failed call to the recorder to what the API and the player say.
func recorderFailure(err error) error {
	class, ok := provisionnvr.ClassOf(err)
	if !ok {
		return &inventory.NVRError{Class: provisionnvr.ClassOffline, Err: err}
	}
	return &inventory.NVRError{Class: class, Err: err}
}

// Recordings lists the stretches of video a recorder camera has between from and to, from the recorder's own
// record log. Neighbouring segments are not merged here: the timeline code does that for every camera.
func (n *NVRVideo) Recordings(ctx context.Context, cam Camera, from, to time.Time) ([]RecordingSpan, error) {
	s, err := n.session(cam)
	if err != nil {
		return nil, err
	}
	segs, err := s.client.RecordLog(ctx, cam.RemoteName, from, to)
	if err != nil {
		n.forgetSession(cam.Server.ID)
		return nil, recorderFailure(err)
	}
	out := make([]RecordingSpan, 0, len(segs))
	for _, sg := range segs {
		span := RecordingSpan{Start: sg.Start, End: sg.End}
		if sg.Type == "MOTION" {
			span.Motion = 1
		}
		out = append(out, span)
	}
	return out, nil
}

// OpenReplay registers a go2rtc stream that plays the camera's recording from start for up to end-start and
// returns where to read it. Each session has its own stream (a second viewer of the same camera must not
// replace the first one's source); Close removes it. quality "main" picks the main stream, anything else
// the sub stream.
func (n *NVRVideo) OpenReplay(ctx context.Context, cam Camera, start, end time.Time, quality string) (*NVRLive, error) {
	if !end.After(start) || end.Sub(start) > maxReplaySpan {
		return nil, &NVRVideoError{Code: CodeCameraOffline, Msg: "the replay range is invalid"}
	}
	stream := provisionnvr.StreamSub
	if quality == "main" {
		stream = provisionnvr.StreamMain
	}
	channel, ok := provisionnvr.ChannelNumber(cam.RemoteName)
	if !ok {
		return nil, &NVRVideoError{Code: CodeCameraOffline, Msg: "the camera is not a numbered channel of the recorder"}
	}
	base, err := url.Parse(cam.Server.BaseUrl)
	if err != nil || base.Hostname() == "" {
		return nil, &NVRVideoError{Code: CodeServerError, Msg: "the recorder has no valid address"}
	}
	s, err := n.session(cam)
	if err != nil {
		return nil, err
	}
	pw, err := n.password(cam)
	if err != nil {
		return nil, err
	}
	off, err := n.utcOffset(ctx, s)
	if err != nil {
		n.forgetSession(cam.Server.ID)
		return nil, rtspFailure(err)
	}
	port := inventory.NVRInfoFromCapabilities(cam.Server.Capabilities).RTSPPort
	if port <= 0 {
		port = defaultRTSPPort
	}
	// The replay of a stream plays in the same codec as its live stream.
	codec, err := n.codec(ctx, cam.ID, base.Hostname(), port, cam.Server.Username, pw, channel, stream)
	if err != nil {
		return nil, err
	}
	transcode := !strings.EqualFold(codec, "H264")
	path := provisionnvr.ReplayPath(channel, stream, start, off, end.Sub(start))
	src := recorderSource(base.Hostname(), port, cam.Server.Username, pw, path, transcode)
	name := "nvr-" + cam.ID.String() + "-rec-" + randomSuffix()
	if err := n.G2.Put(ctx, name, src); err != nil {
		return nil, &NVRVideoError{Code: CodeUpstreamUnreachable, Msg: "could not reach the video gateway"}
	}
	target, err := n.G2.WebSocketURL(name)
	if err != nil {
		return nil, &NVRVideoError{Code: CodeServerError, Msg: "invalid stream name"}
	}
	return &NVRLive{Name: name, Target: target, Transcoded: transcode}, nil
}

// Close removes the go2rtc stream of a finished replay session. It is best effort: a stream that stays
// behind only costs a few bytes in go2rtc's memory until it restarts.
func (n *NVRVideo) Close(ctx context.Context, live *NVRLive) error {
	if live == nil || live.Name == "" {
		return nil
	}
	return n.G2.Delete(ctx, live.Name)
}

func randomSuffix() string {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return "00000000"
	}
	return hex.EncodeToString(b)
}
