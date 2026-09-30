package realtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/authz"
)

var (
	// ErrClosed ends a Subscription whose Hub or owner closed it.
	ErrClosed = errors.New("realtime: closed")
	// ErrSlowConsumer ends a Subscription that did not drain its buffer in time.
	ErrSlowConsumer = errors.New("realtime: slow consumer")
	// ErrTooManyConnections refuses a Subscribe over the per-user limit.
	ErrTooManyConnections = errors.New("realtime: too many connections")
)

// Authorizer decides whether an actor may see a message about scope. Implementations must
// mirror the authorization of the corresponding list endpoints.
type Authorizer interface {
	Allow(ctx context.Context, actor authz.Actor, s Scope) (bool, error)
}

// HubConfig configures NewHub.
type HubConfig struct {
	Authorizer Authorizer
	Log        *slog.Logger
	// Routes decode NATS messages for Dispatch.
	Routes []Route
	// Buffer is the per-connection queue length; overflow closes the connection. Default 64.
	Buffer int
	// MaxPerUser caps concurrent connections of one user. Zero means 5.
	MaxPerUser int
	// Ring stores recent messages for resume. Default is a 5-minute RingBuffer.
	Ring *RingBuffer
}

// Hub fans decoded messages out to subscriptions of this API instance.
type Hub struct {
	cfg HubConfig

	mu                 sync.Mutex
	closed             bool
	subs               map[*Subscription]struct{}
	perUser            map[uuid.UUID]int
	serverOfflineUntil map[uuid.UUID]time.Time
}

func NewHub(cfg HubConfig) *Hub {
	if cfg.Buffer <= 0 {
		cfg.Buffer = 64
	}
	if cfg.MaxPerUser <= 0 {
		cfg.MaxPerUser = 5
	}
	if cfg.Log == nil {
		cfg.Log = slog.Default()
	}
	if cfg.Ring == nil {
		cfg.Ring = NewRingBuffer()
	}
	return &Hub{
		cfg:                cfg,
		subs:               map[*Subscription]struct{}{},
		perUser:            map[uuid.UUID]int{},
		serverOfflineUntil: map[uuid.UUID]time.Time{},
	}
}

// Ring returns the resume ring buffer.
func (h *Hub) Ring() *RingBuffer {
	return h.cfg.Ring
}

// Connections reports the number of live subscriptions.
func (h *Hub) Connections() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subs)
}

// Subscribe registers a connection for actor.
func (h *Hub) Subscribe(actor authz.Actor) (*Subscription, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return nil, ErrClosed
	}
	if h.perUser[actor.UserID] >= h.cfg.MaxPerUser {
		return nil, ErrTooManyConnections
	}
	s := &Subscription{
		hub:              h,
		actor:            actor,
		in:               make(chan Message, h.cfg.Buffer),
		done:             make(chan struct{}),
		lastCamTimes:     make(map[uuid.UUID]time.Time),
		pendingCamMsg:    make(map[uuid.UUID]Message),
		pendingCamTimers: make(map[uuid.UUID]*time.Timer),
	}
	h.subs[s] = struct{}{}
	h.perUser[actor.UserID]++
	return s, nil
}

// Dispatch decodes a raw NATS message and publishes it. Unroutable or malformed messages are
// dropped (logged for the latter): one bad payload must never stop the feed.
func (h *Hub) Dispatch(subject string, data []byte) {
	h.DispatchStream("", 0, subject, data)
}

// DispatchStream decodes raw NATS messages for a given stream and sequence, sets envelope metadata, and publishes them.
func (h *Hub) DispatchStream(stream string, seq uint64, subject string, data []byte) {
	msgs, err := DecodeAll(h.cfg.Routes, subject, data)
	if err != nil {
		if !errors.Is(err, errNoRoute) {
			h.cfg.Log.Warn("realtime: dropped message", "subject", subject, "error", err)
		}
		return
	}
	for _, m := range msgs {
		m.Stream = stream
		m.Seq = seq
		if stream != "" && seq > 0 {
			m.Envelope.ID = fmt.Sprintf("%s:%d", stream, seq)
		}
		if h.cfg.Ring != nil {
			h.cfg.Ring.Add(m)
		}
		h.Publish(m)
	}
}

