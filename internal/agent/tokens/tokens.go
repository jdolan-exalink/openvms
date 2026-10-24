// Package tokens is the edge agent's own API tokens: named, scoped, optionally expiring secrets that
// an operator creates from the agent's console and that OpenVMS (or anything else) presents as a
// bearer. Only a hash of each secret is stored. The agent never writes the file; the console command
// does, and a running agent notices the change on its next request.
package tokens

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
)

// Scope is one kind of thing a token may do.
type Scope string

const (
	// ScopeRead covers the agent's metrics, health and read-only status routes.
	ScopeRead Scope = "read"
	// ScopeEvents covers collecting and acknowledging queued camera readings.
	ScopeEvents Scope = "events"
	// ScopeControl covers routes that act on cameras and on the receiver: ONVIF operations,
	// discovery, the camera registry and configuration.
	ScopeControl Scope = "control"
	// ScopeUpdate covers replacing the agent binary.
	ScopeUpdate Scope = "update"
)

// AllScopes is every scope, in the order they are listed to an operator.
var AllScopes = []Scope{ScopeRead, ScopeEvents, ScopeControl, ScopeUpdate}

const (
	prefix      = "ovmsa"
	fileVersion = 1
)

var namePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,31}$`)

// Token is the stored description of a token; the secret itself is never kept.
type Token struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Scopes    []Scope    `json:"scopes"`
	Hash      string     `json:"hash"`
	CreatedAt time.Time  `json:"created_at"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

type fileContent struct {
	Version int     `json:"version"`
	Tokens  []Token `json:"tokens"`
}

// Store reads and writes the token file. It is safe for concurrent use and for several processes.
type Store struct {
	path string

	mu       sync.Mutex
	cached   []Token
	loadErr  error
	lastInfo os.FileInfo
	hasCache bool
}

// Open returns a store for the file at path. The file is created by the first token.
func Open(path string) *Store { return &Store{path: path} }

// ParseScopes reads a comma separated list of scopes, or "all".
func ParseScopes(raw string) ([]Scope, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, errors.New("at least one scope is required")
	}
	if raw == "all" {
		return slices.Clone(AllScopes), nil
	}
	var out []Scope
	for _, part := range strings.Split(raw, ",") {
		scope := Scope(strings.TrimSpace(part))
		if !slices.Contains(AllScopes, scope) {
			return nil, fmt.Errorf("unknown scope %q (valid: read, events, control, update, all)", scope)
		}
		if !slices.Contains(out, scope) {
			out = append(out, scope)
		}
	}
	return out, nil
}

// Create makes a token and returns its secret, which is shown once and cannot be recovered. A ttl of
// zero means the token does not expire.
func (s *Store) Create(name string, scopes []Scope, ttl time.Duration, now time.Time) (string, Token, error) {
	if !namePattern.MatchString(name) {
		return "", Token{}, errors.New("the name must be 1-32 characters: lowercase letters, digits, '.', '_' or '-'")
	}
	if len(scopes) == 0 {
		return "", Token{}, errors.New("at least one scope is required")
	}
	for _, sc := range scopes {
		if !slices.Contains(AllScopes, sc) {
			return "", Token{}, fmt.Errorf("unknown scope %q", sc)
		}
	}
	if ttl < 0 {
		return "", Token{}, errors.New("the lifetime cannot be negative")
	}
	var secret string
	var created Token
	err := s.mutate(func(tokens []Token) ([]Token, error) {
		for _, t := range tokens {
			if t.Name == name {
				return nil, fmt.Errorf("a token named %q exists; rotate it to replace its secret", name)
			}
		}
		var err error
		secret, created, err = newToken(name, scopes, ttl, now)
		return append(tokens, created), err
	})
	return secret, created, err
}

// Rotate replaces the secret of a token, keeping its name, scopes and lifetime. The old secret stops
// working at once.
func (s *Store) Rotate(idOrName string, now time.Time) (string, Token, error) {
	var secret string
	var rotated Token
	err := s.mutate(func(tokens []Token) ([]Token, error) {
		i := indexOf(tokens, idOrName)
		if i < 0 {
			return nil, fmt.Errorf("no token %q", idOrName)
		}
		old := tokens[i]
		var ttl time.Duration
		if old.ExpiresAt != nil {
			ttl = old.ExpiresAt.Sub(old.CreatedAt)
		}
		var err error
		secret, rotated, err = newToken(old.Name, old.Scopes, ttl, now)
		tokens[i] = rotated
		return tokens, err
	})
	return secret, rotated, err
}

// Revoke deletes a token by ID or name.
func (s *Store) Revoke(idOrName string) error {
	return s.mutate(func(tokens []Token) ([]Token, error) {
		i := indexOf(tokens, idOrName)
		if i < 0 {
			return nil, fmt.Errorf("no token %q", idOrName)
		}
		return slices.Delete(tokens, i, i+1), nil
	})
}

// LoadError reports why the token file cannot be used (unreadable, corrupt, an unknown version), or
// nil when it can, including when it does not exist yet.
func (s *Store) LoadError() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refreshLocked()
	return s.loadErr
}

// LooksLikeToken reports whether a bearer value has the shape of a console token, without checking it.
func LooksLikeToken(presented string) bool {
	_, _, ok := parse(presented)
	return ok
}

