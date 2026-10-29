package camweb

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jdolan-exalink/openvms/internal/agent/camproxy"
)

const (
	cspValue    = "sandbox allow-scripts allow-forms allow-popups allow-modals; frame-ancestors 'self'"
	routePrefix = "/camproxy/"
	maxURILen   = 8 << 10
)

var allowedMethods = map[string]bool{
	http.MethodGet: true, http.MethodHead: true, http.MethodPost: true,
	http.MethodPut: true, http.MethodDelete: true, http.MethodOptions: true,
}

// Request headers that never travel to the camera.
var dropRequestHeaders = map[string]bool{
	"Cookie": true, "Authorization": true, "Host": true, "Origin": true, "Referer": true,
	"Accept-Encoding": true, "Content-Length": true, "Forwarded": true, "X-Forwarded-For": true,
	"X-Forwarded-Host": true, "X-Forwarded-Proto": true, "X-Real-Ip": true, "Proxy-Connection": true,
	"Keep-Alive": true, "Proxy-Authenticate": true, "Proxy-Authorization": true, "Te": true,
	"Trailer": true, "Transfer-Encoding": true, "Upgrade": true, "Connection": true,
	"Access-Control-Request-Method": true, "Access-Control-Request-Headers": true,
}

// Response headers copied from the camera. Anything not listed is dropped, so a camera can neither
// weaken the hardening below nor set state on the OpenVMS origin.
var passResponseHeaders = []string{"Content-Type", "Content-Disposition", "Content-Language", "Content-Range", "Accept-Ranges", "Etag", "Last-Modified", "Allow"}

func harden(w http.ResponseWriter) {
	h := w.Header()
	h.Set("Content-Security-Policy", cspValue)
	h.Del("X-Frame-Options") // the outer middleware sets DENY; the iframe is same-origin and frame-ancestors covers it
	h.Del("Strict-Transport-Security")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Cache-Control", "no-store")
	// Camera scripts run in an opaque origin, so their XHR/fetch to this route are cross-origin and need
	// CORS. No credentials are involved (the session id is in the URL) so a wildcard grants nothing new.
	h.Set("Access-Control-Allow-Origin", "*")
	h.Set("Access-Control-Expose-Headers", "*")
}

func plain(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	if msg != "" {
		_, _ = io.WriteString(w, msg+"\n")
	}
}

