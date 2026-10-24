package provisionlpr

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
)

const registryBodyLimit = 4 << 10

// handleRegistry serves the registry control routes:
//
//	GET    /v1/provision-lpr/registry                  the registry and the unregistered sources seen
//	POST   /v1/provision-lpr/registry/cameras          register or move a camera {"camera_id", "ip"}
//	DELETE /v1/provision-lpr/registry/cameras/<id>     unregister a camera
//	PUT    /v1/provision-lpr/registry/enforce          {"enforce": true|false}
//
// The receiver does not authenticate: the agent mux puts these routes behind the agent token.
func (r *Receiver) handleRegistry(w http.ResponseWriter, req *http.Request) {
	if r.registry == nil {
		http.Error(w, "camera registry is not enabled", http.StatusNotFound)
		return
	}
	rest := strings.Trim(strings.TrimPrefix(req.URL.Path, "/v1/provision-lpr/registry"), "/")
	switch {
	case rest == "":
		if req.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		writeRegistryJSON(w, http.StatusOK, r.registry.Snapshot())
	case rest == "cameras":
		if req.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		var c RegisteredCamera
		if !decodeStrict(w, req, &c) {
			return
		}
		if err := r.registry.Upsert(c); err != nil {
			writeRegistryError(w, err)
			return
		}
		r.log.Info("provision-isr camera registered", "camera_id", c.CameraID)
		writeRegistryJSON(w, http.StatusOK, r.registry.Snapshot())
	case strings.HasPrefix(rest, "cameras/"):
		if req.Method != http.MethodDelete {
			methodNotAllowed(w, http.MethodDelete)
			return
		}
		id := strings.TrimPrefix(rest, "cameras/")
		if !cameraIDPattern.MatchString(id) {
			http.NotFound(w, req)
			return
		}
		if err := r.registry.Remove(id); err != nil {
			writeRegistryError(w, err)
			return
		}
		r.log.Info("provision-isr camera unregistered", "camera_id", id)
		w.WriteHeader(http.StatusNoContent)
	case rest == "enforce":
		if req.Method != http.MethodPut {
			methodNotAllowed(w, http.MethodPut)
			return
		}
		var body struct {
			Enforce *bool `json:"enforce"`
		}
		if !decodeStrict(w, req, &body) {
			return
		}
		if body.Enforce == nil {
			http.Error(w, `"enforce" is required`, http.StatusBadRequest)
			return
		}
		if err := r.registry.SetEnforce(*body.Enforce); err != nil {
			writeRegistryError(w, err)
			return
		}
		r.log.Info("provision-isr registry enforcement changed", "enforce", *body.Enforce)
		writeRegistryJSON(w, http.StatusOK, r.registry.Snapshot())
	default:
		http.NotFound(w, req)
	}
}

func methodNotAllowed(w http.ResponseWriter, allow string) {
	w.Header().Set("Allow", allow)
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}

// decodeStrict reads one small JSON object and refuses unknown fields, so a typo in a field name is
// an error instead of a silently ignored setting.
func decodeStrict(w http.ResponseWriter, req *http.Request, into any) bool {
	dec := json.NewDecoder(io.LimitReader(req.Body, registryBodyLimit))
	dec.DisallowUnknownFields()
	if err := dec.Decode(into); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return false
	}
	return true
}

func writeRegistryJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeRegistryError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrInvalidCamera):
		http.Error(w, err.Error(), http.StatusBadRequest)
	case errors.Is(err, ErrIPInUse), errors.Is(err, ErrRegistryFull), errors.Is(err, ErrRegistryLocked):
		http.Error(w, err.Error(), http.StatusConflict)
	default:
		http.Error(w, "registry could not be saved", http.StatusInternalServerError)
	}
}