// List returns the stored tokens (without secrets).
func (s *Store) List() ([]Token, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refreshLocked()
	return slices.Clone(s.cached), s.loadErr
}

// Authenticate checks a presented bearer value against the stored tokens and the scope a route needs.
// An unreadable file refuses everything.
func (s *Store) Authenticate(presented string, scope Scope, now time.Time) (Token, bool) {
	id, secret, ok := parse(presented)
	if !ok {
		return Token{}, false
	}
	s.mu.Lock()
	s.refreshLocked()
	tokens, loadErr := s.cached, s.loadErr
	s.mu.Unlock()
	if loadErr != nil {
		return Token{}, false
	}
	sum := sha256.Sum256([]byte(secret))
	got := hex.EncodeToString(sum[:])
	for _, t := range tokens {
		if t.ID != id {
			continue
		}
		if subtle.ConstantTimeCompare([]byte(got), []byte(t.Hash)) != 1 {
			return Token{}, false
		}
		if t.ExpiresAt != nil && !now.Before(*t.ExpiresAt) {
			return Token{}, false
		}
		return t, slices.Contains(t.Scopes, scope)
	}
	return Token{}, false
}

func newToken(name string, scopes []Scope, ttl time.Duration, now time.Time) (string, Token, error) {
	idBytes, secretBytes := make([]byte, 6), make([]byte, 32)
	if _, err := rand.Read(idBytes); err != nil {
		return "", Token{}, err
	}
	if _, err := rand.Read(secretBytes); err != nil {
		return "", Token{}, err
	}
	id, secret := hex.EncodeToString(idBytes), hex.EncodeToString(secretBytes)
	sum := sha256.Sum256([]byte(secret))
	t := Token{ID: id, Name: name, Scopes: slices.Clone(scopes), Hash: hex.EncodeToString(sum[:]), CreatedAt: now.UTC()}
	if ttl > 0 {
		exp := now.UTC().Add(ttl)
		t.ExpiresAt = &exp
	}
	return fmt.Sprintf("%s_%s_%s", prefix, id, secret), t, nil
}

// parse splits "ovmsa_<id>_<secret>" and checks the shape, so a malformed value never reaches the lookup.
func parse(presented string) (id, secret string, ok bool) {
	parts := strings.Split(presented, "_")
	if len(parts) != 3 || parts[0] != prefix || len(parts[1]) != 12 || len(parts[2]) != 64 {
		return "", "", false
	}
	if _, err := hex.DecodeString(parts[1]); err != nil {
		return "", "", false
	}
	if _, err := hex.DecodeString(parts[2]); err != nil {
		return "", "", false
	}
	return parts[1], parts[2], true
}

func indexOf(tokens []Token, idOrName string) int {
	return slices.IndexFunc(tokens, func(t Token) bool { return t.ID == idOrName || t.Name == idOrName })
}

// refreshLocked reloads the file when it changed on disk. A file that cannot be read or parsed is
// remembered as an error, never as "no tokens".
func (s *Store) refreshLocked() {
	info, err := os.Stat(s.path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		s.cached, s.loadErr, s.hasCache = nil, nil, true
		return
	case err != nil:
		s.cached, s.loadErr, s.hasCache = nil, err, true
		return
	}
	// The console replaces the file by renaming a new one over it, so a new inode is a change even
	// when the size and the clock tick are the same (a rotated secret has the same length).
	if s.hasCache && s.lastInfo != nil && os.SameFile(s.lastInfo, info) && info.ModTime().Equal(s.lastInfo.ModTime()) && info.Size() == s.lastInfo.Size() {
		return
	}
	s.cached, s.loadErr = readFile(s.path)
	s.lastInfo, s.hasCache = info, true
}

func readFile(path string) ([]Token, error) {
	data, err := os.ReadFile(path) //nolint:gosec // the agent's own token file
	if err != nil {
		return nil, err
	}
	var content fileContent
	if err := json.Unmarshal(data, &content); err != nil {
		return nil, fmt.Errorf("token file is not readable: %w", err)
	}
	if content.Version != fileVersion {
		return nil, fmt.Errorf("token file has version %d, this agent understands %d", content.Version, fileVersion)
	}
	return content.Tokens, nil
}

// mutate applies change to the tokens on disk under a cross-process lock and writes the result
// atomically. It refuses to write over a file it could not read.
func (s *Store) mutate(change func([]Token) ([]Token, error)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(s.path), 0o750); err != nil {
		return err
	}
	unlock, err := lockFile(s.path + ".lock")
	if err != nil {
		return err
	}
	defer unlock()

	var current []Token
	if _, statErr := os.Stat(s.path); statErr == nil {
		if current, err = readFile(s.path); err != nil {
			return err
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return statErr
	}
	next, err := change(slices.Clone(current))
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(fileContent{Version: fileVersion, Tokens: next}, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600) //nolint:gosec // the agent's own token file
	if err != nil {
		return err
	}
	_, werr := f.Write(data)
	if werr == nil {
		werr = f.Sync()
	}
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		_ = os.Remove(tmp)
		return werr
	}
	if err := os.Rename(tmp, s.path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	s.hasCache = false // reread on next use
	return nil
}