// cleanPath turns the escaped path after the session id into the path sent to the camera. Dot
// segments (also percent-encoded) and encoded separators are refused; empty segments collapse.
func cleanPath(raw string) (string, bool) {
	if len(raw) > maxURILen {
		return "", false
	}
	lower := strings.ToLower(raw)
	if strings.Contains(raw, `\`) || strings.Contains(lower, "%5c") || strings.Contains(lower, "%00") || strings.Contains(raw, "\x00") {
		return "", false
	}
	var keep []string
	for _, seg := range strings.Split(raw, "/") {
		if seg == "" {
			continue
		}
		dec, err := url.PathUnescape(seg)
		if err != nil || strings.Contains(dec, "/") || strings.ContainsAny(dec, "\r\n") {
			return "", false
		}
		if dec == ".." {
			return "", false
		}
		if dec == "." {
			continue
		}
		keep = append(keep, seg)
	}
	out := "/" + strings.Join(keep, "/")
	if len(keep) > 0 && strings.HasSuffix(raw, "/") {
		out += "/"
	}
	return out, true
}

func docDir(p string) string {
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[:i+1]
	}
	return "/"
}

// ServeHTTP serves /camproxy/{session}/... . It has no cookie authentication: the session id is the
// credential, and an unknown or expired one gets a bare 404.
func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	harden(w)
	rest, ok := strings.CutPrefix(r.URL.EscapedPath(), routePrefix)
	if !ok {
		plain(w, http.StatusNotFound, "")
		return
	}
	id, rawPath, _ := strings.Cut(rest, "/")
	rawPath = "/" + rawPath
	sess, ok := s.lookup(id)
	if !ok {
		plain(w, http.StatusNotFound, "")
		return
	}
	if !allowedMethods[r.Method] {
		w.Header().Set("Allow", "GET, HEAD, POST, PUT, DELETE, OPTIONS")
		plain(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	// Re-check the permission now and then: a session must not outlive the user's right to the server.
	s.mu.Lock()
	reauth := s.cfg.Now().Sub(sess.lastAuthz) >= s.cfg.ReauthEvery
	s.mu.Unlock()
	if reauth {
		if err := s.authorize(r.Context(), sess.actor, Camera{ID: sess.cameraID, ServerID: sess.serverID}); err != nil {
			s.remove(sess)
			plain(w, http.StatusNotFound, "")
			return
		}
		s.mu.Lock()
		sess.lastAuthz = s.cfg.Now()
		s.mu.Unlock()
	}
	prefix := strings.TrimSuffix(routePrefix, "/") + "/" + sess.id
	if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
		h := w.Header()
		h.Set("Access-Control-Allow-Methods", "GET, HEAD, POST, PUT, DELETE, OPTIONS")
		if v := r.Header.Get("Access-Control-Request-Headers"); v != "" && len(v) < 1024 {
			h.Set("Access-Control-Allow-Headers", v)
		}
		h.Set("Access-Control-Max-Age", "600")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	cleaned, ok := cleanPath(rawPath)
	if !ok {
		plain(w, http.StatusBadRequest, "invalid path")
		return
	}
	uri := cleaned
	if r.URL.RawQuery != "" {
		uri += "?" + r.URL.RawQuery
	}
	if len(uri) > maxURILen {
		plain(w, http.StatusRequestURITooLong, "")
		return
	}
	timer := time.NewTimer(s.cfg.QueueTimeout)
	defer timer.Stop()

	select {
	case sess.sem <- struct{}{}:
		defer func() { <-sess.sem }()
	case <-r.Context().Done():
		return
	case <-timer.C:
		w.Header().Set("Retry-After", "1")
		plain(w, http.StatusTooManyRequests, "too many concurrent requests")
		return
	}

	var data []byte
	var body io.Reader
	if r.Body != nil && r.Body != http.NoBody {
		if r.ContentLength > s.cfg.MaxRequestBody {
			plain(w, http.StatusRequestEntityTooLarge, "request too large")
			return
		}
		var err error
		data, err = io.ReadAll(io.LimitReader(r.Body, s.cfg.MaxRequestBody+1))
		if err != nil || int64(len(data)) > s.cfg.MaxRequestBody {
			plain(w, http.StatusRequestEntityTooLarge, "request too large")
			return
		}
		if len(data) > 0 { // buffered so the agent and camera get a Content-Length, not chunked framing
			body = bytes.NewReader(data)
		}
	}

	hdr := http.Header{}
	for k, vs := range r.Header {
		if dropRequestHeaders[k] || strings.HasPrefix(strings.ToLower(k), "x-openvms-") {
			continue
		}
		hdr[k] = append([]string(nil), vs...)
	}
	for _, v := range r.Header.Values("Connection") { // headers the browser marked hop-by-hop
		for _, name := range strings.Split(v, ",") {
			hdr.Del(strings.TrimSpace(name))
		}
	}
	hdr.Set(camproxy.HeaderTarget, sess.target.String())
	hdr.Set(camproxy.HeaderTargetURI, uri)
	hdr.Set("Accept-Encoding", "identity")
	if r.Header.Get("Origin") != "" {
		hdr.Set("Origin", sess.target.origin())
	}
	if r.Header.Get("Referer") != "" {
		hdr.Set("Referer", sess.target.origin()+"/")
	}
	// Only the credentials a camera understands go on. A bearer token (an OpenVMS API token) never does.
	if a := r.Header.Get("Authorization"); hasScheme(a, "basic") || hasScheme(a, "digest") {
		hdr.Set(camproxy.HeaderForwardAuthorization, a)
	}
	if c := sess.cookieHeader(cleaned); c != "" {
		hdr.Set(camproxy.HeaderForwardCookie, c)
	}

	ctx, cancel := context.WithTimeout(r.Context(), s.cfg.RequestTimeout)
	defer cancel()
	resp, err := s.fwd.Forward(ctx, sess.actor, sess.serverID, r.Method, hdr, body)
	if err != nil {
		var mbe *http.MaxBytesError
		switch {
		case errors.As(err, &mbe):
			plain(w, http.StatusRequestEntityTooLarge, "request too large")
		case errors.Is(err, context.DeadlineExceeded):
			plain(w, http.StatusGatewayTimeout, "the camera did not answer in time")
		default:
			plain(w, http.StatusBadGateway, "the camera could not be reached through the agent")
		}
		return
	}
	defer resp.Body.Close()
	if resp.Header.Get(camproxy.HeaderProxy) != "1" {
		plain(w, http.StatusBadGateway, "the agent could not serve the camera web view")
		return
	}
	if code := resp.Header.Get(camproxy.HeaderProxyError); code != "" {
		switch code {
		case "request_too_large":
			plain(w, http.StatusRequestEntityTooLarge, "request too large")
		case "camera_timeout":
			plain(w, http.StatusGatewayTimeout, "the camera did not answer in time")
		case "response_too_large":
			plain(w, http.StatusBadGateway, "the camera response is too large")
		default:
			plain(w, http.StatusBadGateway, "the camera could not be reached through the agent")
		}
		return
	}
	// If the camera challenges with 401 Basic Auth and we have stored camera credentials, retry once.
	if resp.StatusCode == http.StatusUnauthorized && sess.username != "" && sess.password != "" && r.Header.Get("Authorization") == "" {
		isBasic := false
		for _, v := range resp.Header.Values("Www-Authenticate") {
			if strings.HasPrefix(strings.ToLower(strings.TrimSpace(v)), "basic") {
				isBasic = true
				break
			}
		}
		if isBasic {
			authVal := "Basic " + base64.StdEncoding.EncodeToString([]byte(sess.username+":"+sess.password))
			retryHdr := hdr.Clone()
			retryHdr.Set(camproxy.HeaderForwardAuthorization, authVal)
			var retryBody io.Reader
			if len(data) > 0 {
				retryBody = bytes.NewReader(data)
			}
			if retryResp, retryErr := s.fwd.Forward(ctx, sess.actor, sess.serverID, r.Method, retryHdr, retryBody); retryErr == nil && retryResp.Header.Get(camproxy.HeaderProxy) == "1" && retryResp.Header.Get(camproxy.HeaderProxyError) == "" {
				resp.Body.Close()
				resp = retryResp
				defer resp.Body.Close()
			}
		}
	}
	sess.storeCookies(cleaned, resp.Header.Values(camproxy.HeaderSetCookie))

	out := w.Header()
	for _, k := range passResponseHeaders {
		if v := resp.Header.Values(k); len(v) > 0 {
			out[k] = append([]string(nil), v...)
		}
	}
	for _, v := range resp.Header.Values("Www-Authenticate") {
		out.Add("Www-Authenticate", scopeChallenge(v, sess.id))
	}
	if loc := resp.Header.Get("Location"); loc != "" {
		newLoc, ok := rewriteLocation(loc, prefix, sess.target)
		if !ok {
			out.Del("Content-Type")
			plain(w, http.StatusBadGateway, "the camera redirected to another address ("+"scheme, host or port"+"); open it with that address instead")
			return
		}
		out.Set("Location", newLoc)
	}

	rewrite := ""
	if r.Method != http.MethodHead && resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusNotModified {
		enc := strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Encoding")))
		if (enc == "" || enc == "identity") && (resp.ContentLength < 0 || resp.ContentLength <= s.cfg.RewriteLimit) {
			mt, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
			mt = strings.ToLower(mt)
			switch {
			case mt == "text/html":
				rewrite = "text/html"
			case mt == "text/css" || strings.HasSuffix(strings.ToLower(cleaned), ".css"):
				rewrite = "text/css"
			case isJS(mt, cleaned):
				rewrite = "application/javascript"
			}
		}
	}
	if rewrite == "" {
		if cl := resp.Header.Get("Content-Length"); cl != "" {
			out.Set("Content-Length", cl)
		}
		if enc := resp.Header.Get("Content-Encoding"); enc != "" {
			out.Set("Content-Encoding", enc)
		}
		w.WriteHeader(resp.StatusCode)
		if r.Method != http.MethodHead {
			s.stream(w, resp.Body, nil)
		}
		return
	}

	buf, err := io.ReadAll(io.LimitReader(resp.Body, s.cfg.RewriteLimit+1))
	if err != nil {
		plain(w, http.StatusBadGateway, "the camera response was interrupted")
		return
	}
	if int64(len(buf)) > s.cfg.RewriteLimit { // too big to rewrite: pass it through unchanged
		w.WriteHeader(resp.StatusCode)
		s.stream(w, resp.Body, buf)
		return
	}
	switch rewrite {
	case "text/html":
		buf = rewriteHTML(buf, prefix, docDir(cleaned), sess.target.scheme, sess.username, sess.password)
	case "text/css":
		buf = rewriteCSS(buf, prefix)
	case "application/javascript":
		buf = rewriteJS(buf)
	}
	out.Set("Content-Length", strconvItoa(len(buf)))
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(buf)
}

func isJS(mt, path string) bool {
	switch mt {
	case "application/javascript", "text/javascript", "application/x-javascript", "text/ecmascript":
		return true
	}
	p := strings.ToLower(path)
	return strings.HasSuffix(p, ".js") || strings.HasSuffix(p, ".mjs")
}

func hasScheme(header, scheme string) bool {
	return len(header) > len(scheme) && strings.EqualFold(header[:len(scheme)], scheme) && header[len(scheme)] == ' '
}

// stream copies the body (after head, bytes already read) up to MaxResponseBody and flushes as it
// goes. A body that outgrows the limit after the status was sent can only be cut: abort the response.
func (s *Service) stream(w http.ResponseWriter, src io.Reader, head []byte) {
	flusher, _ := w.(http.Flusher)
	var total int64
	write := func(p []byte) bool {
		total += int64(len(p))
		if total > s.cfg.MaxResponseBody {
			panic(http.ErrAbortHandler)
		}
		if _, err := w.Write(p); err != nil {
			return false
		}
		if flusher != nil {
			flusher.Flush()
		}
		return true
	}
	if len(head) > 0 && !write(head) {
		return
	}
	buf := make([]byte, 32<<10)
	for {
		n, err := src.Read(buf)
		if n > 0 && !write(buf[:n]) {
			return
		}
		if err != nil {
			if !errors.Is(err, io.EOF) {
				panic(http.ErrAbortHandler)
			}
			return
		}
	}
}
