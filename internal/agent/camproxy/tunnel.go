package camproxy

import (
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// TunnelProtocol is the Upgrade token of a tunnel request. A tunnel carries the raw bytes of one
	// connection to the camera (a live-video WebSocket); the agent never parses them.
	TunnelProtocol = "openvms-camera-tunnel"

	// maxTunnelLifetime bounds one tunnel; the control plane's own session limit is shorter.
	maxTunnelLifetime = 8 * time.Hour
)

func isTunnelRequest(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("Upgrade"), TunnelProtocol)
}

// serveTunnel dials the camera named by X-OpenVMS-Target, applies exactly the endpoint rules of the
// HTTP path (private LAN IPv4 only, never this host or a blocked port, checked again on the dialled
// address) and, once connected, answers 101 and relays bytes both ways until either side closes.
func (h *Handler) serveTunnel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		fail(w, http.StatusMethodNotAllowed, "method_not_allowed")
		return
	}
	target, err := parseTargetSyntax(r.Header.Get(HeaderTarget))
	if err != nil {
		fail(w, http.StatusBadRequest, "invalid_target")
		return
	}
	if err := h.checkEndpoint(target.Addr, target.Port); err != nil {
		fail(w, http.StatusForbidden, "target_not_allowed")
		return
	}
	dialer := &net.Dialer{Timeout: h.cfg.ConnectTimeout, Control: h.dialControl}
	hostport := net.JoinHostPort(target.Addr.String(), strconv.Itoa(target.Port))
	var cam net.Conn
	if target.Scheme == "https" {
		// Cameras ship self-signed certificates and targets are literal IPs: nothing to verify, as in
		// the HTTP path, which is the only other user of this relaxation.
		cam, err = (&tls.Dialer{NetDialer: dialer, Config: &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS10}}).DialContext(r.Context(), "tcp", hostport) // #nosec G402 -- see above
	} else {
		cam, err = dialer.DialContext(r.Context(), "tcp", hostport)
	}
	if err != nil {
		fail(w, http.StatusBadGateway, "camera_unreachable")
		return
	}
	defer cam.Close()

	rc := http.NewResponseController(w)
	conn, rw, err := rc.Hijack()
	if err != nil {
		fail(w, http.StatusInternalServerError, "tunnel_unsupported")
		return
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Time{}) // the server's header timeout must not outlive the hijack
	if _, err := io.WriteString(conn, "HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: "+TunnelProtocol+"\r\n"+HeaderProxy+": 1\r\n\r\n"); err != nil {
		return
	}

	limit := time.AfterFunc(maxTunnelLifetime, func() { _ = conn.Close(); _ = cam.Close() })
	defer limit.Stop()
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { // control plane -> camera, including anything the server already buffered
		defer wg.Done()
		_, _ = io.Copy(cam, rw.Reader)
		closeWrite(cam)
	}()
	go func() { // camera -> control plane
		defer wg.Done()
		_, _ = io.Copy(conn, cam)
		closeWrite(conn)
	}()
	wg.Wait()
}

// closeWrite half-closes c when it can, so the peer sees EOF while its own data still drains.
func closeWrite(c net.Conn) {
	type halfCloser interface{ CloseWrite() error }
	if hc, ok := c.(halfCloser); ok {
		_ = hc.CloseWrite()
		return
	}
	_ = c.Close()
}
