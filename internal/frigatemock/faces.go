package frigatemock

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"sync"
)

// faceLibrary is the in-memory face library behind /api/faces: person name -> image id -> bytes,
// plus the unclassified train attempts Frigate keeps under "train".
type faceLibrary struct {
	mu     sync.Mutex
	faces  map[string]map[string][]byte
	train  map[string][]byte
	nextID int
	// calls records mutating requests as "op:name" (create, register, delete, rename).
	calls []string
	// drop counts, per operation, how many upcoming responses are replaced by a 502 AFTER the
	// write was applied: the "write succeeded but the response was lost" case.
	drop map[string]int
}

// FaceCalls returns the mutating face requests received so far, as "op:name".
func (s *Server) FaceCalls() []string {
	l := s.lib()
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.calls...)
}

// SeedFaceImage puts an image straight into the library (a face that exists in Frigate
// independently of any OpenVMS sync). An empty id list entry is not needed: pass no data to
// create an empty folder.
func (s *Server) SeedFaceImage(name, id string, data []byte) {
	l := s.lib()
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.faces[name] == nil {
		l.faces[name] = map[string][]byte{}
	}
	if id != "" {
		l.faces[name][id] = data
	}
}

// DeleteFaceForTest removes a whole face folder, as an operator does in Frigate's own UI.
func (s *Server) DeleteFaceForTest(name string) {
	l := s.lib()
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.faces, name)
}

// DropFaceResponses makes the next n responses of op ("create", "register", "delete") fail with
// 502 after the write has been applied.
func (s *Server) DropFaceResponses(op string, n int) {
	l := s.lib()
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.drop == nil {
		l.drop = map[string]int{}
	}
	l.drop[op] = n
}

// record notes a mutating call and reports whether its response must be dropped. Callers hold l.mu.
func (l *faceLibrary) record(op, name string) bool {
	l.calls = append(l.calls, op+":"+name)
	if l.drop[op] > 0 {
		l.drop[op]--
		return true
	}
	return false
}

func (s *Server) lib() *faceLibrary {
	s.faceOnce.Do(func() { s.faceLib = &faceLibrary{faces: map[string]map[string][]byte{}, train: map[string][]byte{}} })
	return s.faceLib
}

// FaceLibrary returns the current library as GET /api/faces reports it (person -> image ids).
func (s *Server) FaceLibrary() map[string][]string {
	l := s.lib()
	l.mu.Lock()
	defer l.mu.Unlock()
	out := map[string][]string{}
	for name, imgs := range l.faces {
		out[name] = sortedKeys(imgs)
	}
	return out
}

// FaceImageBytes returns the bytes stored for one registered image, or nil.
func (s *Server) FaceImageBytes(name, id string) []byte {
	l := s.lib()
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.faces[name][id]
}

// AddFaceTrainAttempt injects an unclassified recognition attempt, as Frigate saves when
// save_attempts is on.
func (s *Server) AddFaceTrainAttempt(file string) {
	l := s.lib()
	l.mu.Lock()
	defer l.mu.Unlock()
	l.train[file] = []byte("train:" + file)
}

// AddFaceTrainImage injects an unclassified attempt with real image bytes.
func (s *Server) AddFaceTrainImage(file string, data []byte) {
	l := s.lib()
	l.mu.Lock()
	defer l.mu.Unlock()
	l.train[file] = append([]byte(nil), data...)
}

// faceFile serves GET /clips/faces/{name}/{file}, the static route Frigate's UI uses to show
// library and train images.
func (s *Server) faceFile(w http.ResponseWriter, r *http.Request) {
	l := s.lib()
	l.mu.Lock()
	defer l.mu.Unlock()
	var data []byte
	var ok bool
	if name := r.PathValue("name"); name == "train" {
		data, ok = l.train[r.PathValue("file")]
	} else {
		data, ok = l.faces[name][r.PathValue("file")]
	}
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", http.DetectContentType(data))
	_, _ = w.Write(data)
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// requireFaces answers Frigate's 400 when face recognition is disabled.
func (s *Server) requireFaces(w http.ResponseWriter) bool {
	if s.FaceRecognition {
		return true
	}
	writeStatusJSON(w, http.StatusBadRequest, map[string]any{"message": "Face recognition is not enabled.", "success": false})
	return false
}

func writeStatusJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) listFaces(w http.ResponseWriter, _ *http.Request) {
	l := s.lib()
	l.mu.Lock()
	defer l.mu.Unlock()
	out := map[string][]string{}
	for name, imgs := range l.faces {
		out[name] = sortedKeys(imgs)
	}
	if len(l.train) > 0 {
		out["train"] = sortedKeys(l.train)
	}
	writeJSON(w, out)
}

