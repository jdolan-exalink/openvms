package provisionlpr

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	// DefaultCaptureMaxFiles and DefaultCaptureMaxBytes bound what the store keeps on disk. The
	// oldest captures are removed first.
	DefaultCaptureMaxFiles = 200
	DefaultCaptureMaxBytes = 64 << 20

	// Capture reasons.
	CaptureUnparsed = "unparsed"    // the parser could not read the request
	CaptureAll      = "capture_all" // the operator asked to keep every event request
	// CaptureUnregistered is a request an enforcing registry turned away; it is kept as evidence.
	CaptureUnregistered = "unregistered"
)

// captureIDPattern is the only shape of ID the store reads from disk, so a crafted ID can never
// leave the capture directory.
var captureIDPattern = regexp.MustCompile(`^[0-9]{8}T[0-9]{15}Z-[0-9a-f]{12}$`)

// credentialHeaders are never stored: a capture is meant to show the camera's protocol, not its secrets.
var credentialHeaders = map[string]bool{
	"authorization": true, "proxy-authorization": true, "cookie": true, "set-cookie": true,
	"x-api-key": true, "x-auth-token": true,
}

// Capture describes one request kept verbatim. The body lives next to it, byte for byte, so the
// real protocol of a camera or firmware can be read from it later. Bodies can hold plates and
// images: they stay on the agent and are only served through authenticated routes.
type Capture struct {
	ID          string              `json:"id"`
	ReceivedAt  time.Time           `json:"received_at"`
	Reason      string              `json:"reason"`
	ParseError  string              `json:"parse_error,omitempty"`
	SourceIP    string              `json:"source_ip"`
	Method      string              `json:"method"`
	Path        string              `json:"path"`
	Query       string              `json:"query,omitempty"`
	Headers     map[string][]string `json:"headers"`
	ContentType string              `json:"content_type,omitempty"`
	BodySize    int                 `json:"body_size"`
	BodySHA256  string              `json:"body_sha256"`
}

// CaptureStore keeps captures as <id>.json and <id>.body files in one directory.
type CaptureStore struct {
	dir      string
	maxFiles int
	maxBytes int64
	mu       sync.Mutex
}

// NewCaptureStore creates the directory (0700) and returns a store. Non-positive limits use the defaults.
func NewCaptureStore(dir string, maxFiles int, maxBytes int64) (*CaptureStore, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, errors.New("capture directory is required")
	}
	if maxFiles <= 0 {
		maxFiles = DefaultCaptureMaxFiles
	}
	if maxBytes <= 0 {
		maxBytes = DefaultCaptureMaxBytes
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create capture directory: %w", err)
	}
	return &CaptureStore{dir: dir, maxFiles: maxFiles, maxBytes: maxBytes}, nil
}

