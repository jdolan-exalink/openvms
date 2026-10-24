package provisionlpr

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"regexp"
	"sort"
	"sync"
	"time"
)

const (
	maxRegisteredCameras = 1000
	maxUnregisteredSeen  = 100
)

var (
	// ErrIPInUse means another registered camera already has that address.
	ErrIPInUse = errors.New("another camera is registered with that address")
	// ErrInvalidCamera means the camera ID or address cannot be registered.
	ErrInvalidCamera = errors.New("invalid camera registration")
	// ErrRegistryFull means the registry holds as many cameras as it may.
	ErrRegistryFull = errors.New("too many registered cameras")

	cameraIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
)

// RegisteredCamera ties the ID OpenVMS knows a camera by to the address it connects from.
type RegisteredCamera struct {
	CameraID string `json:"camera_id"`
	IP       string `json:"ip"`
}

// UnregisteredSource is an address that sent requests without being registered. The list lets an
// operator see which camera still has to be registered before enforcement is turned on.
type UnregisteredSource struct {
	IP        string    `json:"ip"`
	FirstSeen time.Time `json:"first_seen"`
	LastSeen  time.Time `json:"last_seen"`
	Requests  int64     `json:"requests"`
	LastPath  string    `json:"last_path"`
	Rejected  bool      `json:"rejected"` // true when enforcement turned its requests away
}

// RegistrySnapshot is the registry as the control routes report it.
type RegistrySnapshot struct {
	// Enforce is true when requests from unregistered addresses are refused.
	Enforce      bool                 `json:"enforce"`
	Cameras      []RegisteredCamera   `json:"cameras"`
	Unregistered []UnregisteredSource `json:"unregistered_seen"`
	// LoadError is set when the stored registry could not be read; the registry then refuses
	// everything instead of silently accepting anyone.
	LoadError string `json:"load_error,omitempty"`
}

type registryFile struct {
	Enforce bool               `json:"enforce"`
	Cameras []RegisteredCamera `json:"cameras"`
}

// Registry is the list of cameras allowed to talk to the receiver, kept on disk. Until an operator
// turns enforcement on it only identifies cameras (a registered address gets its registered ID) and
// everything is still accepted, so an upgrade does not cut cameras that already work.
type Registry struct {
	mu        sync.RWMutex
	path      string
	enforce   bool
	cameras   map[string]RegisteredCamera // by camera ID
	byIP      map[string]string           // address -> camera ID
	seen      map[string]*UnregisteredSource
	loadError string
}

// LoadRegistry reads the registry at path. A missing file is an empty registry that does not
// enforce. A file that cannot be read gives a registry that enforces with no cameras, together with
// the error, so a damaged file can never turn into "accept everyone".
func LoadRegistry(path string) (*Registry, error) {
	r := &Registry{path: path, cameras: map[string]RegisteredCamera{}, byIP: map[string]string{}, seen: map[string]*UnregisteredSource{}}
	data, err := os.ReadFile(path) //nolint:gosec // path is the agent's own state file.
	if errors.Is(err, os.ErrNotExist) {
		return r, nil
	}
	if err == nil {
		var f registryFile
		if err = json.Unmarshal(data, &f); err == nil {
			for _, c := range f.Cameras {
				if verr := r.addLocked(c); verr != nil {
					err = fmt.Errorf("stored camera %q: %w", c.CameraID, verr)
					break
				}
			}
			r.enforce = f.Enforce
		}
	}
	if err != nil {
		locked := &Registry{path: path, enforce: true, cameras: map[string]RegisteredCamera{}, byIP: map[string]string{}, seen: map[string]*UnregisteredSource{}, loadError: err.Error()}
		return locked, fmt.Errorf("read provision camera registry: %w", err)
	}
	return r, nil
}

// normalizeCamera validates a registration and returns it with the address in canonical form.
func normalizeCamera(c RegisteredCamera) (RegisteredCamera, error) {
	if !cameraIDPattern.MatchString(c.CameraID) {
		return c, fmt.Errorf("%w: camera_id must be 1 to 128 letters, digits, dots, colons, dashes or underscores", ErrInvalidCamera)
	}
	addr, err := netip.ParseAddr(c.IP)
	if err != nil {
		return c, fmt.Errorf("%w: ip is not an address", ErrInvalidCamera)
	}
	addr = addr.Unmap()
	if addr.IsUnspecified() || addr.IsMulticast() || addr.IsLoopback() {
		return c, fmt.Errorf("%w: ip must be the address the camera connects from", ErrInvalidCamera)
	}
	return RegisteredCamera{CameraID: c.CameraID, IP: addr.String()}, nil
}

