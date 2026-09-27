// Package frigate is the Frigate Adapter Layer (PRD §19). The rest of the VMS talks to
// Frigate only through Adapter; version differences live in the implementations.
package frigate

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"sync"
	"time"
)

// AuthMode says how the VMS authenticates against a Frigate server.
type AuthMode string

const (
	// AuthCredentials logs in with a Frigate user on the authenticated port (8971).
	AuthCredentials AuthMode = "credentials"
	// AuthNone talks to a Frigate that does not ask for a login: the internal port 5000
	// or 8971 with auth disabled. Only for trusted networks: anyone reaching the port
	// has full access to that Frigate.
	AuthNone AuthMode = "none"
)

// ConnInfo is everything needed to reach one Frigate server.
type ConnInfo struct {
	BaseURL       string
	AuthMode      AuthMode
	Username      string
	Password      string
	TLSSkipVerify bool
}

// Mode returns the auth mode, defaulting to credentials.
func (i ConnInfo) Mode() AuthMode {
	if i.AuthMode == "" {
		return AuthCredentials
	}
	return i.AuthMode
}

var (
	// ErrUnauthorized means Frigate rejected the service account.
	ErrUnauthorized = errors.New("frigate rejected the credentials")
	// ErrUnauthenticatedPort means credentials were given for Frigate's internal port 5000,
	// which has no login. Register it with auth mode "none" instead.
	ErrUnauthenticatedPort = errors.New(`port 5000 is Frigate's unauthenticated internal port: use the authenticated port 8971 with credentials, or auth mode "none"`)
	// ErrNotFound means Frigate answered 404 for the resource.
	ErrNotFound = errors.New("not found in frigate")
)

// ValidateBaseURL checks scheme and host. Port 5000 is only accepted without credentials.
func ValidateBaseURL(raw string, mode AuthMode) (*url.URL, error) {
	u, err := url.Parse(strings.TrimRight(strings.TrimSpace(raw), "/"))
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, fmt.Errorf("invalid Frigate URL %q: use http(s)://host:port", raw)
	}
	if u.Port() == "5000" && mode != AuthNone {
		return nil, ErrUnauthenticatedPort
	}
	if u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return nil, fmt.Errorf("invalid Frigate URL %q: remove query, fragment or credentials", raw)
	}
	return u, nil
}

// client performs authenticated requests, logging in lazily and again after a 401.
type client struct {
	base *url.URL
	info ConnInfo
	http *http.Client
	// stream has no overall timeout, for media that is copied while it plays.
	stream *http.Client
	tls    *tls.Config

	mu       sync.Mutex
	loggedIn bool
}

