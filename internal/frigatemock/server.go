package frigatemock

import (
	"bytes"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"hash/fnv"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// Server serves the subset of the Frigate HTTP API used by OpenVMS.
type Server struct {
	Name        string
	Version     string
	TopicPrefix string
	Cameras     []Camera
	Store       *Store
	User        string
	Password    string
	RequireAuth bool
	StartedAt   time.Time
	// Offline makes every request fail with 503, to simulate an unreachable Frigate.
	Offline atomic.Bool

	token string
}

const tokenCookie = "frigate_token"

func (s *Server) Handler() http.Handler {
	if s.token == "" {
		b := make([]byte, 24)
		_, _ = rand.Read(b)
		s.token = hex.EncodeToString(b)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/login", s.login)
	mux.HandleFunc("GET /api/version", s.auth(s.version))
	mux.HandleFunc("GET /api/config", s.auth(s.config))
	mux.HandleFunc("GET /api/stats", s.auth(s.stats))
	mux.HandleFunc("GET /api/review", s.auth(s.listReviews))
	mux.HandleFunc("GET /api/review/{id}", s.auth(s.getReview))
	mux.HandleFunc("GET /api/events", s.auth(s.objects))
	mux.HandleFunc("POST /api/export/{camera}/start/{start}/end/{end}", s.auth(s.startExport))
	mux.HandleFunc("GET /api/exports/{id}", s.auth(s.getExport))
	mux.HandleFunc("GET /exports/{file}", s.auth(s.exportFile))
	mux.HandleFunc("GET /api/{camera}/{file}", s.auth(s.latest))
	mux.HandleFunc("GET /clips/review/{file}", s.auth(s.thumb))
	return mux
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var body struct {
		User     string `json:"user"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	userOK := subtle.ConstantTimeCompare([]byte(body.User), []byte(s.User)) == 1
	passOK := subtle.ConstantTimeCompare([]byte(body.Password), []byte(s.Password)) == 1
	if !userOK || !passOK {
		http.Error(w, `{"message":"Login failed"}`, http.StatusUnauthorized)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: tokenCookie, Value: s.token, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode})
	w.WriteHeader(http.StatusOK)
}

// auth mimics port 8971: either the session cookie or a Bearer token is required.
func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.Offline.Load() {
			http.Error(w, "offline", http.StatusServiceUnavailable)
			return
		}
		if s.RequireAuth && !s.authorized(r) {
			http.Error(w, `{"message":"Unauthorized"}`, http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func (s *Server) authorized(r *http.Request) bool {
	presented := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if c, err := r.Cookie(tokenCookie); err == nil {
		presented = c.Value
	}
	return presented != "" && subtle.ConstantTimeCompare([]byte(presented), []byte(s.token)) == 1
}

func (s *Server) version(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	_, _ = w.Write([]byte(s.Version))
}

func (s *Server) config(w http.ResponseWriter, _ *http.Request) {
	cams := map[string]any{}
	for _, c := range s.Cameras {
		zones := map[string]any{}
		for _, z := range c.Zones {
			zones[z] = map[string]any{"coordinates": "0,0,1,0,1,1,0,1"}
		}
		cams[c.Name] = map[string]any{
			"name":    c.Name,
			"enabled": true,
			"detect":  map[string]any{"enabled": true, "width": 1280, "height": 720, "fps": 5},
			"record":  map[string]any{"enabled": true},
			"live":    map[string]any{"streams": map[string]string{"main": c.Name, "sub": c.Name + "_sub"}},
			"lpr":     map[string]any{"enabled": c.LPR},
			"zones":   zones,
		}
	}
	writeJSON(w, map[string]any{
		"cameras": cams,
		"mqtt":    map[string]any{"enabled": true, "topic_prefix": s.TopicPrefix},
		"version": s.Version,
	})
}

func (s *Server) stats(w http.ResponseWriter, _ *http.Request) {
	cams := map[string]any{}
	for _, c := range s.Cameras {
		cams[c.Name] = map[string]any{"camera_fps": 5.0, "detection_fps": 1.2, "process_fps": 5.0, "skipped_fps": 0.0, "detection_enabled": true}
	}
	const gb = 1024.0
	writeJSON(w, map[string]any{
		"cameras":   cams,
		"detectors": map[string]any{"ov": map[string]any{"inference_speed": 9.4, "detection_start": 0.0}},
		"service": map[string]any{
			"uptime":  int(time.Since(s.StartedAt).Seconds()),
			"version": s.Version,
			"storage": map[string]any{
				"/media/frigate/recordings": map[string]any{"total": 4000 * gb, "used": 2750 * gb, "free": 1250 * gb, "mount_type": "ext4"},
			},
		},
	})
}

func (s *Server) listReviews(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	query := Query{Severity: q.Get("severity"), Limit: 100}
	if v := q.Get("cameras"); v != "" && v != "all" {
		query.Cameras = map[string]bool{}
		for _, c := range strings.Split(v, ",") {
			query.Cameras[c] = true
		}
	}
	query.After, _ = strconv.ParseFloat(q.Get("after"), 64)
	query.Before, _ = strconv.ParseFloat(q.Get("before"), 64)
	if n, err := strconv.Atoi(q.Get("limit")); err == nil && n > 0 {
		query.Limit = n
	}
	writeJSON(w, s.Store.List(query))
}

func (s *Server) getReview(w http.ResponseWriter, r *http.Request) {
	rev, ok := s.Store.Get(r.PathValue("id"))
	if !ok {
		http.Error(w, `{"message":"Review not found"}`, http.StatusNotFound)
		return
	}
	writeJSON(w, rev)
}

func (s *Server) latest(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("camera")
	if r.PathValue("file") == "recordings" {
		s.recordings(w, r) // same shape as /api/{camera}/{file}; one pattern avoids a mux conflict
		return
	}
	if r.PathValue("file") != "latest.jpg" {
		http.NotFound(w, r)
		return
	}
	for _, c := range s.Cameras {
		if c.Name == name {
			writeJPEG(w, name+time.Now().Format("15:04:05"))
			return
		}
	}
	http.Error(w, `{"message":"Camera not found"}`, http.StatusNotFound)
}

func (s *Server) thumb(w http.ResponseWriter, r *http.Request) {
	writeJPEG(w, r.PathValue("file"))
}

// writeJPEG draws a flat image whose colour is derived from seed, so each thumbnail is stable.
func writeJPEG(w http.ResponseWriter, seed string) {
	h := fnv.New32a()
	_, _ = h.Write([]byte(seed))
	v := h.Sum32()
	img := image.NewRGBA(image.Rect(0, 0, 320, 180))
	bg := color.RGBA{uint8(v), uint8(v >> 8), uint8(v >> 16), 255}
	for y := range 180 {
		for x := range 320 {
			c := bg
			if (x/40+y/40)%2 == 0 {
				c = color.RGBA{bg.R / 2, bg.G / 2, bg.B / 2, 255}
			}
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	_ = jpeg.Encode(&buf, img, &jpeg.Options{Quality: 70})
	w.Header().Set("Content-Type", "image/jpeg")
	_, _ = w.Write(buf.Bytes())
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