// Publish offers m to every subscription of its tenant. It never blocks: a subscription whose
// buffer is full is closed with ErrSlowConsumer.
func (h *Hub) Publish(m Message) {
	now := time.Now()
	// Server offline suppression: when server status is offline, suppress per-camera status for 30s
	if m.Envelope.Type == TypeServerStatus || m.Envelope.Type == TypeServerStatusChanged {
		var p statusPayload
		if err := json.Unmarshal(m.Envelope.Data, &p); err == nil && p.To == "offline" && m.Envelope.ServerID != nil {
			h.mu.Lock()
			if h.serverOfflineUntil == nil {
				h.serverOfflineUntil = make(map[uuid.UUID]time.Time)
			}
			h.serverOfflineUntil[*m.Envelope.ServerID] = now.Add(30 * time.Second)
			h.mu.Unlock()
		}
	} else if m.Envelope.Type == TypeCameraStatusChanged && m.Envelope.ServerID != nil {
		h.mu.Lock()
		until, ok := h.serverOfflineUntil[*m.Envelope.ServerID]
		h.mu.Unlock()
		if ok && now.Before(until) {
			// Suppressed while server is offline
			return
		}
	}

	h.mu.Lock()
	var slow []*Subscription
	for s := range h.subs {
		// Tenant isolation is enforced here, independent of the Authorizer. Platform actors
		// (no tenant) fall through to the Authorizer, which scopes them by their grants.
		if s.actor.TenantID != nil && *s.actor.TenantID != m.Envelope.TenantID {
			continue
		}
		if !s.matchesSite(m.Envelope.SiteID) {
			continue
		}
		if !s.matchesTopic(m.Envelope.Type) {
			continue
		}
		if !s.deliver(m) {
			slow = append(slow, s)
		}
	}
	h.mu.Unlock()
	for _, s := range slow {
		h.cfg.Log.Warn("realtime: closing slow consumer", "user_id", s.actor.UserID)
		s.closeWith(ErrSlowConsumer)
	}
}

// Close ends every subscription and refuses new ones (server shutdown).
func (h *Hub) Close() {
	h.mu.Lock()
	h.closed = true
	subs := make([]*Subscription, 0, len(h.subs))
	for s := range h.subs {
		subs = append(subs, s)
	}
	h.mu.Unlock()
	for _, s := range subs {
		s.closeWith(ErrClosed)
	}
}

func (h *Hub) remove(s *Subscription) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.subs[s]; !ok {
		return
	}
	delete(h.subs, s)
	if h.perUser[s.actor.UserID]--; h.perUser[s.actor.UserID] <= 0 {
		delete(h.perUser, s.actor.UserID)
	}
}

// Subscription is one connection's view of the feed.
type Subscription struct {
	hub   *Hub
	actor authz.Actor
	in    chan Message

	once sync.Once
	done chan struct{}
	err  error

	mu               sync.Mutex
	topics           map[string]struct{}
	siteIDs          map[uuid.UUID]struct{}
	lastCamTimes     map[uuid.UUID]time.Time
	pendingCamMsg    map[uuid.UUID]Message
	pendingCamTimers map[uuid.UUID]*time.Timer
}

func (s *Subscription) matchesSite(siteID *uuid.UUID) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.siteIDs) == 0 {
		return true
	}
	if siteID == nil {
		return true
	}
	_, ok := s.siteIDs[*siteID]
	return ok
}

