package camweb

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/agent/camproxy"
	"github.com/jdolan-exalink/openvms/internal/authz"
)

const (
	// wsPath is where the camera UI's WebSocket connections arrive: /camproxy/{session}/__ws/{port}/{path}.
	// The shim rewrites the camera's ws://host:port/path to it, because a camera streams live video over
	// a port of its own that the HTTP proxy cannot reach.
	wsPath = "/__ws/"

	maxTunnelsPerSession = 8
	tunnelHandshake      = 15 * time.Second
)

// Tunneler is implemented by forwarders that can open a raw byte tunnel to a camera through the
// server's agent. Without it the camera UI still works, only its live video does not.
type Tunneler interface {
	Tunnel(ctx context.Context, actor authz.Actor, serverID uuid.UUID, header http.Header) (io.ReadWriteCloser, error)
}

func isWebSocketUpgrade(r *http.Request) bool {
	return r.Method == http.MethodGet &&
		strings.EqualFold(r.Header.Get("Upgrade"), "websocket") &&
		strings.Contains(strings.ToLower(r.Header.Get("Connection")), "upgrade")
}

// serveWebSocket carries one camera WebSocket. It opens a tunnel to the camera's port through the
// agent, replays the browser's handshake into it and then relays bytes both ways, so the camera
// negotiates the WebSocket itself and no frame is parsed here. rest is "{port}/{path}".
func (s *Service) serveWebSocket(w http.ResponseWriter, r *http.Request, sess *session, rest string) {
	tn, ok := s.fwd.(Tunneler)
	if !ok {
		plain(w, http.StatusNotImplemented, "live video is not available through this connection")
		return
	}
	portStr, path, _ := strings.Cut(rest, "/")
	port, err := strconv.Atoi(portStr)
	if err != nil || port < 1 || port > 65535 || !isWebSocketUpgrade(r) {
		plain(w, http.StatusBadRequest, "invalid websocket request")
		return
	}
	path = "/" + path
	if len(path)+len(r.URL.RawQuery) > maxURILen {
		plain(w, http.StatusRequestURITooLong, "")
		return
	}
	s.mu.Lock()
	if sess.tunnels >= maxTunnelsPerSession {
		s.mu.Unlock()
		w.Header().Set("Retry-After", "5")
		plain(w, http.StatusTooManyRequests, "too many open streams")
		return
	}
	sess.tunnels++
	s.mu.Unlock()
	defer func() { s.mu.Lock(); sess.tunnels--; s.mu.Unlock() }()

	tgt := sess.target
	tgt.port = port
	ctl := http.Header{}
	ctl.Set(camproxy.HeaderTarget, tgt.String())
	ctx, cancel := context.WithCancel(context.WithoutCancel(r.Context()))
	defer cancel()
	handshake := time.AfterFunc(tunnelHandshake, cancel)
	tunnel, err := tn.Tunnel(ctx, sess.actor, sess.serverID, ctl)
	handshake.Stop()
	if err != nil {
		plain(w, http.StatusBadGateway, "the camera stream could not be opened through the agent")
		return
	}
	defer tunnel.Close()

	// The camera's own handshake. Go canonicalizes header names (Sec-Websocket-Key), and embedded WebSocket
	// servers often match them case-sensitively, so the request is written by hand with the spelling every
	// browser uses. Extensions are not negotiated (a camera has no use for permessage-deflate) and nothing
	// that identifies OpenVMS or its origin goes along.
	var hs bytes.Buffer
	line := func(name, value string) {
		if value != "" && !strings.ContainsAny(value, "\r\n") {
			hs.WriteString(name + ": " + value + "\r\n")
		}
	}
	hs.WriteString("GET " + (&url.URL{Path: path, RawQuery: r.URL.RawQuery}).RequestURI() + " HTTP/1.1\r\n")
	line("Host", tgt.host+":"+strconv.Itoa(port))
	line("Connection", "Upgrade")
	line("Upgrade", "websocket")
	// The page that opens a camera stream is the camera's own web page, not its stream port: cameras that
	// check the Origin of a WebSocket expect that one.
	line("Origin", sess.target.origin())
	line("Sec-WebSocket-Key", r.Header.Get("Sec-WebSocket-Key"))
	line("Sec-WebSocket-Version", r.Header.Get("Sec-WebSocket-Version"))
	line("Sec-WebSocket-Protocol", r.Header.Get("Sec-WebSocket-Protocol"))
	line("User-Agent", r.Header.Get("User-Agent"))
	line("Cookie", sess.cookieHeader(path))
	hs.WriteString("\r\n")
	if _, err := tunnel.Write(hs.Bytes()); err != nil {
		plain(w, http.StatusBadGateway, "the camera stream could not be opened through the agent")
		return
	}

	conn, rw, err := http.NewResponseController(w).Hijack()
	if err != nil {
		plain(w, http.StatusInternalServerError, "")
		return
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Time{})
	// A stream never outlives its session.
	s.mu.Lock()
	left := sess.expires.Sub(s.cfg.Now())
	s.mu.Unlock()
	expire := time.AfterFunc(left, func() { _ = conn.Close(); _ = tunnel.Close() })
	defer expire.Stop()

	var once sync.Once
	var toCam, fromCam int64
	var firstLine string
	started := time.Now()
	done := make(chan struct{})
	finish := func() { once.Do(func() { close(done); _ = conn.Close(); _ = tunnel.Close() }) }
	go func() { // browser -> camera, incl. bytes already buffered
		n, _ := io.Copy(tunnel, rw.Reader)
		atomic.AddInt64(&toCam, n)
		finish()
	}()
	go func() { // camera -> browser, starting with its 101
		buf := make([]byte, 32<<10)
		for {
			n, err := tunnel.Read(buf)
			if n > 0 {
				if atomic.AddInt64(&fromCam, int64(n)) == int64(n) { // first bytes: keep the status line for diagnosis
					firstLine, _, _ = strings.Cut(string(buf[:min(n, 120)]), "\r\n")
				}
				if _, werr := conn.Write(buf[:n]); werr != nil {
					break
				}
			}
			if err != nil {
				break
			}
		}
		finish()
	}()
	<-done
	// No credentials or payload: only what is needed to tell why a camera stream ended.
	slog.Info("camera stream closed", "target", tgt.String(), "path", path, "to_camera_bytes", atomic.LoadInt64(&toCam), "from_camera_bytes", atomic.LoadInt64(&fromCam), "camera_first_line", firstLine, "duration_ms", time.Since(started).Milliseconds())
}
