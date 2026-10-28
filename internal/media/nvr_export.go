package media

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jdolan-exalink/openvms/internal/inventory"
	"github.com/jdolan-exalink/openvms/internal/provisionnvr"
)

// ErrNoRecording is returned when a recorder has no video at all in the range asked to export.
var ErrNoRecording = errors.New("the recorder has no recording in that range")

// ReplayURL is the RTSP address that plays the camera's recording from start for end-start, in the stream
// quality asked ("main" or sub). It carries the recorder's credentials: use it only as an ffmpeg input and never
// log it.
func (n *NVRVideo) ReplayURL(ctx context.Context, cam Camera, start, end time.Time, quality string) (string, error) {
	stream := provisionnvr.StreamSub
	if quality == "main" {
		stream = provisionnvr.StreamMain
	}
	channel, ok := provisionnvr.ChannelNumber(cam.RemoteName)
	if !ok {
		return "", &NVRVideoError{Code: CodeCameraOffline, Msg: "the camera is not a numbered channel of the recorder"}
	}
	base, err := url.Parse(cam.Server.BaseUrl)
	if err != nil || base.Hostname() == "" {
		return "", &NVRVideoError{Code: CodeServerError, Msg: "the recorder has no valid address"}
	}
	pw, err := n.password(cam)
	if err != nil {
		return "", err
	}
	s, err := n.session(cam)
	if err != nil {
		return "", err
	}
	off, err := n.utcOffset(ctx, s)
	if err != nil {
		n.forgetSession(cam.Server.ID)
		return "", rtspFailure(err)
	}
	port := inventory.NVRInfoFromCapabilities(cam.Server.Capabilities).RTSPPort
	if port <= 0 {
		port = defaultRTSPPort
	}
	path := provisionnvr.ReplayPath(channel, stream, start, off, end.Sub(start))
	return "rtsp://" + url.UserPassword(cam.Server.Username, pw).String() + "@" + net.JoinHostPort(base.Hostname(), strconv.Itoa(port)) + path, nil
}

// HasRecording reports whether the recorder holds any video between from and to.
func (n *NVRVideo) HasRecording(ctx context.Context, cam Camera, from, to time.Time) (bool, error) {
	spans, err := n.Recordings(ctx, cam, from, to)
	if err != nil {
		return false, err
	}
	return len(spans) > 0, nil
}

const (
	// replayStallTimeoutUS ends a read of the recorder that gets no data, in microseconds.
	replayStallTimeoutUS = 10_000_000
	// replayCapSlack lets ffmpeg's -t cap sit a little above the clip so a clip is never cut short.
	replayCapSlack = 2
	// replayFinishMargin is how close to the clip's length the capture is asked to finish.
	replayFinishMargin = time.Second
	// ffmpegMessageMax bounds the ffmpeg message kept in an error.
	ffmpegMessageMax = 300
)

// ffmpegReplayArgs copies the recorder's replay into an MP4 without re-encoding. Audio is dropped: recorders
// send G.711, which MP4 cannot carry as it is. The recorder does not close the stream when the clip ends, so
// -timeout ends a stalled read after 10 s and captureReplay asks ffmpeg to finish once the whole clip is written.
func ffmpegReplayArgs(rtspURL, outPath string, d time.Duration) []string {
	secs := int(d.Round(time.Second) / time.Second)
	if secs < 1 {
		secs = 1
	}
	return []string{
		"-y", "-loglevel", "error", "-nostats", "-progress", "pipe:1",
		"-rtsp_transport", "tcp", "-timeout", strconv.Itoa(replayStallTimeoutUS), "-i", rtspURL,
		"-t", strconv.Itoa(secs + replayCapSlack), "-an", "-c:v", "copy", "-movflags", "+faststart",
		"-f", "mp4", outPath,
	}
}

// redactRTSP removes the recorder's address and credentials from a message ffmpeg printed.
func redactRTSP(msg, rtspURL string) string {
	msg = strings.ReplaceAll(msg, rtspURL, "rtsp://***")
	if u, err := url.Parse(rtspURL); err == nil {
		if u.User != nil {
			if pw, ok := u.User.Password(); ok && pw != "" {
				msg = strings.ReplaceAll(msg, pw, "***")
				msg = strings.ReplaceAll(msg, url.QueryEscape(pw), "***")
				msg = strings.ReplaceAll(msg, url.PathEscape(pw), "***")
			}
		}
		if h := u.Hostname(); h != "" {
			msg = strings.ReplaceAll(msg, h, "recorder")
		}
	}
	return msg
}

// progressWriter turns ffmpeg's "-progress" lines into calls with how much video is written.
type progressWriter struct {
	buf []byte
	fn  func(done time.Duration)
}

func (w *progressWriter) Write(p []byte) (int, error) {
	w.buf = append(w.buf, p...)
	for {
		i := bytes.IndexByte(w.buf, '\n')
		if i < 0 {
			break
		}
		line := strings.TrimSpace(string(w.buf[:i]))
		w.buf = w.buf[i+1:]
		if v, ok := strings.CutPrefix(line, "out_time_us="); ok && w.fn != nil {
			if us, err := strconv.ParseInt(v, 10, 64); err == nil && us > 0 {
				w.fn(time.Duration(us) * time.Microsecond)
			}
		}
	}
	return len(p), nil
}

// captureReplay runs ffmpeg to write the replay to outPath. onProgress receives how much video is written so far.
// The recorder plays at normal speed, so this takes about as long as the clip.
func captureReplay(ctx context.Context, ffmpegPath, rtspURL, outPath string, d time.Duration, onProgress func(done time.Duration)) error {
	cmd := exec.CommandContext(ctx, ffmpegPath, ffmpegReplayArgs(rtspURL, outPath, d)...) // #nosec G204 -- fixed binary, argv only, no shell
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	// ffmpeg is told to finish ("q" on its input) as soon as the whole clip is written, instead of waiting for the
	// recorder to close a stream it keeps open.
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	var quit sync.Once
	var written atomic.Int64
	cmd.Stdout = &progressWriter{fn: func(done time.Duration) {
		written.Store(int64(done))
		if onProgress != nil {
			onProgress(done)
		}
		if done >= d-replayFinishMargin {
			// In its own goroutine: this runs inside ffmpeg's output pipe reader and must never block it.
			quit.Do(func() {
				go func() {
					_, _ = io.WriteString(stdin, "q\n")
					_ = stdin.Close()
				}()
			})
		}
	}}
	// A killed ffmpeg may leave a child holding the pipes; do not wait for it forever.
	cmd.WaitDelay = 2 * time.Second
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("could not start ffmpeg: %s", redactRTSP(err.Error(), rtspURL))
	}
	if err := cmd.Wait(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		msg := strings.TrimSpace(redactRTSP(stderr.String(), rtspURL))
		// The range may run past the last recording: the recorder then sends nothing more and keeps the stream
		// open, so ffmpeg ends on its stall timeout. What was written up to there is the footage that exists.
		if written.Load() > 0 && strings.Contains(strings.ToLower(msg), "timed out") {
			if fi, serr := os.Stat(outPath); serr == nil && fi.Size() > 0 {
				return nil
			}
		}
		if msg == "" {
			msg = err.Error()
		}
		if len(msg) > ffmpegMessageMax {
			msg = msg[:ffmpegMessageMax]
		}
		return fmt.Errorf("the recorder's video could not be copied: %s", msg)
	}
	return nil
}