func (s *Subscription) matchesTopic(msgType string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.topics) == 0 {
		switch msgType {
		case TypeEventCreated, TypeServerStatus, TypeAlarmUpdated, TypeNotificationCreated, TypeCameraStatusChanged:
			return true
		default:
			return false
		}
	}
	for topic := range s.topics {
		switch topic {
		case "camera":
			if msgType == TypeCameraStatusChanged || msgType == "camera.status" {
				return true
			}
		case "server":
			if msgType == TypeServerStatus || msgType == TypeServerStatusChanged {
				return true
			}
		case "alarm":
			if strings.HasPrefix(msgType, "alarm.") {
				return true
			}
		case "object":
			if msgType == TypeEventCreated || msgType == TypeObjectDetected {
				return true
			}
		case "notification":
			if msgType == TypeNotificationCreated {
				return true
			}
		}
	}
	return false
}

// SetTopics opts the subscription into specific topics.
func (s *Subscription) SetTopics(topics []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.topics = make(map[string]struct{}, len(topics))
	for _, t := range topics {
		s.topics[t] = struct{}{}
	}
}

// SetSiteIDs narrows the subscription to specific site IDs.
func (s *Subscription) SetSiteIDs(siteIDs []uuid.UUID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.siteIDs = make(map[uuid.UUID]struct{}, len(siteIDs))
	for _, id := range siteIDs {
		s.siteIDs[id] = struct{}{}
	}
}

func (s *Subscription) enqueue(m Message) bool {
	select {
	case <-s.done:
		return false
	case s.in <- m:
		return true
	default:
		return false
	}
}

func (s *Subscription) deliver(m Message) bool {
	if m.Envelope.Type == TypeCameraStatusChanged && m.Envelope.CameraID != nil {
		camID := *m.Envelope.CameraID
		s.mu.Lock()
		if s.lastCamTimes == nil {
			s.lastCamTimes = make(map[uuid.UUID]time.Time)
			s.pendingCamMsg = make(map[uuid.UUID]Message)
			s.pendingCamTimers = make(map[uuid.UUID]*time.Timer)
		}
		last, ok := s.lastCamTimes[camID]
		now := time.Now()
		if ok && now.Sub(last) < 500*time.Millisecond {
			s.pendingCamMsg[camID] = m
			if _, hasTimer := s.pendingCamTimers[camID]; !hasTimer {
				rem := 500*time.Millisecond - now.Sub(last)
				s.pendingCamTimers[camID] = time.AfterFunc(rem, func() {
					s.mu.Lock()
					msg, hasMsg := s.pendingCamMsg[camID]
					delete(s.pendingCamMsg, camID)
					delete(s.pendingCamTimers, camID)
					s.lastCamTimes[camID] = time.Now()
					s.mu.Unlock()
					if hasMsg {
						s.enqueue(msg)
					}
				})
			}
			s.mu.Unlock()
			return true
		}
		s.lastCamTimes[camID] = now
		s.mu.Unlock()
	}
	return s.enqueue(m)
}

// Close releases the subscription. It is safe to call more than once.
func (s *Subscription) Close() { s.closeWith(ErrClosed) }

func (s *Subscription) closeWith(err error) {
	s.once.Do(func() {
		s.err = err
		close(s.done)
		s.mu.Lock()
		for _, tm := range s.pendingCamTimers {
			tm.Stop()
		}
		s.pendingCamTimers = nil
		s.mu.Unlock()
		s.hub.remove(s)
	})
}

// Recv returns the next message the actor is allowed to see. It returns the closing error
// (ErrClosed, ErrSlowConsumer) once the subscription ended, or ctx's error.
func (s *Subscription) Recv(ctx context.Context) (Envelope, error) {
	for {
		select {
		case <-s.done:
			return Envelope{}, s.err
		case <-ctx.Done():
			return Envelope{}, ctx.Err()
		case m := <-s.in:
			ok, err := s.hub.cfg.Authorizer.Allow(ctx, s.actor, m.Scope)
			if err != nil {
				if ctx.Err() == nil {
					s.hub.cfg.Log.Warn("realtime: authorization failed, message dropped", "error", err)
				}
				continue // fail closed
			}
			if ok {
				return m.Envelope, nil
			}
		}
	}
}
