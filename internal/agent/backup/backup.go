// Package backup keeps, on the host, the Frigate config.yml as it was before OpenVMS first touched
// it. The file travels with the host, so another OpenVMS can adopt the server later and the
// operator can go back to the original configuration when the agent is removed.
//
// Only the Frigate configuration and a few facts about it are stored. Agent tokens and OpenVMS
// credentials never go in here. The first save wins and is never replaced: a second save returns
// ErrExists together with the metadata of what is already stored.
package backup

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

const (
	// MaxConfigBytes is the largest config.yml the store accepts.
	MaxConfigBytes = 2 << 20
	// MaxRegisteredByLen bounds the free text saying who registered the server.
	MaxRegisteredByLen = 200
	maxVersionLen      = 64

	dirName  = "backup"
	yamlName = "frigate-config.original.yml"
	metaName = "frigate-config.original.json"
)

var (
	// ErrExists means a backup is already stored; nothing was changed.
	ErrExists = errors.New("an original configuration backup already exists")
	// ErrNotFound means no backup is stored.
	ErrNotFound = errors.New("no original configuration backup")
	// ErrInvalid means the configuration or its metadata cannot be stored.
	ErrInvalid = errors.New("invalid backup")
)

// Meta describes a stored backup. It never carries the configuration itself.
type Meta struct {
	SavedAt        time.Time `json:"saved_at"`
	FrigateVersion string    `json:"frigate_version"`
	RegisteredBy   string    `json:"registered_by"`
	SHA256         string    `json:"sha256"`
	Size           int       `json:"size"`
}

// Store is the backup directory inside the agent's state directory.
type Store struct {
	dir string
}

// New returns the store under <stateDir>/backup. Nothing is created until the first save.
func New(stateDir string) *Store {
	return &Store{dir: filepath.Join(stateDir, dirName)}
}

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, args...))
}

// Validate checks that config can be stored: non-empty, within the size limit, valid UTF-8 and a
// YAML mapping. The returned errors never contain the text itself.
func Validate(config string) error {
	if strings.TrimSpace(config) == "" {
		return invalid("the configuration is empty")
	}
	if len(config) > MaxConfigBytes {
		return invalid("the configuration is larger than %d bytes", MaxConfigBytes)
	}
	if !utf8.ValidString(config) {
		return invalid("the configuration is not valid UTF-8")
	}
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(config), &doc); err != nil {
		return invalid("the configuration is not valid YAML")
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return invalid("the configuration must be a YAML mapping")
	}
	return nil
}

// Save stores config as the original, unless one already exists. When it does, the stored metadata
// comes back with ErrExists and nothing is written. Concurrent saves leave exactly one winner.
func (s *Store) Save(config, frigateVersion, registeredBy string) (Meta, error) {
	if err := Validate(config); err != nil {
		return Meta{}, err
	}
	frigateVersion = strings.TrimSpace(frigateVersion)
	registeredBy = strings.TrimSpace(registeredBy)
	if utf8.RuneCountInString(registeredBy) > MaxRegisteredByLen || !utf8.ValidString(registeredBy) {
		return Meta{}, invalid("registered_by is longer than %d characters", MaxRegisteredByLen)
	}
	if len(frigateVersion) > maxVersionLen || !utf8.ValidString(frigateVersion) {
		return Meta{}, invalid("frigate_version is longer than %d characters", maxVersionLen)
	}
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return Meta{}, fmt.Errorf("create the backup directory: %w", err)
	}
	// #nosec G302 -- 0700 is the strictest mode a directory can have and still be entered by its owner.
	if err := os.Chmod(s.dir, 0o700); err != nil {
		return Meta{}, fmt.Errorf("protect the backup directory: %w", err)
	}
	sum := sha256.Sum256([]byte(config))
	meta := Meta{
		SavedAt: time.Now().UTC().Truncate(time.Second), FrigateVersion: frigateVersion, RegisteredBy: registeredBy,
		SHA256: hex.EncodeToString(sum[:]), Size: len(config),
	}
	metaJSON, err := json.Marshal(meta)
	if err != nil {
		return Meta{}, err
	}

	tmpYAML, err := s.tempFile([]byte(config))
	if err != nil {
		return Meta{}, err
	}
	defer os.Remove(tmpYAML)
	tmpMeta, err := s.tempFile(metaJSON)
	if err != nil {
		return Meta{}, err
	}
	defer os.Remove(tmpMeta)

	// A hard link fails when the name exists, which is what makes the first writer the only one.
	if err := os.Link(tmpYAML, filepath.Join(s.dir, yamlName)); err != nil {
		if errors.Is(err, os.ErrExist) {
			existing, metaErr := s.Meta()
			if metaErr != nil {
				return Meta{}, fmt.Errorf("%w (its metadata is unreadable)", ErrExists)
			}
			return existing, ErrExists
		}
		return Meta{}, fmt.Errorf("store the backup: %w", err)
	}
	if err := os.Rename(tmpMeta, filepath.Join(s.dir, metaName)); err != nil {
		// The configuration is stored; the metadata is rebuilt from it when read.
		return meta, nil
	}
	return meta, nil
}

