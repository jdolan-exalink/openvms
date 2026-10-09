package relay

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"
)

var (
	ErrTokenExpired  = errors.New("relay: token expired")
	ErrTokenNotFound = errors.New("relay: invalid token")
)

// Session represents an authorized relay session forwarding traffic from client to node.
type Session struct {
	Token            string
	NodeAddress      string
	CameraID         string
	ClientDeviceID   string
	CreatedAt        time.Time
	ExpiresAt        time.Time
	BytesTransferred int64
}

// Config configures the OpenVMS Media Relay.
type Config struct {
	ListenAddr string
	Log        *slog.Logger
	TokenTTL   time.Duration
}

// Server handles authenticated relay proxies between desktop clients and edge nodes.
type Server struct {
	cfg      Config
	mu       sync.RWMutex
	sessions map[string]*Session
	listener net.Listener
}

// NewServer creates a new Media Relay server.
func NewServer(cfg Config) *Server {
	if cfg.TokenTTL <= 0 {
		cfg.TokenTTL = 1 * time.Hour
	}
	if cfg.Log == nil {
		cfg.Log = slog.Default()
	}

	return &Server{
		cfg:      cfg,
		sessions: make(map[string]*Session),
	}
}

// AuthorizeSession generates a short-lived token granting relay access to target node.
func (s *Server) AuthorizeSession(nodeAddress, cameraID, clientDeviceID string) (*Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	token := hex.EncodeToString(b)

	now := time.Now()
	sess := &Session{
		Token:          token,
		NodeAddress:    nodeAddress,
		CameraID:       cameraID,
		ClientDeviceID: clientDeviceID,
		CreatedAt:      now,
		ExpiresAt:      now.Add(s.cfg.TokenTTL),
	}

	s.sessions[token] = sess
	return sess, nil
}

// ValidateToken verifies a relay token.
func (s *Server) ValidateToken(token string) (*Session, error) {
	s.mu.RLock()
	sess, ok := s.sessions[token]
	s.mu.RUnlock()

	if !ok {
		return nil, ErrTokenNotFound
	}
	if time.Now().After(sess.ExpiresAt) {
		s.mu.Lock()
		delete(s.sessions, token)
		s.mu.Unlock()
		return nil, ErrTokenExpired
	}
	return sess, nil
}

// Handler returns the HTTP relay handler that proxies media streams.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/relay/stream", s.handleStream)
	return mux
}

func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	sess, err := s.ValidateToken(token)
	if err != nil {
		http.Error(w, "unauthorized relay token", http.StatusUnauthorized)
		return
	}

	targetURL, err := url.Parse(sess.NodeAddress)
	if err != nil {
		http.Error(w, "bad target node url", http.StatusBadGateway)
		return
	}

	req, err := http.NewRequestWithContext(r.Context(), r.Method, targetURL.String(), r.Body)
	if err != nil {
		http.Error(w, "failed to build upstream request", http.StatusInternalServerError)
		return
	}
	for k, vv := range r.Header {
		for _, v := range vv {
			req.Header.Add(k, v)
		}
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		s.cfg.Log.Warn("relay upstream connect failed", "target", targetURL.String(), "error", err)
		http.Error(w, "upstream node unreachable", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	for k, vv := range resp.Header {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)

	copied, _ := io.Copy(w, resp.Body)
	s.mu.Lock()
	sess.BytesTransferred += copied
	s.mu.Unlock()
}

// Start begins serving relay connections.
func (s *Server) Start(addr string) error {
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	s.listener = lis
	s.cfg.Log.Info("openvms media relay server listening", "addr", addr)
	srv := &http.Server{Handler: s.Handler()}
	return srv.Serve(lis)
}

// Close terminates the relay listener.
func (s *Server) Close() error {
	if s.listener != nil {
		return s.listener.Close()
	}
	return nil
}
