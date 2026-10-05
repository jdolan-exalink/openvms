package mediasession

import (
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"
)

var (
	ErrSessionNotFound = errors.New("mediasession: session not found")
	ErrSessionClosed   = errors.New("mediasession: session is closed")
)

// SessionState indicates the lifecycle status of a streaming session.
type SessionState string

const (
	StateActive    SessionState = "active"
	StatePaused    SessionState = "paused"
	StateMigrating SessionState = "migrating"
	StateClosed    SessionState = "closed"
)

// TransportProtocol defines the underlying transport used for the stream.
type TransportProtocol string

const (
	TransportRTPUDP TransportProtocol = "rtp_udp"
	TransportRTPTCP TransportProtocol = "rtp_tcp"
	TransportWebRTC TransportProtocol = "webrtc"
	TransportQUIC   TransportProtocol = "quic"
)

// ConnectionPath represents the network topology path.
type ConnectionPath string

const (
	PathDirectLAN ConnectionPath = "direct_lan"
	PathDirectWAN ConnectionPath = "direct_wan"
	PathP2PWAN    ConnectionPath = "p2p_wan"
	PathRelay     ConnectionPath = "relay"
)

// StreamProfile indicates stream resolution/quality.
type StreamProfile string

const (
	ProfileMain   StreamProfile = "main"
	ProfileSub    StreamProfile = "sub"
	ProfileMobile StreamProfile = "mobile"
)

// Session represents a logical media streaming session independent of physical transport.
type Session struct {
	mu             sync.RWMutex
	ID             string            `json:"session_id"`
	CameraID       string            `json:"camera_id"`
	NodeID         string            `json:"node_id"`
	ClientDeviceID string            `json:"client_device_id"`
	Profile        StreamProfile     `json:"profile"`
	Transport      TransportProtocol `json:"transport"`
	Path           ConnectionPath    `json:"path"`
	Codec          string            `json:"codec"`
	StartedAt      time.Time         `json:"started_at"`
	LastActiveAt   time.Time         `json:"last_active_at"`
	BytesSent      int64             `json:"bytes_sent"`
	DroppedPackets int64             `json:"dropped_packets"`
	RTTMs          float32           `json:"rtt_ms"`
	State          SessionState      `json:"state"`
}

// SessionView is a snapshot copy of a Session's current attributes.
type SessionView struct {
	ID             string            `json:"session_id"`
	CameraID       string            `json:"camera_id"`
	NodeID         string            `json:"node_id"`
	ClientDeviceID string            `json:"client_device_id"`
	Profile        StreamProfile     `json:"profile"`
	Transport      TransportProtocol `json:"transport"`
	Path           ConnectionPath    `json:"path"`
	Codec          string            `json:"codec"`
	StartedAt      time.Time         `json:"started_at"`
	LastActiveAt   time.Time         `json:"last_active_at"`
	BytesSent      int64             `json:"bytes_sent"`
	DroppedPackets int64             `json:"dropped_packets"`
	RTTMs          float32           `json:"rtt_ms"`
	State          SessionState      `json:"state"`
}

func (s *Session) Snapshot() SessionView {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return SessionView{
		ID:             s.ID,
		CameraID:       s.CameraID,
		NodeID:         s.NodeID,
		ClientDeviceID: s.ClientDeviceID,
		Profile:        s.Profile,
		Transport:      s.Transport,
		Path:           s.Path,
		Codec:          s.Codec,
		StartedAt:      s.StartedAt,
		LastActiveAt:   s.LastActiveAt,
		BytesSent:      s.BytesSent,
		DroppedPackets: s.DroppedPackets,
		RTTMs:          s.RTTMs,
		State:          s.State,
	}
}

// CreateSessionInput contains initialization attributes for a new stream session.
type CreateSessionInput struct {
	SessionID      string
	CameraID       string
	NodeID         string
	ClientDeviceID string
	Profile        StreamProfile
	Transport      TransportProtocol
	Path           ConnectionPath
	Codec          string
}

// Manager coordinates active media sessions, migrations, and adaptive quality changes.
type Manager struct {
	mu       sync.RWMutex
	sessions map[string]*Session
	ttl      time.Duration
	stopCh   chan struct{}
}

// NewManager initializes a new SessionManager.
func NewManager(ttl time.Duration) *Manager {
	if ttl <= 0 {
		ttl = 15 * time.Minute
	}
	m := &Manager{
		sessions: make(map[string]*Session),
		ttl:      ttl,
		stopCh:   make(chan struct{}),
	}
	go m.cleanupLoop()
	return m
}

