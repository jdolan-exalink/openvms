// Package camproxy lets the control plane reach the web interface of a camera that only the edge
// agent can see. The agent forwards one HTTP request per call to a camera on its LAN and streams the
// answer back. It is the second line of defence of that feature: whatever the control plane sends,
// the agent only talks to literal private IPv4 addresses (RFC 1918), never to its own host, never to
// its own listening ports and never to SSH.
//
// Wire contract (all control headers start with X-OpenVMS-):
//
//	request  X-OpenVMS-Target                 http|https://IPv4[:port]  (scheme and authority only)
//	         X-OpenVMS-Target-URI             path and query on the camera, starting with "/"
//	         X-OpenVMS-Forward-Authorization  becomes the upstream Authorization
//	         X-OpenVMS-Forward-Cookie         becomes the upstream Cookie
//	response X-OpenVMS-Proxy: 1               present on every answer produced by the proxy path
//	         X-OpenVMS-Proxy-Error: <code>    present when the agent itself refused or failed
//	         X-OpenVMS-Set-Cookie             one entry per upstream Set-Cookie
//
// The Authorization header of the call is the agent token and is never forwarded.
package camproxy

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	// Path is the agent route.
	Path = "/v1/camera-web/"

	HeaderTarget               = "X-OpenVMS-Target"
	HeaderTargetURI            = "X-OpenVMS-Target-URI"
	HeaderForwardAuthorization = "X-OpenVMS-Forward-Authorization"
	HeaderForwardCookie        = "X-OpenVMS-Forward-Cookie"
	HeaderSetCookie            = "X-OpenVMS-Set-Cookie"
	HeaderProxy                = "X-OpenVMS-Proxy"
	HeaderProxyError           = "X-OpenVMS-Proxy-Error"

	DefaultMaxRequestBody  = 10 << 20
	DefaultMaxResponseBody = 64 << 20
	DefaultConnectTimeout  = 5 * time.Second
	DefaultTotalTimeout    = 60 * time.Second
	maxTargetURILen        = 8 << 10
)

// Config tunes a Handler. The zero value is the production policy.
type Config struct {
	// BlockedPorts are the agent's own listening ports; port 22 is always blocked.
	BlockedPorts []int
	// SelfAddrs lists the addresses of this host; nil reads the network interfaces.
	SelfAddrs       func() []netip.Addr
	MaxRequestBody  int64
	MaxResponseBody int64
	ConnectTimeout  time.Duration
	TotalTimeout    time.Duration
}

// Target is a validated camera address.
type Target struct {
	Scheme string
	Addr   netip.Addr
	Port   int
}

// AllowedAddr reports whether a is a private IPv4 address a camera may have: 10/8, 172.16/12 or
// 192.168/16. Loopback, link-local (169.254/16), CGNAT (100.64/10), multicast, unspecified, IPv6 and
// public addresses are refused.
func AllowedAddr(a netip.Addr) bool {
	if !a.IsValid() || !a.Is4() {
		return false
	}
	b := a.As4()
	switch {
	case b[0] == 10:
		return true
	case b[0] == 172 && b[1]&0xf0 == 16:
		return true
	case b[0] == 192 && b[1] == 168:
		return true
	}
	return false
}

// ParseTarget validates the X-OpenVMS-Target value: scheme and authority of a literal IPv4 address.
// Ports that belong to the agent are checked by the Handler, which knows them.
func ParseTarget(raw string) (Target, error) {
	t, err := parseTargetSyntax(raw)
	if err != nil {
		return Target{}, err
	}
	if !AllowedAddr(t.Addr) {
		return Target{}, errors.New("target address is not a private LAN address")
	}
	return t, nil
}

// parseTargetSyntax checks the shape only; the address range is the Handler's checkEndpoint.
func parseTargetSyntax(raw string) (Target, error) {
	if raw == "" || len(raw) > 128 || strings.ContainsAny(raw, " \t\r\n\\") {
		return Target{}, errors.New("invalid target")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Opaque != "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.ForceQuery {
		return Target{}, errors.New("invalid target")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return Target{}, errors.New("target scheme must be http or https")
	}
	addr, err := netip.ParseAddr(u.Hostname())
	if err != nil || !addr.Is4() || addr.Zone() != "" {
		return Target{}, errors.New("target must be a literal IPv4 address")
	}
	port := 80
	if u.Scheme == "https" {
		port = 443
	}
	if p := u.Port(); p != "" {
		port, err = strconv.Atoi(p)
		if err != nil {
			return Target{}, errors.New("invalid target port")
		}
	} else if strings.HasSuffix(u.Host, ":") {
		return Target{}, errors.New("invalid target port")
	}
	if port < 1 || port > 65535 {
		return Target{}, errors.New("target port out of range")
	}
	return Target{Scheme: u.Scheme, Addr: addr, Port: port}, nil
}

