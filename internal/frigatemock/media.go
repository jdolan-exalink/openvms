package frigatemock

import (
	"bytes"
	"fmt"
	"hash/fnv"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Tracked objects, recordings and exports: enough of Frigate for the VMS event index,
// plate search, timeline and exports to be exercised without a real Frigate.

type objectData struct {
	TopScore   float64  `json:"top_score"`
	Plate      *string  `json:"recognized_license_plate,omitempty"`
	PlateScore *float64 `json:"recognized_license_plate_score,omitempty"`
}

type trackedObject struct {
	ID          string     `json:"id"`
	Camera      string     `json:"camera"`
	Label       string     `json:"label"`
	SubLabel    *string    `json:"sub_label"`
	Zones       []string   `json:"zones"`
	StartTime   float64    `json:"start_time"`
	EndTime     *float64   `json:"end_time"`
	HasSnapshot bool       `json:"has_snapshot"`
	Data        objectData `json:"data"`
}

// objects derives one tracked object per review detection. On LPR cameras the plate
// the generator put in sub_labels becomes the recognized plate, as in Frigate 0.16+.
// has_snapshot is deterministically true for alert reviews and false for detections: real
// Frigate ties it to per-object/config state we do not simulate, but this split gives every
// camera (LPR or not) a mix of both for tests, instead of a flat true/false.
func (s *Server) objects(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	after, _ := strconv.ParseFloat(q.Get("after"), 64)
	before, _ := strconv.ParseFloat(q.Get("before"), 64)
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 {
		limit = 100
	}
	cams := map[string]bool{}
	if v := q.Get("cameras"); v != "" && v != "all" {
		for _, c := range strings.Split(v, ",") {
			cams[c] = true
		}
	}
	lpr := map[string]bool{}
	for _, c := range s.Cameras {
		lpr[c.Name] = c.LPR
	}
	var out []trackedObject
	for _, rev := range s.Store.List(Query{Limit: 0}) {
		if len(cams) > 0 && !cams[rev.Camera] {
			continue
		}
		for i, det := range rev.Data.Detections {
			o := trackedObject{
				ID: det, Camera: rev.Camera, Label: rev.Data.Objects[0], Zones: rev.Data.Zones,
				StartTime: rev.StartTime + float64(i), EndTime: rev.EndTime, HasSnapshot: rev.Severity == "alert",
				Data: objectData{TopScore: 0.87},
			}
			if lpr[rev.Camera] && len(rev.Data.SubLabels) > 0 {
				plate := rev.Data.SubLabels[len(rev.Data.SubLabels)-1]
				score := 0.93
				o.Data.Plate, o.Data.PlateScore = &plate, &score
			}
			if (after > 0 && o.StartTime <= after) || (before > 0 && o.StartTime >= before) {
				continue
			}
			out = append(out, o)
		}
	}
	if q.Get("sort") == "date_asc" {
		sort.Slice(out, func(i, j int) bool { return out[i].StartTime < out[j].StartTime })
	} else {
		sort.Slice(out, func(i, j int) bool { return out[i].StartTime > out[j].StartTime })
	}
	if len(out) > limit {
		out = out[:limit]
	}
	if out == nil {
		out = []trackedObject{}
	}
	writeJSON(w, out)
}

// eventSnapshot serves a fake JPEG for a tracked object's (Frigate "event" in the events API
// sense) full-frame snapshot, the same endpoint eventSnapshot/lprReadSnapshot in
// internal/media/gateway.go relay through. Real Frigate 404s for an unknown id; this mock never
// tracks tracked-object existence separately from reviews, so it always serves an image keyed by
// id, deterministic like the other mock thumbnails.
func (s *Server) eventSnapshot(w http.ResponseWriter, r *http.Request) {
	writeJPEG(w, "event-"+r.PathValue("id"))
}

// eventClip serves a fake, deterministic MP4-shaped payload for a tracked object's clip
// (PDW-2's /media/v1/lpr/reads/{id}/clip.mp4 relays this), the same "keyed by id, always
// exists" convention eventSnapshot uses. http.ServeContent gives it real HTTP Range support
// (206 Partial Content, Content-Range, Accept-Ranges), the same behaviour real Frigate's
// static clip serving has, so PDW-2's seekable <video> can be exercised without a real
// Frigate.
func (s *Server) eventClip(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	data := clipBytes(id)
	w.Header().Set("Content-Type", "video/mp4")
	http.ServeContent(w, r, id+".mp4", time.Time{}, bytes.NewReader(data))
}

// clipBytes derives a stable, seed-dependent byte slice standing in for an MP4 file: large
// enough (16 KiB) that Range requests for different byte windows observably differ.
func clipBytes(seed string) []byte {
	h := fnv.New32a()
	_, _ = h.Write([]byte(seed))
	v := h.Sum32()
	b := make([]byte, 16<<10)
	for i := range b {
		b[i] = byte(v>>uint(i%24)) ^ byte(i)
	}
	return b
}

type recordingSegment struct {
	ID        string  `json:"id"`
	StartTime float64 `json:"start_time"`
	EndTime   float64 `json:"end_time"`
	Motion    int     `json:"motion"`
	Objects   int     `json:"objects"`
}

// recordings reports continuous 10-second segments with a gap every hour, like a
// camera that dropped briefly.
func (s *Server) recordings(w http.ResponseWriter, r *http.Request) {
	if !s.hasCamera(r.PathValue("camera")) {
		http.Error(w, `{"message":"Camera not found"}`, http.StatusNotFound)
		return
	}
	q := r.URL.Query()
	after, _ := strconv.ParseFloat(q.Get("after"), 64)
	before, _ := strconv.ParseFloat(q.Get("before"), 64)
	now := unixSeconds(time.Now())
	if before == 0 || before > now {
		before = now
	}
	if after == 0 || before-after > 24*3600 {
		after = before - 24*3600
	}
	var out []recordingSegment
	for t := after - float64(int64(after)%10); t < before; t += 10 {
		if int64(t)%3600 < 60 {
			continue // one minute without recording every hour
		}
		out = append(out, recordingSegment{ID: fmt.Sprintf("%.0f-rec", t), StartTime: t, EndTime: t + 10, Motion: int(t) % 7, Objects: int(t) % 3})
	}
	if out == nil {
		out = []recordingSegment{}
	}
	writeJSON(w, out)
}

func (s *Server) hasCamera(name string) bool {
	for _, c := range s.Cameras {
		if c.Name == name {
			return true
		}
	}
	return false
}

type mockExport struct {
	ID         string  `json:"id"`
	Camera     string  `json:"camera"`
	Name       string  `json:"name"`
	Date       float64 `json:"date"`
	VideoPath  string  `json:"video_path"`
	ThumbPath  string  `json:"thumb_path"`
	InProgress bool    `json:"in_progress"`
	readyAt    time.Time
}

type exportStore struct {
	mu    sync.Mutex
	items map[string]*mockExport
}

var exports = &exportStore{items: map[string]*mockExport{}}

func (s *Server) startExport(w http.ResponseWriter, r *http.Request) {
	cam := r.PathValue("camera")
	if !s.hasCamera(cam) {
		http.Error(w, `{"success":false,"message":"Camera not found"}`, http.StatusNotFound)
		return
	}
	id := fmt.Sprintf("%s_%d", cam, time.Now().UnixNano())
	e := &mockExport{
		ID: id, Camera: cam, Name: "Export " + cam, Date: unixSeconds(time.Now()),
		VideoPath: "/media/frigate/exports/" + id + ".mp4", ThumbPath: "/media/frigate/exports/" + id + ".webp",
		InProgress: true, readyAt: time.Now().Add(3 * time.Second),
	}
	exports.mu.Lock()
	exports.items[id] = e
	exports.mu.Unlock()
	writeJSON(w, map[string]any{"success": true, "message": "Starting export of recording.", "export_id": id, "status": "queued"})
}

func (s *Server) getExport(w http.ResponseWriter, r *http.Request) {
	exports.mu.Lock()
	e, ok := exports.items[r.PathValue("id")]
	var cp mockExport
	if ok {
		if e.InProgress && time.Now().After(e.readyAt) {
			e.InProgress = false
		}
		cp = *e
	}
	exports.mu.Unlock()
	if !ok {
		http.Error(w, `{"message":"Export not found"}`, http.StatusNotFound)
		return
	}
	writeJSON(w, cp)
}

// exportFile serves a tiny placeholder instead of a real MP4.
func (s *Server) exportFile(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSuffix(r.PathValue("file"), ".mp4")
	exports.mu.Lock()
	e, ok := exports.items[name]
	ready := ok && !e.InProgress
	exports.mu.Unlock()
	if !ready {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "video/mp4")
	_, _ = w.Write([]byte("mock export " + name))
}