func newClient(info ConnInfo) (*client, error) {
	base, err := ValidateBaseURL(info.BaseURL, info.Mode())
	if err != nil {
		return nil, err
	}
	jar, _ := cookiejar.New(nil)
	tlsCfg := &tls.Config{
		MinVersion: tls.VersionTLS12,
		// Frigate ships a self-signed certificate on 8971; operators opt in per server.
		InsecureSkipVerify: info.TLSSkipVerify, //nolint:gosec // explicit per-server opt-in
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = (&net.Dialer{Timeout: 5 * time.Second}).DialContext
	transport.MaxIdleConnsPerHost = 16
	transport.TLSClientConfig = tlsCfg
	return &client{
		base:   base,
		info:   info,
		http:   &http.Client{Jar: jar, Transport: transport, Timeout: 15 * time.Second},
		stream: &http.Client{Jar: jar, Transport: transport},
		tls:    tlsCfg,
	}, nil
}

func (c *client) login(ctx context.Context) error {
	if c.info.Mode() == AuthNone {
		c.loggedIn = true
		return nil
	}
	body, _ := json.Marshal(map[string]string{"user": c.info.Username, "password": c.info.Password})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base.JoinPath("/api/login").String(), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("frigate login: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return ErrUnauthorized
	case resp.StatusCode >= 300:
		return fmt.Errorf("frigate login: unexpected status %d", resp.StatusCode)
	}
	c.loggedIn = true
	return nil
}

// ensureLogin logs in unless a session is already open.
func (c *client) ensureLogin(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.loggedIn {
		return nil
	}
	return c.login(ctx)
}

func (c *client) invalidate() {
	c.mu.Lock()
	c.loggedIn = false
	c.mu.Unlock()
}

// do sends a request built by mk, logging in first and retrying once after a 401.
// The caller closes the body of the returned response.
func (c *client) do(ctx context.Context, hc *http.Client, mk func() (*http.Request, error)) (*http.Response, error) {
	for attempt := 0; attempt < 2; attempt++ {
		if err := c.ensureLogin(ctx); err != nil {
			return nil, err
		}
		req, err := mk()
		if err != nil {
			return nil, err
		}
		resp, err := hc.Do(req)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode == http.StatusUnauthorized {
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if attempt == 0 && c.info.Mode() == AuthCredentials {
				c.invalidate()
				continue
			}
			return nil, ErrUnauthorized
		}
		return resp, nil
	}
	return nil, ErrUnauthorized
}

func (c *client) url(path string, query url.Values) string {
	u := c.base.JoinPath(path)
	u.RawQuery = query.Encode()
	return u.String()
}

// send performs a request with an optional JSON body and returns the response body.
func (c *client) send(ctx context.Context, method, path string, query url.Values, payload any) ([]byte, error) {
	var raw []byte
	if payload != nil {
		var err error
		if raw, err = json.Marshal(payload); err != nil {
			return nil, err
		}
	}
	resp, err := c.do(ctx, c.http, func() (*http.Request, error) {
		var body io.Reader
		if raw != nil {
			body = bytes.NewReader(raw)
		}
		req, err := http.NewRequestWithContext(ctx, method, c.url(path, query), body)
		if err == nil && raw != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		return req, err
	})
	if err != nil {
		return nil, fmt.Errorf("frigate %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, err
	}
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return nil, fmt.Errorf("frigate %s %s: %w", method, path, ErrNotFound)
	case resp.StatusCode >= 300:
		return nil, fmt.Errorf("frigate %s %s: status %d: %s", method, path, resp.StatusCode, snippet(body))
	}
	return body, nil
}

func (c *client) get(ctx context.Context, path string, query url.Values) ([]byte, error) {
	return c.send(ctx, http.MethodGet, path, query, nil)
}

func (c *client) getJSON(ctx context.Context, path string, query url.Values, out any) error {
	body, err := c.get(ctx, path, query)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("decode frigate %s: %w", path, err)
	}
	return nil
}

// Open streams a GET response (media). Statuses other than 401 are returned as they are
// so the caller can relay them; the caller closes the body.
func (c *client) Open(ctx context.Context, path string, query url.Values, header http.Header) (*http.Response, error) {
	return c.do(ctx, c.stream, func() (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url(path, query), nil)
		if err != nil {
			return nil, err
		}
		for _, k := range []string{"Range", "If-None-Match", "If-Modified-Since", "Accept"} {
			if v := header.Get(k); v != "" {
				req.Header.Set(k, v)
			}
		}
		return req, nil
	})
}

// WebSocket returns the ws(s) URL for path and the headers that authenticate it
// (Frigate's session cookie in credentials mode).
func (c *client) WebSocket(ctx context.Context, path string, query url.Values) (string, http.Header, *tls.Config, error) {
	if err := c.ensureLogin(ctx); err != nil {
		return "", nil, nil, err
	}
	u := c.base.JoinPath(path)
	u.RawQuery = query.Encode()
	h := http.Header{}
	var parts []string
	for _, ck := range c.http.Jar.Cookies(c.base) {
		parts = append(parts, ck.Name+"="+ck.Value)
	}
	if len(parts) > 0 {
		h.Set("Cookie", strings.Join(parts, "; "))
	}
	if u.Scheme == "https" {
		u.Scheme = "wss"
	} else {
		u.Scheme = "ws"
	}
	return u.String(), h, c.tls, nil
}

func snippet(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	return s
}
