package frigatemock

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"strconv"
	"time"
)

// Camera describes one simulated camera.
type Camera struct {
	Name           string
	Zones          []string
	LPR            bool
	Detect         *bool
	TrackedObjects []string
}

// Publisher sends a payload to an MQTT topic. Nil disables publishing.
type Publisher interface {
	Publish(topic string, payload []byte) error
}

// Generator creates review items over time and publishes their lifecycle like Frigate does:
// one "new", zero or more "update" as objects or plates are recognised, then "end".
type Generator struct {
	Cameras     []Camera
	Store       *Store
	Publisher   Publisher
	TopicPrefix string
	Interval    time.Duration
	Log         *slog.Logger
	rng         *rand.Rand
}

func (g *Generator) random() *rand.Rand {
	if g.rng == nil {
		g.rng = rand.New(rand.NewPCG(uint64(time.Now().UnixNano()), 42))
	}
	return g.rng
}

// Seed fills the store with finished reviews spread over the past window,
// so reconciliation has history to backfill. Nothing is published for them.
func (g *Generator) Seed(n int, window time.Duration, now time.Time) {
	rng := g.random()
	for range n {
		start := now.Add(-time.Duration(rng.Int64N(int64(window))))
		r := g.buildReview(start)
		end := unixSeconds(start.Add(time.Duration(5+rng.IntN(55)) * time.Second))
		r.EndTime = &end
		g.enrich(&r)
		g.Store.Put(r)
	}
}

// Run emits a new review lifecycle every Interval until ctx is cancelled.
func (g *Generator) Run(ctx context.Context) {
	t := time.NewTicker(g.Interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			go g.lifecycle(ctx, time.Now())
		}
	}
}

func (g *Generator) lifecycle(ctx context.Context, start time.Time) {
	r := g.buildReview(start)
	g.Store.Put(r)
	g.publish(ReviewMessage{Type: "new", After: &r})

	if !sleep(ctx, 2*time.Second) {
		return
	}
	before := r
	g.enrich(&r)
	g.Store.Put(r)
	g.publish(ReviewMessage{Type: "update", Before: &before, After: &r})

	if !sleep(ctx, 3*time.Second) {
		return
	}
	before = r
	end := unixSeconds(time.Now())
	r.EndTime = &end
	g.Store.Put(r)
	g.publish(ReviewMessage{Type: "end", Before: &before, After: &r})
}

func (g *Generator) buildReview(start time.Time) Review {
	rng := g.random()
	cam := g.Cameras[rng.IntN(len(g.Cameras))]
	id := newReviewID(start, rng)
	obj := []string{"person", "car", "car", "motorcycle", "dog"}[rng.IntN(5)]
	if cam.LPR {
		obj = "car"
	}
	severity := "detection"
	if rng.IntN(3) == 0 || cam.LPR {
		severity = "alert"
	}
	r := Review{
		ID:        id,
		Camera:    cam.Name,
		StartTime: unixSeconds(start),
		Severity:  severity,
		ThumbPath: fmt.Sprintf("/media/frigate/clips/review/thumb-%s-%s.webp", cam.Name, id),
		Data: ReviewData{
			Detections: []string{formatTS(unixSeconds(start)) + "-" + strconv.Itoa(rng.IntN(1e6))},
			Objects:    []string{obj},
			SubLabels:  []string{},
			Zones:      []string{},
			Audio:      []string{},
		},
	}
	if len(cam.Zones) > 0 {
		r.Data.Zones = []string{cam.Zones[rng.IntN(len(cam.Zones))]}
	}
	return r
}

// enrich simulates late information: a second object and, on LPR cameras, a recognised plate.
func (g *Generator) enrich(r *Review) {
	rng := g.random()
	if rng.IntN(2) == 0 {
		r.Data.Objects = appendUnique(r.Data.Objects, "person")
	}
	for _, c := range g.Cameras {
		if c.Name == r.Camera && c.LPR {
			r.Data.SubLabels = appendUnique(r.Data.SubLabels, randomPlate(rng))
		}
	}
}

func (g *Generator) publish(m ReviewMessage) {
	if g.Publisher == nil {
		return
	}
	b, err := json.Marshal(m)
	if err != nil {
		g.Log.Error("marshal review message", "error", err)
		return
	}
	if err := g.Publisher.Publish(g.TopicPrefix+"/reviews", b); err != nil {
		g.Log.Warn("publish review message", "error", err)
	}
}

// randomPlate returns an Argentine plate in either the Mercosur (AB123CD) or old (ABC123) format.
func randomPlate(rng *rand.Rand) string {
	l := func() byte { return byte('A' + rng.IntN(26)) }
	d := func() byte { return byte('0' + rng.IntN(10)) }
	if rng.IntN(3) == 0 {
		return string([]byte{l(), l(), l(), d(), d(), d()})
	}
	return string([]byte{l(), l(), d(), d(), d(), l(), l()})
}

func appendUnique(s []string, v string) []string {
	for _, x := range s {
		if x == v {
			return s
		}
	}
	return append(s, v)
}

func formatTS(f float64) string { return strconv.FormatFloat(f, 'f', 6, 64) }

func sleep(ctx context.Context, d time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(d):
		return true
	}
}