// Close terminates the manager and background cleanup routine.
func (m *Manager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	select {
	case <-m.stopCh:
		return
	default:
		close(m.stopCh)
	}
}

// CreateSession allocates a new MediaSession.
func (m *Manager) CreateSession(in CreateSessionInput) *Session {
	m.mu.Lock()
	defer m.mu.Unlock()

	id := in.SessionID
	if id == "" {
		id = uuid.NewString()
	}

	profile := in.Profile
	if profile == "" {
		profile = ProfileMain
	}

	transport := in.Transport
	if transport == "" {
		transport = TransportRTPUDP
	}

	path := in.Path
	if path == "" {
		path = PathDirectLAN
	}

	codec := in.Codec
	if codec == "" {
		codec = "h264"
	}

	now := time.Now()
	s := &Session{
		ID:             id,
		CameraID:       in.CameraID,
		NodeID:         in.NodeID,
		ClientDeviceID: in.ClientDeviceID,
		Profile:        profile,
		Transport:      transport,
		Path:           path,
		Codec:          codec,
		StartedAt:      now,
		LastActiveAt:   now,
		State:          StateActive,
	}

	m.sessions[id] = s
	return s
}

// GetSession retrieves an active session by ID.
func (m *Manager) GetSession(id string) (*Session, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.sessions[id]
	return s, ok
}

// UpdateProfile executes an adaptive profile change (e.g. sub to main on grid tile maximize).
func (m *Manager) UpdateProfile(sessionID string, newProfile StreamProfile) error {
	m.mu.RLock()
	s, ok := m.sessions[sessionID]
	m.mu.RUnlock()

	if !ok {
		return ErrSessionNotFound
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.State == StateClosed {
		return ErrSessionClosed
	}

	s.Profile = newProfile
	s.LastActiveAt = time.Now()
	return nil
}

// MigratePath migrates the session to a different network path without closing the player (e.g. LAN -> WAN -> Relay).
func (m *Manager) MigratePath(sessionID string, newPath ConnectionPath, newTransport TransportProtocol) error {
	m.mu.RLock()
	s, ok := m.sessions[sessionID]
	m.mu.RUnlock()

	if !ok {
		return ErrSessionNotFound
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.State == StateClosed {
		return ErrSessionClosed
	}

	s.State = StateMigrating
	s.Path = newPath
	s.Transport = newTransport
	s.LastActiveAt = time.Now()
	s.State = StateActive

	return nil
}

// RecordTelemetry records incoming network telemetry for an active session.
func (m *Manager) RecordTelemetry(sessionID string, bytesDelta int64, droppedPacketsDelta int64, rttMs float32) error {
	m.mu.RLock()
	s, ok := m.sessions[sessionID]
	m.mu.RUnlock()

	if !ok {
		return ErrSessionNotFound
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.BytesSent += bytesDelta
	s.DroppedPackets += droppedPacketsDelta
	s.RTTMs = rttMs
	s.LastActiveAt = time.Now()

	return nil
}

// CloseSession gracefully terminates a session.
func (m *Manager) CloseSession(sessionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	s, ok := m.sessions[sessionID]
	if !ok {
		return ErrSessionNotFound
	}

	s.mu.Lock()
	s.State = StateClosed
	s.LastActiveAt = time.Now()
	s.mu.Unlock()

	delete(m.sessions, sessionID)
	return nil
}

// ListActiveSessions returns a list of current active session views.
func (m *Manager) ListActiveSessions() []SessionView {
	m.mu.RLock()
	defer m.mu.RUnlock()

	out := make([]SessionView, 0, len(m.sessions))
	for _, s := range m.sessions {
		out = append(out, s.Snapshot())
	}
	return out
}

// ActiveCount returns the number of currently tracked media sessions.
func (m *Manager) ActiveCount() int {
	if m == nil {
		return 0
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.sessions)
}

func (m *Manager) cleanupLoop() {
	ticker := time.NewTicker(m.ttl / 2)
	defer ticker.Stop()

	for {
		select {
		case <-m.stopCh:
			return
		case <-ticker.C:
			m.mu.Lock()
			now := time.Now()
			for id, s := range m.sessions {
				s.mu.RLock()
				inactive := now.Sub(s.LastActiveAt) > m.ttl
				closed := s.State == StateClosed
				s.mu.RUnlock()

				if inactive || closed {
					delete(m.sessions, id)
				}
			}
			m.mu.Unlock()
		}
	}
}
