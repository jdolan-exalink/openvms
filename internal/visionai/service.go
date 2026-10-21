package visionai

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/jdolan-exalink/openvms/internal/authz"
)

const (
	// EventWindow is how long after ending an event still competes for a slot.
	EventWindow = 10 * time.Minute
	// SessionIdleTTL is how long an unused allocator session is kept.
	SessionIdleTTL = 10 * time.Minute
)

// Result is the slot grid computed for one request.
type Result struct {
	Source      string
	Layout      string
	GeneratedAt time.Time
	// PlatesVisible mirrors EventSet.PlatesVisible.
	PlatesVisible bool
	Slots         []SlotAssignment
}

type session struct {
	allocator *SlotAllocator
	lastUsed  time.Time
}

// Service ranks real events into per-session slot grids. Allocation state (min display,
// cooldown, replacement) lives in memory per tenant|user|source|layout and is evicted when idle.
type Service struct {
	// Clock returns the current time; tests replace it.
	Clock func() time.Time

	source   EventSource
	mu       sync.Mutex
	sessions map[string]*session
}

// NewService builds a Service over an event source.
func NewService(source EventSource) *Service {
	return &Service{Clock: time.Now, source: source, sessions: make(map[string]*session)}
}

// SessionCount reports the live allocator sessions.
func (s *Service) SessionCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.sessions)
}

func sessionKey(a authz.Actor, source, layout string) string {
	tenant := "platform"
	if !a.IsPlatform() {
		tenant = a.TenantID.String()
	}
	return tenant + "|" + a.UserID.String() + "|" + source + "|" + layout
}

// session returns the actor's allocator for the config, creating it on first use, and evicts
// sessions idle longer than SessionIdleTTL.
func (s *Service) session(a authz.Actor, cfg VisionConfig, now time.Time) *SlotAllocator {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, sess := range s.sessions {
		if now.Sub(sess.lastUsed) > SessionIdleTTL {
			delete(s.sessions, k)
		}
	}
	// Each inactivity timeout keeps its own allocator so switching it never mixes slot state.
	key := sessionKey(a, cfg.ID, cfg.Layout) + "|" + strconv.Itoa(cfg.InactivitySeconds)
	sess, ok := s.sessions[key]
	if !ok {
		if !a.IsPlatform() {
			cfg.TenantID = *a.TenantID
		}
		sess = &session{allocator: NewSlotAllocator(cfg)}
		s.sessions[key] = sess
	}
	sess.lastUsed = now
	return sess.allocator
}

// SlotsOption adjusts one Slots request.
type SlotsOption func(*VisionConfig)

// WithInactivity sets the inactivity timeout in seconds (0 disables it). Values outside
// InactivityOptions are rejected by Slots.
func WithInactivity(seconds int) SlotsOption {
	return func(c *VisionConfig) { c.InactivitySeconds = seconds }
}

// Slots loads the actor's recent events, ranks them and returns the current slot grid.
func (s *Service) Slots(ctx context.Context, actor authz.Actor, source, layout string, opts ...SlotsOption) (Result, error) {
	cfg, err := PresetConfig(source, layout)
	if err != nil {
		return Result{}, err
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	if !slices.Contains(InactivityOptions, cfg.InactivitySeconds) {
		return Result{}, &ValidationError{Msg: fmt.Sprintf("inactivity must be one of %v seconds", InactivityOptions)}
	}
	now := s.Clock()
	set, err := s.source.RecentEvents(ctx, actor, EventWindow, now)
	if err != nil {
		return Result{}, err
	}
	var events []VisionEvent
	for _, r := range set.Rows {
		events = append(events, RowToEvents(r, cfg)...)
	}
	// Rows arrive newest first; keep the order stable for equal scores.
	sort.SliceStable(events, func(i, j int) bool { return events[i].StartTime.After(events[j].StartTime) })

	slots := s.session(actor, cfg, now).Allocate(events, now)
	return Result{Source: source, Layout: layout, GeneratedAt: now, PlatesVisible: set.PlatesVisible, Slots: slots}, nil
}