// Save stores the request and returns its description. reason says why it was kept and parseErr,
// when not empty, what the parser said.
func (s *CaptureStore) Save(req *http.Request, body []byte, reason, parseErr, sourceIP string, now time.Time) (Capture, error) {
	sum := sha256.Sum256(body)
	idSeed := sha256.Sum256([]byte(fmt.Sprintf("%d|%s|%x", now.UnixNano(), sourceIP, sum)))
	c := Capture{
		ID:          fmt.Sprintf("%s%09dZ-%s", now.UTC().Format("20060102T150405"), now.UTC().Nanosecond(), hex.EncodeToString(idSeed[:6])),
		ReceivedAt:  now.UTC(),
		Reason:      reason,
		ParseError:  truncate(parseErr, 300),
		SourceIP:    sourceIP,
		Method:      req.Method,
		Path:        truncate(req.URL.Path, 300),
		Query:       truncate(req.URL.RawQuery, 500),
		Headers:     redactHeaders(req.Header),
		ContentType: truncate(req.Header.Get("Content-Type"), 200),
		BodySize:    len(body),
		BodySHA256:  hex.EncodeToString(sum[:]),
	}
	meta, err := json.Marshal(c)
	if err != nil {
		return Capture{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// The body goes first and the description last, so a listing never shows half a capture.
	if err := writeFileAtomic(filepath.Join(s.dir, c.ID+".body"), body); err != nil {
		return Capture{}, err
	}
	if err := writeFileAtomic(filepath.Join(s.dir, c.ID+".json"), meta); err != nil {
		_ = os.Remove(filepath.Join(s.dir, c.ID+".body"))
		return Capture{}, err
	}
	s.pruneLocked(c.ID)
	return c, nil
}

// List returns up to limit captures, newest first. A non-positive limit means all of them.
func (s *CaptureStore) List(limit int) ([]Capture, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids, err := s.idsLocked()
	if err != nil {
		return nil, err
	}
	out := make([]Capture, 0, len(ids))
	for _, id := range ids {
		if limit > 0 && len(out) >= limit {
			break
		}
		if c, err := s.readMetaLocked(id); err == nil {
			out = append(out, c)
		}
	}
	return out, nil
}

// Get returns the description and the original body of one capture.
func (s *CaptureStore) Get(id string) (Capture, []byte, error) {
	if !captureIDPattern.MatchString(id) {
		return Capture{}, nil, os.ErrNotExist
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	c, err := s.readMetaLocked(id)
	if err != nil {
		return Capture{}, nil, err
	}
	body, err := os.ReadFile(filepath.Join(s.dir, id+".body")) //nolint:gosec // id was matched against captureIDPattern above.
	if err != nil {
		return Capture{}, nil, err
	}
	return c, body, nil
}

func (s *CaptureStore) readMetaLocked(id string) (Capture, error) {
	data, err := os.ReadFile(filepath.Join(s.dir, id+".json")) //nolint:gosec // id is checked against captureIDPattern by every caller.
	if err != nil {
		return Capture{}, err
	}
	var c Capture
	if err := json.Unmarshal(data, &c); err != nil {
		return Capture{}, err
	}
	return c, nil
}

// idsLocked lists the IDs of complete captures, newest first (IDs sort by time).
func (s *CaptureStore) idsLocked() ([]string, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, e := range entries {
		name := e.Name()
		if id, ok := strings.CutSuffix(name, ".json"); ok && captureIDPattern.MatchString(id) {
			ids = append(ids, id)
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(ids)))
	return ids, nil
}

// pruneLocked removes the oldest captures until the store fits its limits. keep is never removed.
func (s *CaptureStore) pruneLocked(keep string) {
	ids, err := s.idsLocked()
	if err != nil {
		return
	}
	sizes := make(map[string]int64, len(ids))
	var total int64
	for _, id := range ids {
		var n int64
		for _, ext := range []string{".json", ".body"} {
			if info, err := os.Stat(filepath.Join(s.dir, id+ext)); err == nil {
				n += info.Size()
			}
		}
		sizes[id] = n
		total += n
	}
	for i := len(ids) - 1; i >= 0 && (len(ids) > s.maxFiles || total > s.maxBytes); i-- {
		id := ids[i]
		if id == keep {
			continue
		}
		_ = os.Remove(filepath.Join(s.dir, id+".json"))
		_ = os.Remove(filepath.Join(s.dir, id+".body"))
		total -= sizes[id]
		ids = append(ids[:i], ids[i+1:]...)
	}
}

func writeFileAtomic(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// redactHeaders copies the headers without credentials and bounds how many are kept.
func redactHeaders(h http.Header) map[string][]string {
	out := make(map[string][]string, len(h))
	for name, values := range h {
		if credentialHeaders[strings.ToLower(name)] || len(out) >= 64 {
			continue
		}
		kept := make([]string, 0, len(values))
		for _, v := range values {
			kept = append(kept, truncate(v, 500))
		}
		out[http.CanonicalHeaderKey(name)] = kept
	}
	return out
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