func (r *Registry) addLocked(c RegisteredCamera) error {
	c, err := normalizeCamera(c)
	if err != nil {
		return err
	}
	if owner, taken := r.byIP[c.IP]; taken && owner != c.CameraID {
		return ErrIPInUse
	}
	if _, exists := r.cameras[c.CameraID]; !exists && len(r.cameras) >= maxRegisteredCameras {
		return ErrRegistryFull
	}
	if old, exists := r.cameras[c.CameraID]; exists {
		delete(r.byIP, old.IP)
	}
	r.cameras[c.CameraID] = c
	r.byIP[c.IP] = c.CameraID
	delete(r.seen, c.IP)
	return nil
}

// Upsert registers a camera or changes the address of a registered one.
func (r *Registry) Upsert(c RegisteredCamera) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	before := r.cloneLocked()
	if err := r.addLocked(c); err != nil {
		return err
	}
	if err := r.saveLocked(); err != nil {
		r.restoreLocked(before)
		return err
	}
	return nil
}

// Remove unregisters a camera. Removing one that is not registered is not an error.
func (r *Registry) Remove(cameraID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	c, ok := r.cameras[cameraID]
	if !ok {
		return nil
	}
	before := r.cloneLocked()
	delete(r.cameras, cameraID)
	delete(r.byIP, c.IP)
	if err := r.saveLocked(); err != nil {
		r.restoreLocked(before)
		return err
	}
	return nil
}

// SetEnforce turns enforcement on or off. A registry that failed to load cannot be changed: it has to
// be repaired on the host first, so the damaged state is not overwritten by accident.
func (r *Registry) SetEnforce(on bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.loadError != "" {
		return fmt.Errorf("registry unreadable: %s", r.loadError)
	}
	before := r.cloneLocked()
	r.enforce = on
	if err := r.saveLocked(); err != nil {
		r.restoreLocked(before)
		return err
	}
	return nil
}

// Enforcing reports whether unregistered addresses are refused.
func (r *Registry) Enforcing() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.enforce
}

// Identify returns the camera registered for an address.
func (r *Registry) Identify(ip string) (RegisteredCamera, bool) {
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return RegisteredCamera{}, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	id, ok := r.byIP[addr.Unmap().String()]
	if !ok {
		return RegisteredCamera{}, false
	}
	return r.cameras[id], true
}

// NoteUnregistered records that an address that is not registered sent a request.
func (r *Registry) NoteUnregistered(ip, path string, rejected bool, now time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.seen[ip]
	if !ok {
		if len(r.seen) >= maxUnregisteredSeen {
			var oldest string
			for k, v := range r.seen {
				if oldest == "" || v.LastSeen.Before(r.seen[oldest].LastSeen) {
					oldest = k
				}
			}
			delete(r.seen, oldest)
		}
		s = &UnregisteredSource{IP: ip, FirstSeen: now}
		r.seen[ip] = s
	}
	s.LastSeen, s.LastPath, s.Rejected = now, truncate(path, 200), rejected
	s.Requests++
}

// Snapshot returns the registry for reporting: cameras by ID, unregistered sources newest first.
func (r *Registry) Snapshot() RegistrySnapshot {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := RegistrySnapshot{Enforce: r.enforce, Cameras: make([]RegisteredCamera, 0, len(r.cameras)), Unregistered: make([]UnregisteredSource, 0, len(r.seen)), LoadError: r.loadError}
	for _, c := range r.cameras {
		out.Cameras = append(out.Cameras, c)
	}
	sort.Slice(out.Cameras, func(i, j int) bool { return out.Cameras[i].CameraID < out.Cameras[j].CameraID })
	for _, s := range r.seen {
		out.Unregistered = append(out.Unregistered, *s)
	}
	sort.Slice(out.Unregistered, func(i, j int) bool { return out.Unregistered[i].LastSeen.After(out.Unregistered[j].LastSeen) })
	return out
}

func (r *Registry) saveLocked() error {
	f := registryFile{Enforce: r.enforce, Cameras: make([]RegisteredCamera, 0, len(r.cameras))}
	for _, c := range r.cameras {
		f.Cameras = append(f.Cameras, c)
	}
	sort.Slice(f.Cameras, func(i, j int) bool { return f.Cameras[i].CameraID < f.Cameras[j].CameraID })
	data, err := json.Marshal(f)
	if err != nil {
		return err
	}
	return writeFileAtomic(r.path, data)
}

type registryState struct {
	enforce bool
	cameras map[string]RegisteredCamera
	byIP    map[string]string
}

func (r *Registry) cloneLocked() registryState {
	s := registryState{enforce: r.enforce, cameras: make(map[string]RegisteredCamera, len(r.cameras)), byIP: make(map[string]string, len(r.byIP))}
	for k, v := range r.cameras {
		s.cameras[k] = v
	}
	for k, v := range r.byIP {
		s.byIP[k] = v
	}
	return s
}

func (r *Registry) restoreLocked(s registryState) {
	r.enforce, r.cameras, r.byIP = s.enforce, s.cameras, s.byIP
}
