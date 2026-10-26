// Package camweb shows a camera's own web interface inside OpenVMS. The browser never talks to the
// camera: it talks to /camproxy/{session}/ on the control plane, which forwards each request through
// the edge agent of the camera's server (the agent sits on the camera LAN).
//
// Security model, in short: the session id in the path is the only credential of that route, so it is
// 256 bits of randomness, bound server-side to one user, camera, scheme, host and port; the host comes
// from the camera record, never from the browser; the camera's cookies live in a server-side jar;
// every response is served with a CSP sandbox WITHOUT allow-same-origin so camera scripts run in an
// opaque origin and can never act as OpenVMS.
package camweb

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/netip"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/agent/camproxy"
	"github.com/jdolan-exalink/openvms/internal/authz"
)

var (
	// ErrNoAddress means the camera record gives no usable private IPv4 address (HTTP 404).
	ErrNoAddress = errors.New("camera has no usable address")
	// ErrNoAgent means the camera's server has no agent (HTTP 409).
	ErrNoAgent = errors.New("server has no agent")
	// ErrAgentUnsupported means the agent is too old to proxy camera web interfaces (HTTP 409).
	ErrAgentUnsupported = errors.New("agent does not support the camera web view")
	// ErrUnreachable means the agent could not reach the camera (HTTP 502).
	ErrUnreachable = errors.New("camera unreachable through the agent")
	// ErrBadRequest means the scheme or port are not acceptable (HTTP 400).
	ErrBadRequest = errors.New("invalid scheme or port")
)

// Forwarder sends one request to the agent route /v1/camera-web/ of a server and returns its answer.
// The caller closes the body.
type Forwarder interface {
	Forward(ctx context.Context, actor authz.Actor, serverID uuid.UUID, method string, header http.Header, body io.Reader) (*http.Response, error)
}

// Config bounds the service. The zero value is the production policy.
type Config struct {
	TTL                   time.Duration // sliding idle lifetime, default 30m
	AbsoluteTTL           time.Duration // hard lifetime, default 8h
	MaxPerUser            int           // default 4; the oldest session is replaced
	MaxTotal              int           // default 1024
	ReauthEvery           time.Duration // permission re-check interval, default 60s
	PerSessionConcurrency int           // default 8
	MaxRequestBody        int64         // default 10 MiB
	MaxResponseBody       int64         // default 64 MiB
	RewriteLimit          int64         // largest HTML/CSS body rewritten, default 5 MiB
	ProbeTimeout          time.Duration // reachability check when opening, default 8s
	RequestTimeout        time.Duration // default 65s
	Now                   func() time.Time
}

func (c *Config) defaults() {
	if c.TTL <= 0 {
		c.TTL = 30 * time.Minute
	}
	if c.AbsoluteTTL <= 0 {
		c.AbsoluteTTL = 8 * time.Hour
	}
	if c.MaxPerUser <= 0 {
		c.MaxPerUser = 4
	}
	if c.MaxTotal <= 0 {
		c.MaxTotal = 1024
	}
	if c.ReauthEvery <= 0 {
		c.ReauthEvery = 60 * time.Second
	}
	if c.PerSessionConcurrency <= 0 {
		c.PerSessionConcurrency = 8
	}
	if c.MaxRequestBody <= 0 {
		c.MaxRequestBody = 10 << 20
	}
	if c.MaxResponseBody <= 0 {
		c.MaxResponseBody = 64 << 20
	}
	if c.RewriteLimit <= 0 {
		c.RewriteLimit = 5 << 20
	}
	if c.ProbeTimeout <= 0 {
		c.ProbeTimeout = 8 * time.Second
	}
	if c.RequestTimeout <= 0 {
		c.RequestTimeout = 65 * time.Second
	}
	if c.Now == nil {
		c.Now = time.Now
	}
}

// Camera is what the caller resolved from the camera record. Host must come from the record.
type Camera struct {
	ID       uuid.UUID
	ServerID uuid.UUID
	Host     string
}

// Opened is the result of Open.
type Opened struct {
	URL       string // "/camproxy/{session}/"
	ExpiresAt time.Time
}

type target struct {
	scheme string
	host   string
	port   int
}

type session struct {
	hash      [32]byte
	id        string
	actor     authz.Actor
	cameraID  uuid.UUID
	serverID  uuid.UUID
	target    target
	jar       *cookiejar.Jar
	sem       chan struct{}
	created   time.Time
	expires   time.Time
	lastUsed  time.Time
	lastAuthz time.Time
}

// Service owns the sessions and serves /camproxy/.
type Service struct {
	cfg        Config
	fwd        Forwarder
	authorize  func(ctx context.Context, actor authz.Actor, serverID uuid.UUID) error
	agentReady func(ctx context.Context, actor authz.Actor, serverID uuid.UUID) error

	mu       sync.Mutex
	sessions map[[32]byte]*session
}

// NewService builds the service. authorize must enforce servers.manage and config-secret access on
// the server (it is called when a session opens and again at least every ReauthEvery while it is
// used); agentReady returns ErrNoAgent when the server has no agent. Expired sessions are removed
// lazily, so there is no goroutine to stop.
func NewService(cfg Config, fwd Forwarder, authorize, agentReady func(context.Context, authz.Actor, uuid.UUID) error) *Service {
	cfg.defaults()
	return &Service{cfg: cfg, fwd: fwd, authorize: authorize, agentReady: agentReady, sessions: map[[32]byte]*session{}}
}

