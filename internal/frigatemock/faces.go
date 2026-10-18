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
	for _, id := range body.IDs {
		delete(l.faces[name], id)
	}
	// Frigate removes the folder once its last image is gone.
	if imgs, ok := l.faces[name]; ok && len(imgs) == 0 {
		delete(l.faces, name)
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