// Handler is the agent side of the camera web view.
type Handler struct {
	cfg       Config
	blocked   map[int]bool
	allowAddr func(netip.Addr) bool // replaced only by tests, which cannot use a private LAN address
	transport *http.Transport
}

// New builds a Handler.
func New(cfg Config) *Handler {
	if cfg.MaxRequestBody <= 0 {
		cfg.MaxRequestBody = DefaultMaxRequestBody
	}
	if cfg.MaxResponseBody <= 0 {
		cfg.MaxResponseBody = DefaultMaxResponseBody
	}
	if cfg.ConnectTimeout <= 0 {
		cfg.ConnectTimeout = DefaultConnectTimeout
	}
	if cfg.TotalTimeout <= 0 {
		cfg.TotalTimeout = DefaultTotalTimeout
	}
	h := &Handler{cfg: cfg, blocked: map[int]bool{22: true}, allowAddr: AllowedAddr}
	for _, p := range cfg.BlockedPorts {
		h.blocked[p] = true
	}
	dialer := &net.Dialer{Timeout: cfg.ConnectTimeout, Control: h.dialControl}
	h.transport = &http.Transport{
		Proxy:               nil, // never through HTTP_PROXY: it would bypass the dial guard
		DialContext:         dialer.DialContext,
		DisableKeepAlives:   true, // no connection is shared between callers
		DisableCompression:  true, // bytes pass through as the camera sent them
		TLSHandshakeTimeout: cfg.ConnectTimeout,
		// Cameras ship self-signed certificates and there is no name to verify (targets are literal IPs).
		// This client is used only by this proxy, which only reaches private LAN addresses.
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS10}, // #nosec G402 -- see above
	}
	return h
}

func (h *Handler) selfAddrs() []netip.Addr {
	if h.cfg.SelfAddrs != nil {
		return h.cfg.SelfAddrs()
	}
	var out []netip.Addr
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil
	}
	for _, a := range addrs {
		if ipnet, ok := a.(*net.IPNet); ok {
			if ip, ok := netip.AddrFromSlice(ipnet.IP); ok {
				out = append(out, ip.Unmap())
			}
		}
	}
	return out
}

func (h *Handler) checkEndpoint(addr netip.Addr, port int) error {
	if !h.allowAddr(addr) {
		return errors.New("address not allowed")
	}
	if port < 1 || port > 65535 || h.blocked[port] {
		return errors.New("port not allowed")
	}
	for _, self := range h.selfAddrs() {
		if self == addr {
			return errors.New("address belongs to this host")
		}
	}
	return nil
}

// dialControl runs after name resolution, on the address about to be connected: it holds even if a
// name ever reached the dialer.
func (h *Handler) dialControl(_, address string, _ syscall.RawConn) error {
	host, portStr, err := net.SplitHostPort(address)
	if err != nil {
		return errors.New("camera proxy: bad dial address")
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return errors.New("camera proxy: bad dial address")
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return errors.New("camera proxy: bad dial port")
	}
	if err := h.checkEndpoint(addr, port); err != nil {
		return fmt.Errorf("camera proxy: %w", err)
	}
	return nil
}

func fail(w http.ResponseWriter, status int, code string) {
	w.Header().Set(HeaderProxyError, code)
	w.Header().Set(HeaderProxy, "1")
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(code + "\n"))
}

var hopByHop = []string{"Connection", "Proxy-Connection", "Keep-Alive", "Proxy-Authenticate", "Proxy-Authorization", "Te", "Trailer", "Transfer-Encoding", "Upgrade"}

func stripHopByHop(h http.Header) {
	for _, v := range h.Values("Connection") {
		for _, name := range strings.Split(v, ",") {
			if name = strings.TrimSpace(name); name != "" {
				h.Del(name)
			}
		}
	}
	for _, name := range hopByHop {
		h.Del(name)
	}
}

var allowedMethods = map[string]bool{
	http.MethodGet: true, http.MethodHead: true, http.MethodPost: true,
	http.MethodPut: true, http.MethodDelete: true, http.MethodOptions: true,
}

func validURI(uri string) bool {
	if uri == "" {
		return true
	}
	if len(uri) > maxTargetURILen || uri[0] != '/' || strings.HasPrefix(uri, "//") {
		return false
	}
	for i := 0; i < len(uri); i++ {
		if uri[i] <= ' ' || uri[i] == 0x7f || uri[i] == '\\' {
			return false
		}
	}
	return true
}