// tempFile writes data to a private file in the backup directory and returns its path.
func (s *Store) tempFile(data []byte) (string, error) {
	f, err := os.CreateTemp(s.dir, ".tmp-*")
	if err != nil {
		return "", fmt.Errorf("create a temporary backup file: %w", err)
	}
	name := f.Name()
	fail := func(err error) (string, error) {
		_ = f.Close()
		_ = os.Remove(name)
		return "", fmt.Errorf("write the backup: %w", err)
	}
	if err := f.Chmod(0o600); err != nil {
		return fail(err)
	}
	if _, err := f.Write(data); err != nil {
		return fail(err)
	}
	if err := f.Sync(); err != nil {
		return fail(err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(name)
		return "", fmt.Errorf("write the backup: %w", err)
	}
	return name, nil
}

// Load returns the stored configuration with its metadata.
func (s *Store) Load() (string, Meta, error) {
	data, err := os.ReadFile(filepath.Join(s.dir, yamlName))
	if errors.Is(err, os.ErrNotExist) {
		return "", Meta{}, ErrNotFound
	}
	if err != nil {
		return "", Meta{}, fmt.Errorf("read the backup: %w", err)
	}
	meta, err := s.metaFor(data)
	if err != nil {
		return "", Meta{}, err
	}
	return string(data), meta, nil
}

// Meta returns the metadata of the stored backup without the configuration.
func (s *Store) Meta() (Meta, error) {
	data, err := os.ReadFile(filepath.Join(s.dir, yamlName))
	if errors.Is(err, os.ErrNotExist) {
		return Meta{}, ErrNotFound
	}
	if err != nil {
		return Meta{}, fmt.Errorf("read the backup: %w", err)
	}
	return s.metaFor(data)
}

// metaFor reads the metadata file and checks it describes data. When it is missing (a save that was
// interrupted) or does not match, the facts that can be derived from the file are used instead.
func (s *Store) metaFor(data []byte) (Meta, error) {
	sum := sha256.Sum256(data)
	digest := hex.EncodeToString(sum[:])
	var meta Meta
	if raw, err := os.ReadFile(filepath.Join(s.dir, metaName)); err == nil {
		if json.Unmarshal(raw, &meta) == nil && meta.SHA256 == digest && meta.Size == len(data) {
			return meta, nil
		}
	}
	meta = Meta{SHA256: digest, Size: len(data)}
	if st, err := os.Stat(filepath.Join(s.dir, yamlName)); err == nil {
		meta.SavedAt = st.ModTime().UTC().Truncate(time.Second)
	}
	return meta, nil
}

// Delete removes the stored backup so a new original can be saved.
func (s *Store) Delete() error {
	err := os.Remove(filepath.Join(s.dir, yamlName))
	if errors.Is(err, os.ErrNotExist) {
		_ = os.Remove(filepath.Join(s.dir, metaName))
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("delete the backup: %w", err)
	}
	if err := os.Remove(filepath.Join(s.dir, metaName)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("delete the backup metadata: %w", err)
	}
	return nil
}
