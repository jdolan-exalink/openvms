package visionai

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jdolan-exalink/openvms/internal/store"
)

var (
	ErrConfigNotFound = errors.New("vision config not found")
	ErrInvalidSlot    = errors.New("invalid slot index")
)

// Service provides high-level operations for Vision AI sessions, event ingestion,
// and real-time slot broadcast.
type Service struct {
	mu         sync.RWMutex
	store      *store.Store
	allocators map[string]*SlotAllocator // key: tenantID:configID
	configs    map[string]VisionConfig   // key: tenantID:configID
	subscribers map[string][]chan []SlotAssignment // key: tenantID:configID
	events     map[uuid.UUID][]VisionEvent // key: tenantID, sliding window
}

// NewService instantiates a new Vision AI Service.
func NewService(st *store.Store) *Service {
	return &Service{
		store:       st,
		allocators:  make(map[string]*SlotAllocator),
		configs:     make(map[string]VisionConfig),
		subscribers: make(map[string][]chan []SlotAssignment),
		events:      make(map[uuid.UUID][]VisionEvent),
	}
}

func sessionKey(tenantID uuid.UUID, configID string) string {
	return fmt.Sprintf("%s:%s", tenantID, configID)
}

// GetDefaultConfig returns or creates the default Vision AI configuration for a tenant.
func (s *Service) GetDefaultConfig(ctx context.Context, tenantID uuid.UUID) (VisionConfig, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := sessionKey(tenantID, "default")
	if cfg, ok := s.configs[key]; ok {
		return cfg, nil
	}

	def := DefaultVisionConfig(tenantID)
	s.configs[key] = def
	s.allocators[key] = NewSlotAllocator(def)
	return def, nil
}

// ListConfigs returns all saved Vision configurations for a tenant.
func (s *Service) ListConfigs(ctx context.Context, tenantID uuid.UUID) ([]VisionConfig, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := sessionKey(tenantID, "default")
	if _, ok := s.configs[key]; !ok {
		def := DefaultVisionConfig(tenantID)
		s.configs[key] = def
		s.allocators[key] = NewSlotAllocator(def)
	}

	var res []VisionConfig
	for k, cfg := range s.configs {
		if cfg.TenantID == tenantID {
			res = append(res, cfg)
		}
		_ = k
	}
	return res, nil
}

// SaveConfig saves or updates a configuration and updates its allocator.
func (s *Service) SaveConfig(ctx context.Context, cfg VisionConfig) (VisionConfig, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if cfg.ID == "" {
		cfg.ID = uuid.New().String()
	}

	key := sessionKey(cfg.TenantID, cfg.ID)
	s.configs[key] = cfg
	s.allocators[key] = NewSlotAllocator(cfg)
	return cfg, nil
}

// DeleteConfig removes a configuration and its active allocator.
func (s *Service) DeleteConfig(ctx context.Context, tenantID uuid.UUID, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := sessionKey(tenantID, id)
	if _, ok := s.configs[key]; !ok {
		return ErrConfigNotFound
	}

	delete(s.configs, key)
	delete(s.allocators, key)
	return nil
}

// GetSlots returns current slot assignments for a given configuration.
func (s *Service) GetSlots(ctx context.Context, tenantID uuid.UUID, configID string) ([]SlotAssignment, error) {
	s.mu.Lock()
	key := sessionKey(tenantID, configID)
	alloc, ok := s.allocators[key]
	if !ok {
		if configID == "default" {
			def := DefaultVisionConfig(tenantID)
			s.configs[key] = def
			alloc = NewSlotAllocator(def)
			s.allocators[key] = alloc
		} else {
			s.mu.Unlock()
			return nil, ErrConfigNotFound
		}
	}
	s.mu.Unlock()

	return alloc.Slots(), nil
}

// SetPin pins or unpins a slot in a configuration session.
func (s *Service) SetPin(ctx context.Context, tenantID uuid.UUID, configID string, slotIndex int, pinned bool) error {
	s.mu.Lock()
	key := sessionKey(tenantID, configID)
	alloc, ok := s.allocators[key]
	if !ok {
		s.mu.Unlock()
		return ErrConfigNotFound
	}
	s.mu.Unlock()

	alloc.SetPin(slotIndex, pinned)
	s.broadcast(tenantID, configID, alloc.Slots())
	return nil
}

// IngestEvent feeds a new VisionEvent, updates all allocators for the tenant, and broadcasts diffs.
func (s *Service) IngestEvent(ctx context.Context, ev VisionEvent) {
	s.mu.Lock()
	tenantID := ev.TenantID
	now := time.Now()

	// Maintain sliding window of events (keep recent 5 minutes)
	cutoff := now.Add(-5 * time.Minute)
	currentEvents := s.events[tenantID]
	var kept []VisionEvent
	for _, old := range currentEvents {
		if old.Timestamp.After(cutoff) {
			kept = append(kept, old)
		}
	}
	kept = append(kept, ev)
	s.events[tenantID] = kept

	// Find all allocators for this tenant
	var targets []struct {
		configID string
		alloc    *SlotAllocator
	}
	for key, alloc := range s.allocators {
		tIDStr, cID, ok := strings.Cut(key, ":")
		if !ok {
			continue
		}
		tID, err := uuid.Parse(tIDStr)
		if err == nil && tID == tenantID {
			targets = append(targets, struct {
				configID string
				alloc    *SlotAllocator
			}{configID: cID, alloc: alloc})
		}
	}
	s.mu.Unlock()

	for _, target := range targets {
		updatedSlots := target.alloc.Allocate(kept, now)
		s.broadcast(tenantID, target.configID, updatedSlots)
	}
}

// Subscribe returns a channel receiving slot assignment updates for a specific session.
func (s *Service) Subscribe(tenantID uuid.UUID, configID string) <-chan []SlotAssignment {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := sessionKey(tenantID, configID)
	ch := make(chan []SlotAssignment, 10)
	s.subscribers[key] = append(s.subscribers[key], ch)
	return ch
}

func (s *Service) broadcast(tenantID uuid.UUID, configID string, slots []SlotAssignment) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	key := sessionKey(tenantID, configID)
	subs := s.subscribers[key]
	for _, ch := range subs {
		select {
		case ch <- slots:
		default:
		}
	}
}
