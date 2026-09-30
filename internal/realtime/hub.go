package realtime

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

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
}

// Hub fans decoded messages out to subscriptions of this API instance.
type Hub struct {
	cfg HubConfig

	mu      sync.Mutex
	closed  bool
	subs    map[*Subscription]struct{}
	perUser map[uuid.UUID]int
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
	return &Hub{cfg: cfg, subs: map[*Subscription]struct{}{}, perUser: map[uuid.UUID]int{}}
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
	s := &Subscription{hub: h, actor: actor, in: make(chan Message, h.cfg.Buffer), done: make(chan struct{})}
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
		h.Publish(m)
	}
}

// Publish offers m to every subscription of its tenant. It never blocks: a subscription whose
// buffer is full is closed with ErrSlowConsumer.
func (h *Hub) Publish(m Message) {
	h.mu.Lock()
	var slow []*Subscription
	for s := range h.subs {
		// Tenant isolation is enforced here, independent of the Authorizer. Platform actors
		// (no tenant) fall through to the Authorizer, which scopes them by their grants.
		if s.actor.TenantID != nil && *s.actor.TenantID != m.Envelope.TenantID {
			continue
		}
		select {
		case s.in <- m:
		default:
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
}

// Close releases the subscription. It is safe to call more than once.
func (s *Subscription) Close() { s.closeWith(ErrClosed) }

func (s *Subscription) closeWith(err error) {
	s.once.Do(func() {
		s.err = err
		close(s.done)
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
