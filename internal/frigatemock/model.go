// Package frigatemock simulates the parts of a Frigate server that OpenVMS talks to:
// the authenticated HTTP API (port 8971 semantics), review items and MQTT review messages.
// It is used for local development and CI (PRD §114). Shapes follow Frigate 0.17.
package frigatemock

import (
	"math/rand/v2"
	"sort"
	"sync"
	"time"
)

// ReviewData mirrors the "data" object of a Frigate review item.
type ReviewData struct {
	Detections []string `json:"detections"`
	Objects    []string `json:"objects"`
	SubLabels  []string `json:"sub_labels"`
	Zones      []string `json:"zones"`
	Audio      []string `json:"audio"`
}

// Review mirrors a Frigate review item as returned by GET /api/review.
type Review struct {
	ID              string     `json:"id"`
	Camera          string     `json:"camera"`
	StartTime       float64    `json:"start_time"`
	EndTime         *float64   `json:"end_time"`
	HasBeenReviewed bool       `json:"has_been_reviewed"`
	Severity        string     `json:"severity"`
	ThumbPath       string     `json:"thumb_path"`
	Data            ReviewData `json:"data"`
}

// ReviewMessage is the payload published on {topic_prefix}/reviews.
type ReviewMessage struct {
	Type   string  `json:"type"` // new | update | end
	Before *Review `json:"before"`
	After  *Review `json:"after"`
}

// Store keeps review items in memory, bounded to max entries.
type Store struct {
	mu      sync.RWMutex
	reviews map[string]*Review
	max     int
}

func NewStore(capacity int) *Store {
	return &Store{reviews: make(map[string]*Review), max: capacity}
}

func (s *Store) Put(r Review) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := r
	s.reviews[r.ID] = &cp
	if len(s.reviews) > s.max {
		s.evictOldestLocked()
	}
}

func (s *Store) Get(id string) (Review, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.reviews[id]
	if !ok {
		return Review{}, false
	}
	return *r, true
}

// Query filters the way Frigate's /api/review does for the parameters OpenVMS uses.
type Query struct {
	Cameras  map[string]bool // empty means all
	Severity string
	After    float64
	Before   float64
	Limit    int
}

// List returns matches newest first.
func (s *Store) List(q Query) []Review {
	s.mu.RLock()
	out := make([]Review, 0, len(s.reviews))
	for _, r := range s.reviews {
		if len(q.Cameras) > 0 && !q.Cameras[r.Camera] {
			continue
		}
		if q.Severity != "" && r.Severity != q.Severity {
			continue
		}
		if q.After > 0 && r.StartTime < q.After {
			continue
		}
		if q.Before > 0 && r.StartTime > q.Before {
			continue
		}
		out = append(out, *r)
	}
	s.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool { return out[i].StartTime > out[j].StartTime })
	if q.Limit > 0 && len(out) > q.Limit {
		out = out[:q.Limit]
	}
	return out
}

func (s *Store) evictOldestLocked() {
	var oldest *Review
	for _, r := range s.reviews {
		if oldest == nil || r.StartTime < oldest.StartTime {
			oldest = r
		}
	}
	if oldest != nil {
		delete(s.reviews, oldest.ID)
	}
}

func unixSeconds(t time.Time) float64 { return float64(t.UnixMicro()) / 1e6 }

const idAlphabet = "abcdefghijklmnopqrstuvwxyz0123456789"

// newReviewID follows Frigate's "<start_time>-<6 random chars>" format.
func newReviewID(start time.Time, rng *rand.Rand) string {
	b := make([]byte, 6)
	for i := range b {
		b[i] = idAlphabet[rng.IntN(len(idAlphabet))]
	}
	return formatTS(unixSeconds(start)) + "-" + string(b)
}
