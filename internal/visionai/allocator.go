package visionai

import (
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// SlotAllocator manages dynamic assignment of events to visual slots.
type SlotAllocator struct {
	mu      sync.RWMutex
	config  VisionConfig
	scoring *ScoringEngine
	slots   []SlotAssignment
	history map[uuid.UUID]CameraHistory
}

// NewSlotAllocator creates and initializes an allocator for the given configuration.
func NewSlotAllocator(config VisionConfig) *SlotAllocator {
	slotCount := parseSlotCount(config.Layout)
	slots := make([]SlotAssignment, slotCount)
	for i := range slots {
		slots[i] = SlotAssignment{
			SlotIndex: i,
		}
	}

	return &SlotAllocator{
		config:  config,
		scoring: NewScoringEngine(),
		slots:   slots,
		history: make(map[uuid.UUID]CameraHistory),
	}
}

func parseSlotCount(layout string) int {
	switch strings.ToLower(strings.TrimSpace(layout)) {
	case "focus":
		return 5
	case "1":
		return 1
	case "4":
		return 4
	case "9":
		return 9
	case "16":
		return 16
	case "25":
		return 25
	default:
		n, err := strconv.Atoi(layout)
		if err == nil && n > 0 && n <= 36 {
			return n
		}
		return 4
	}
}

// SetPin pins or unpins a slot index.
func (a *SlotAllocator) SetPin(slotIndex int, pinned bool) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if slotIndex >= 0 && slotIndex < len(a.slots) {
		a.slots[slotIndex].Pinned = pinned
	}
}

// Slots returns a snapshot of current slot assignments.
func (a *SlotAllocator) Slots() []SlotAssignment {
	a.mu.RLock()
	defer a.mu.RUnlock()

	res := make([]SlotAssignment, len(a.slots))
	copy(res, a.slots)
	return res
}