func (s *Server) createFace(w http.ResponseWriter, r *http.Request) {
	if !s.requireFaces(w) {
		return
	}
	l := s.lib()
	l.mu.Lock()
	defer l.mu.Unlock()
	name := r.PathValue("name")
	if _, ok := l.faces[name]; !ok {
		l.faces[name] = map[string][]byte{}
	}
	if l.record("create", name) {
		writeStatusJSON(w, http.StatusBadGateway, map[string]any{"success": false})
		return
	}
	writeJSON(w, map[string]any{"success": true, "message": "Successfully created face folder."})
}

func (s *Server) registerFace(w http.ResponseWriter, r *http.Request) {
	if !s.requireFaces(w) {
		return
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		writeStatusJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "file is required"})
		return
	}
	defer file.Close()
	data, _ := io.ReadAll(file)
	l := s.lib()
	l.mu.Lock()
	defer l.mu.Unlock()
	name := r.PathValue("name")
	if l.faces[name] == nil {
		l.faces[name] = map[string][]byte{}
	}
	l.nextID++
	id := fmt.Sprintf("%s-%04d.webp", name, l.nextID)
	l.faces[name][id] = data
	if l.record("register", name) {
		writeStatusJSON(w, http.StatusBadGateway, map[string]any{"success": false})
		return
	}
	writeJSON(w, map[string]any{"success": true, "message": "Successfully registered " + id + "."})
}

func (s *Server) deleteFaceImages(w http.ResponseWriter, r *http.Request) {
	if !s.requireFaces(w) {
		return
	}
	var body struct {
		IDs []string `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeStatusJSON(w, http.StatusUnprocessableEntity, map[string]any{"detail": "invalid body"})
		return
	}
	l := s.lib()
	l.mu.Lock()
	defer l.mu.Unlock()
	name := r.PathValue("name")
	if name == "train" {
		for _, id := range body.IDs {
			delete(l.train, id)
		}
		writeJSON(w, map[string]any{"success": true, "message": "Successfully deleted faces."})
		return
	}
	for _, id := range body.IDs {
		delete(l.faces[name], id)
	}
	// Frigate removes the folder once its last image is gone.
	if imgs, ok := l.faces[name]; ok && len(imgs) == 0 {
		delete(l.faces, name)
	}
	if l.record("delete", name) {
		writeStatusJSON(w, http.StatusBadGateway, map[string]any{"success": false})
		return
	}
	writeJSON(w, map[string]any{"success": true, "message": "Successfully deleted faces."})
}

func (s *Server) renameFace(w http.ResponseWriter, r *http.Request) {
	if s.FaceRenameUnsupported {
		http.Error(w, `{"detail":"Not Found"}`, http.StatusNotFound)
		return
	}
	if !s.requireFaces(w) {
		return
	}
	var body struct {
		NewName string `json:"new_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.NewName == "" {
		writeStatusJSON(w, http.StatusUnprocessableEntity, map[string]any{"detail": "new_name is required"})
		return
	}
	l := s.lib()
	l.mu.Lock()
	defer l.mu.Unlock()
	old := r.PathValue("old")
	imgs, ok := l.faces[old]
	if _, taken := l.faces[body.NewName]; !ok || taken {
		writeStatusJSON(w, http.StatusBadRequest, map[string]any{"message": "Error renaming face. Check Frigate logs.", "success": false})
		return
	}
	delete(l.faces, old)
	l.faces[body.NewName] = imgs
	writeJSON(w, map[string]any{"success": true, "message": "Successfully renamed face to " + body.NewName + "."})
}

func (s *Server) classifyTrain(w http.ResponseWriter, r *http.Request) {
	if !s.requireFaces(w) {
		return
	}
	var body struct {
		TrainingFile string `json:"training_file"`
		EventID      string `json:"event_id"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if body.TrainingFile == "" && body.EventID == "" {
		writeStatusJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "A training file or event_id must be passed."})
		return
	}
	l := s.lib()
	l.mu.Lock()
	defer l.mu.Unlock()
	data, ok := l.train[body.TrainingFile]
	if !ok {
		writeStatusJSON(w, http.StatusNotFound, map[string]any{"success": false, "message": "Invalid filename or no file exists: " + body.TrainingFile})
		return
	}
	name := r.PathValue("name")
	if l.faces[name] == nil {
		l.faces[name] = map[string][]byte{}
	}
	delete(l.train, body.TrainingFile)
	l.nextID++
	id := fmt.Sprintf("%s-%04d.webp", name, l.nextID)
	l.faces[name][id] = data
	writeJSON(w, map[string]any{"success": true, "message": "Successfully saved " + body.TrainingFile + " as " + id + "."})
}