func (s *Service) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.sessions)
}

// blockedPorts are never offered as camera ports: SSH and the agent's own listeners.
func blockedPort(p int) bool { return p == 22 || p == 7419 || p == 7443 }

// Open validates the request, checks the camera answers through the agent and creates a session.
func (s *Service) Open(ctx context.Context, actor authz.Actor, cam Camera, scheme string, port int) (Opened, error) {
	if err := s.authorize(ctx, actor, cam.ServerID); err != nil {
		return Opened{}, err
	}
	if (scheme != "http" && scheme != "https") || port < 1 || port > 65535 || blockedPort(port) {
		return Opened{}, ErrBadRequest
	}
	addr, err := netip.ParseAddr(cam.Host)
	if err != nil || !camproxy.AllowedAddr(addr) {
		return Opened{}, ErrNoAddress
	}
	if err := s.agentReady(ctx, actor, cam.ServerID); err != nil {
		return Opened{}, err
	}
	tgt := target{scheme: scheme, host: addr.String(), port: port}
	if err := s.probe(ctx, actor, cam.ServerID, tgt); err != nil {
		return Opened{}, err
	}

	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return Opened{}, err
	}
	id := hex.EncodeToString(raw[:])
	jar, _ := cookiejar.New(nil)
	now := s.cfg.Now()
	sess := &session{
		hash: sha256.Sum256([]byte(id)), id: id, actor: actor, cameraID: cam.ID, serverID: cam.ServerID,
		target: tgt, jar: jar, sem: make(chan struct{}, s.cfg.PerSessionConcurrency),
		created: now, expires: now.Add(s.cfg.TTL), lastUsed: now, lastAuthz: now,
	}
	s.mu.Lock()
	s.sweepLocked(now)
	for s.countUserLocked(actor.UserID) >= s.cfg.MaxPerUser {
		s.evictOldestLocked(func(o *session) bool { return o.actor.UserID == actor.UserID })
	}
	for len(s.sessions) >= s.cfg.MaxTotal {
		s.evictOldestLocked(func(*session) bool { return true })
	}
	s.sessions[sess.hash] = sess
	s.mu.Unlock()
	return Opened{URL: "/camproxy/" + id + "/", ExpiresAt: sess.expires}, nil
}

func (s *Service) probe(ctx context.Context, actor authz.Actor, serverID uuid.UUID, tgt target) error {
	ctx, cancel := context.WithTimeout(ctx, s.cfg.ProbeTimeout)
	defer cancel()
	h := http.Header{}
	h.Set(camproxy.HeaderTarget, tgt.String())
	h.Set(camproxy.HeaderTargetURI, "/")
	h.Set("Accept-Encoding", "identity")
	resp, err := s.fwd.Forward(ctx, actor, serverID, http.MethodGet, h, nil)
	if err != nil {
		return ErrUnreachable
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.Header.Get(camproxy.HeaderProxy) != "1" {
		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
			return ErrAgentUnsupported
		}
		return ErrUnreachable
	}
	if resp.Header.Get(camproxy.HeaderProxyError) != "" {
		return ErrUnreachable
	}
	return nil
}

func (t target) String() string {
	return t.scheme + "://" + t.host + ":" + strconv.Itoa(t.port)
}

// origin is how the camera sees itself (default ports omitted).
func (t target) origin() string {
	if (t.scheme == "http" && t.port == 80) || (t.scheme == "https" && t.port == 443) {
		return t.scheme + "://" + t.host
	}
	return t.String()
}

func (s *Service) sweepLocked(now time.Time) {
	for k, sess := range s.sessions {
		if s.expiredLocked(sess, now) {
			delete(s.sessions, k)
		}
	}
}

func (s *Service) expiredLocked(sess *session, now time.Time) bool {
	return !now.Before(sess.expires) || !now.Before(sess.created.Add(s.cfg.AbsoluteTTL))
}

func (s *Service) countUserLocked(user uuid.UUID) int {
	n := 0
	for _, sess := range s.sessions {
		if sess.actor.UserID == user {
			n++
		}
	}
	return n
}

func (s *Service) evictOldestLocked(match func(*session) bool) {
	var oldest *session
	for _, sess := range s.sessions {
		if match(sess) && (oldest == nil || sess.lastUsed.Before(oldest.lastUsed)) {
			oldest = sess
		}
	}
	if oldest != nil {
		delete(s.sessions, oldest.hash)
	}
}

// lookup finds a live session by its id and slides its expiry. The map is keyed by the SHA-256 of the
// id, so no comparison of the secret itself depends on how many leading bytes match.
func (s *Service) lookup(id string) (*session, bool) {
	if len(id) != 64 {
		return nil, false
	}
	if _, err := hex.DecodeString(id); err != nil {
		return nil, false
	}
	key := sha256.Sum256([]byte(id))
	now := s.cfg.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[key]
	if !ok {
		return nil, false
	}
	if s.expiredLocked(sess, now) {
		delete(s.sessions, key)
		return nil, false
	}
	sess.lastUsed = now
	sess.expires = now.Add(s.cfg.TTL)
	if abs := sess.created.Add(s.cfg.AbsoluteTTL); sess.expires.After(abs) {
		sess.expires = abs
	}
	return sess, true
}

func (s *Service) remove(sess *session) {
	s.mu.Lock()
	delete(s.sessions, sess.hash)
	s.mu.Unlock()
}