// ServeHTTP forwards one request. The caller has already authenticated the agent token.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !allowedMethods[r.Method] {
		w.Header().Set("Allow", "GET, HEAD, POST, PUT, DELETE, OPTIONS")
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
	uri := r.Header.Get(HeaderTargetURI)
	if !validURI(uri) {
		fail(w, http.StatusBadRequest, "invalid_target_uri")
		return
	}
	if uri == "" {
		uri = "/"
	}
	if r.ContentLength > h.cfg.MaxRequestBody {
		fail(w, http.StatusRequestEntityTooLarge, "request_too_large")
		return
	}

	hostport := net.JoinHostPort(target.Addr.String(), strconv.Itoa(target.Port))
	upURL, err := url.Parse(target.Scheme + "://" + hostport + uri)
	if err != nil {
		fail(w, http.StatusBadRequest, "invalid_target_uri")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), h.cfg.TotalTimeout)
	defer cancel()
	var body io.Reader
	if r.Body != nil && r.Body != http.NoBody && r.ContentLength != 0 {
		body = http.MaxBytesReader(w, r.Body, h.cfg.MaxRequestBody)
	}
	out, err := http.NewRequestWithContext(ctx, r.Method, upURL.String(), body) // #nosec G704 -- validated target
	if err != nil {
		fail(w, http.StatusBadRequest, "invalid_request")
		return
	}
	out.ContentLength = r.ContentLength
	for k, vs := range r.Header {
		out.Header[k] = append([]string(nil), vs...)
	}
	stripHopByHop(out.Header)
	for _, k := range []string{"Authorization", "Cookie", "Host", "Forwarded", "X-Forwarded-For", "X-Forwarded-Host", "X-Forwarded-Proto", "X-Real-Ip", "Content-Length"} {
		out.Header.Del(k)
	}
	for k := range out.Header {
		if strings.HasPrefix(strings.ToLower(k), "x-openvms-") {
			delete(out.Header, k)
		}
	}
	if v := r.Header.Get(HeaderForwardAuthorization); v != "" {
		out.Header.Set("Authorization", v)
	}
	if v := r.Header.Get(HeaderForwardCookie); v != "" {
		out.Header.Set("Cookie", v)
	}
	out.Host = hostport

	client := &http.Client{
		Transport:     h.transport,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	// The target was validated above (literal private IPv4, allowed port) and the dialer re-checks it.
	resp, err := client.Do(out) // #nosec G704 -- see above
	if err != nil {
		var mbe *http.MaxBytesError
		switch {
		case errors.As(err, &mbe):
			fail(w, http.StatusRequestEntityTooLarge, "request_too_large")
		case errors.Is(err, context.DeadlineExceeded):
			fail(w, http.StatusGatewayTimeout, "camera_timeout")
		default:
			fail(w, http.StatusBadGateway, "camera_unreachable")
		}
		return
	}
	defer resp.Body.Close()
	if resp.ContentLength > h.cfg.MaxResponseBody {
		fail(w, http.StatusBadGateway, "response_too_large")
		return
	}

	hdr := w.Header()
	stripHopByHop(resp.Header)
	for k, vs := range resp.Header {
		lk := strings.ToLower(k)
		switch {
		case strings.HasPrefix(lk, "x-openvms-"):
			continue
		case lk == "set-cookie":
			for _, v := range vs {
				hdr.Add(HeaderSetCookie, v)
			}
		default:
			hdr[k] = append([]string(nil), vs...)
		}
	}
	hdr.Set(HeaderProxy, "1")
	w.WriteHeader(resp.StatusCode)
	if r.Method == http.MethodHead {
		return
	}
	h.stream(w, resp.Body)
}

// stream copies up to MaxResponseBody bytes, flushing as it goes so live streams keep moving. A body
// that outgrows the limit after the status was sent cannot be reported any other way: abort it.
func (h *Handler) stream(w http.ResponseWriter, src io.Reader) {
	flusher, _ := w.(http.Flusher)
	buf := make([]byte, 32<<10)
	var total int64
	for {
		n, err := src.Read(buf)
		if n > 0 {
			total += int64(n)
			if total > h.cfg.MaxResponseBody {
				panic(http.ErrAbortHandler)
			}
			if _, werr := w.Write(buf[:n]); werr != nil {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
		if err != nil {
			if !errors.Is(err, io.EOF) {
				panic(http.ErrAbortHandler)
			}
			return
		}
	}
}
