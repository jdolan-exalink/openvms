package media

import (
	"context"
	"errors"
	"net"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/go2rtc"
	"github.com/jdolan-exalink/openvms/internal/inventory"
	"github.com/jdolan-exalink/openvms/internal/provisionnvr"
	"github.com/jdolan-exalink/openvms/internal/secrets"
)

// codecCacheTTL is how long the codec of a recorder stream is remembered. It changes only when someone
// reconfigures the camera, and every viewer would otherwise pay a DESCRIBE.
const codecCacheTTL = 10 * time.Minute

// defaultRTSPPort is used when the recorder never reported its RTSP port.
const defaultRTSPPort = 554

// NVRVideo plays the cameras of Provision-ISR recorders. It builds the RTSP source from the stored
// account, registers it in a go2rtc container (which keeps the credentials in memory) and returns the
// websocket the gateway proxies to the browser. The browser never sees the recorder's address or password.
type NVRVideo struct {
	Sealer *secrets.Sealer
	G2     *go2rtc.Client

	// Probe replaces provisionnvr.ProbeRTSP. For tests.
	Probe func(ctx context.Context, host string, port int, user, password string, channel int, stream provisionnvr.Stream) (*provisionnvr.RTSPFacts, error)
	// Now replaces time.Now. For tests.
	Now func() time.Time

	mu     sync.Mutex
	codecs map[codecKey]codecEntry
}

type codecKey struct {
	camera uuid.UUID
	stream provisionnvr.Stream
}

type codecEntry struct {
	codec string
	until time.Time
}

// NVRVideoError is a failure to open a recorder camera, with the live error code the player gets.
type NVRVideoError struct {
	Code string
	Msg  string
}

func (e *NVRVideoError) Error() string { return e.Code + ": " + e.Msg }

// NVRLive is an opened recorder stream.
type NVRLive struct {
	// Name is the go2rtc stream name (audited; it holds no secret).
	Name string
	// Target is the go2rtc websocket to dial.
	Target string
	// Transcoded is true when the stream is converted to H.264.
	Transcoded bool
}

func (n *NVRVideo) now() time.Time {
	if n.Now != nil {
		return n.Now()
	}
	return time.Now()
}

// Open registers the camera's stream in go2rtc and returns where to read it. quality "main" picks the main
// stream; anything else the sub stream.
func (n *NVRVideo) Open(ctx context.Context, cam Camera, quality string) (*NVRLive, error) {
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
	pw, err := n.Sealer.Open(cam.Server.PasswordSealed, cam.Server.ID[:])
	if err != nil {
		return nil, &NVRVideoError{Code: CodeServerError, Msg: "the recorder's stored password cannot be read"}
	}
	port := inventory.NVRInfoFromCapabilities(cam.Server.Capabilities).RTSPPort
	if port <= 0 {
		port = defaultRTSPPort
	}

	codec, err := n.codec(ctx, cam.ID, base.Hostname(), port, cam.Server.Username, string(pw), channel, stream)
	if err != nil {
		return nil, err
	}
	transcode := !strings.EqualFold(codec, "H264")
	src := recorderSource(base.Hostname(), port, cam.Server.Username, string(pw), provisionnvr.RTSPPath(channel, stream), transcode)
	name := "nvr-" + cam.ID.String() + "-" + string(stream)
	if err := n.G2.Put(ctx, name, src); err != nil {
		return nil, &NVRVideoError{Code: CodeUpstreamUnreachable, Msg: "could not reach the video gateway"}
	}
	target, err := n.G2.WebSocketURL(name)
	if err != nil {
		return nil, &NVRVideoError{Code: CodeServerError, Msg: "invalid stream name"}
	}
	return &NVRLive{Name: name, Target: target, Transcoded: transcode}, nil
}

// codec returns the video codec of a recorder stream, asking the recorder at most once per codecCacheTTL.
func (n *NVRVideo) codec(ctx context.Context, camera uuid.UUID, host string, port int, user, password string, channel int, stream provisionnvr.Stream) (string, error) {
	key := codecKey{camera, stream}
	now := n.now()
	n.mu.Lock()
	if e, ok := n.codecs[key]; ok && now.Before(e.until) {
		n.mu.Unlock()
		return e.codec, nil
	}
	n.mu.Unlock()

	probe := n.Probe
	if probe == nil {
		probe = provisionnvr.ProbeRTSP
	}
	facts, err := probe(ctx, host, port, user, password, channel, stream)
	if err != nil {
		return "", rtspFailure(err)
	}
	n.mu.Lock()
	if n.codecs == nil {
		n.codecs = map[codecKey]codecEntry{}
	}
	n.codecs[key] = codecEntry{codec: facts.Codec, until: now.Add(codecCacheTTL)}
	n.mu.Unlock()
	return facts.Codec, nil
}

// rtspFailure maps what the recorder's RTSP server said to the code the player shows.
func rtspFailure(err error) error {
	if errors.Is(err, provisionnvr.ErrRTSPAuth) {
		return &NVRVideoError{Code: CodeUpstreamUnreachable, Msg: "the recorder rejected the stored credentials"}
	}
	class, _ := provisionnvr.ClassOf(err)
	switch class {
	case provisionnvr.ClassOffline, provisionnvr.ClassUnsupported:
		return &NVRVideoError{Code: CodeUpstreamUnreachable, Msg: "could not reach the recorder's video"}
	default: // the recorder answered but has no such stream (channel without a camera, for example)
		return &NVRVideoError{Code: CodeCameraOffline, Msg: "the recorder has no video for this camera"}
	}
}

// recorderSource is the go2rtc source of a recorder stream. It carries the credentials: it goes only to
// go2rtc's API on the private network and is never logged. The recorder's H.264 is copied; anything else
// is converted, because not every browser plays H.265 through Media Source Extensions.
func recorderSource(host string, port int, user, password, path string, transcode bool) string {
	userinfo := url.UserPassword(user, password).String()
	rtsp := "rtsp://" + userinfo + "@" + net.JoinHostPort(host, strconv.Itoa(port)) + path
	if transcode {
		return "ffmpeg:" + rtsp + "#video=h264"
	}
	return "ffmpeg:" + rtsp + "#video=copy"
}