// matchesConfig checks if an event satisfies the configured object, site, camera, and zone filters.
func (a *SlotAllocator) matchesConfig(ev VisionEvent) bool {
	// Objects filter
	if len(a.config.Objects) > 0 {
		matched := false
		for _, o := range a.config.Objects {
			if strings.EqualFold(o, ev.Object) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}

	// Sites filter
	if len(a.config.Sites) > 0 {
		matched := false
		for _, s := range a.config.Sites {
			if s == ev.SiteID {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}

	if a.config.RequirePlate && ev.Plate == "" {
		return false
	}
	if a.config.RequireZone && ev.Zone == "" {
		return false
	}
	if a.config.RequireAlarm && ev.AlarmSeverity == "" {
		return false
	}

	// Cameras filter
	if len(a.config.Cameras) > 0 {
		matched := false
		for _, c := range a.config.Cameras {
			if c == ev.CameraID {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}

	return true
}

type scoredCandidate struct {
	event     VisionEvent
	score     float64
	breakdown map[string]float64
}

func (c scoredCandidate) reason() SlotReason {
	return SlotReason{
		Object:     c.event.Object,
		Confidence: c.event.Confidence,
		Zone:       c.event.Zone,
		Plate:      c.event.Plate,
		Alarm:      c.event.AlarmSeverity,
		Attributes: c.event.Attributes,
		Breakdown:  c.breakdown,
	}
}

// replaceMargin is how much a newcomer must outscore an on-screen camera to evict it before its
// max display time; it keeps equally ranked activity from churning the grid on every poll.
const replaceMargin = 10.0

// incumbentStanding is the slot's score without the already-visible penalty, which only exists to
// rank duplicates and must not make an on-screen camera lose to an equally ranked newcomer.
func incumbentStanding(s SlotAssignment) float64 {
	return s.Score - s.Reason.Breakdown["already_visible_penalty"]
}

// release empties a slot and records the camera's exit for cooldown scoring. Caller holds a.mu.
func (a *SlotAllocator) release(i int, now time.Time) {
	cam := a.slots[i].CameraID
	hist := a.history[cam]
	hist.CurrentlyVisible = false
	hist.LastExitedAt = now
	a.history[cam] = hist
	a.slots[i] = SlotAssignment{SlotIndex: i}
}

// Allocate processes candidates, ranks them, and assigns them to eligible slots.
func (a *SlotAllocator) Allocate(events []VisionEvent, now time.Time) []SlotAssignment {
	a.mu.Lock()
	defer a.mu.Unlock()

	// 1. Filter and score candidates
	var candidates []scoredCandidate
	inactivity := time.Duration(a.config.InactivitySeconds) * time.Second
	for _, ev := range events {
		if !a.matchesConfig(ev) {
			continue
		}
		// An event whose last detection is older than the inactivity timeout no longer competes.
		if inactivity > 0 && now.Sub(lastDetection(ev, now)) > inactivity {
			continue
		}

		hist := a.history[ev.CameraID]
		score, breakdown := a.scoring.CalculateScore(ev, hist, a.config, now)
		candidates = append(candidates, scoredCandidate{
			event:     ev,
			score:     score,
			breakdown: breakdown,
		})
	}

	// Sort candidates descending by score
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})

	// A camera occupies at most one slot: keep only its best-scoring candidate.
	seenCams := make(map[uuid.UUID]bool, len(candidates))
	best := candidates[:0]
	for _, c := range candidates {
		if seenCams[c.event.CameraID] {
			continue
		}
		seenCams[c.event.CameraID] = true
		best = append(best, c)
	}
	candidates = best

	// Release unpinned slots that have no candidate anymore once they outlived their max display, so
	// the grid empties when activity stops instead of showing stale cameras forever.
	for i, s := range a.slots {
		if s.CameraID == uuid.Nil || s.Pinned || seenCams[s.CameraID] {
			continue
		}
		// With an inactivity timeout a camera without an active candidate leaves once its min
		// display time is over; without one it is kept until its max display time.
		keep := now.Before(s.MinDisplayUntil)
		if inactivity <= 0 {
			keep = !now.After(s.MaxDisplayUntil)
		}
		if keep {
			continue
		}
		a.release(i, now)
	}

	minDisplay := time.Duration(a.config.MinDisplaySeconds) * time.Second
	if minDisplay <= 0 {
		minDisplay = 5 * time.Second
	}
	maxDisplay := time.Duration(a.config.MaxDisplaySeconds) * time.Second
	if maxDisplay <= 0 {
		maxDisplay = 30 * time.Second
	}

	// Check if any active slot already holds a candidate camera to update it
	allocatedCams := make(map[uuid.UUID]int)
	for i, s := range a.slots {
		if s.CameraID != uuid.Nil {
			allocatedCams[s.CameraID] = i
		}
	}

	for _, cand := range candidates {
		camID := cand.event.CameraID

		// If this camera is already displayed in a slot, update its score and reason without moving it
		if slotIdx, found := allocatedCams[camID]; found {
			a.slots[slotIdx].Score = cand.score
			a.slots[slotIdx].EventID = cand.event.ID
			a.slots[slotIdx].Reason = cand.reason()
			if start := detectionStart(cand.event); start.After(a.slots[slotIdx].DetectedAt) {
				a.slots[slotIdx].DetectedAt = start
			}
			continue
		}

		// Find an empty slot or the best candidate slot to replace
		bestSlotIdx := -1
		lowestScore := 1e9

		for i, s := range a.slots {
			if s.Pinned {
				continue
			}

			if s.CameraID == uuid.Nil {
				bestSlotIdx = i
				break
			}

			// If the slot has not reached min display time, cannot replace it yet
			if now.Before(s.MinDisplayUntil) {
				continue
			}

			// Before max display a newcomer must clearly beat the incumbent's standing score.
			standing := incumbentStanding(s)
			if now.After(s.MaxDisplayUntil) || cand.score > standing+replaceMargin {
				if standing < lowestScore {
					lowestScore = standing
					bestSlotIdx = i
				}
			}
		}

		if bestSlotIdx >= 0 {
			// Evict previous camera if any
			prevCam := a.slots[bestSlotIdx].CameraID
			if prevCam != uuid.Nil {
				hist := a.history[prevCam]
				hist.CurrentlyVisible = false
				hist.LastExitedAt = now
				a.history[prevCam] = hist
				delete(allocatedCams, prevCam)
			}

			// Assign candidate to slot
			a.slots[bestSlotIdx] = SlotAssignment{
				SlotIndex:       bestSlotIdx,
				CameraID:        camID,
				CameraName:      cand.event.CameraName,
				EventID:         cand.event.ID,
				Score:           cand.score,
				Reason:          cand.reason(),
				Pinned:          false,
				AssignedAt:      now,
				DetectedAt:      detectionStart(cand.event),
				MinDisplayUntil: now.Add(minDisplay),
				MaxDisplayUntil: now.Add(maxDisplay),
			}

			// Update new camera history
			hist := a.history[camID]
			hist.CurrentlyVisible = true
			hist.VisibleSince = now
			a.history[camID] = hist

			allocatedCams[camID] = bestSlotIdx
		}
	}

	res := make([]SlotAssignment, len(a.slots))
	copy(res, a.slots)
	return res
}

// lastDetection is when the event was last active: now while it is still running, else its end.
func lastDetection(ev VisionEvent, now time.Time) time.Time {
	if ev.EndTime == nil {
		return now
	}
	return *ev.EndTime
}

// detectionStart is when the event's detection began.
func detectionStart(ev VisionEvent) time.Time {
	if !ev.StartTime.IsZero() {
		return ev.StartTime
	}
	return ev.Timestamp
}
