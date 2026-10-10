package mtls

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
)

// ErrNoCredentials means the state directory holds no certificate yet.
var ErrNoCredentials = errors.New("mtls: no stored credentials")

const (
	keyFile   = "key.pem"
	certFile  = "cert.pem"
	caFile    = "ca.pem"
	tokenFile = "enroll-token.sha256"
	pending   = ".new"
)

// Store keeps the credentials in Dir as key.pem (0600), cert.pem and ca.pem. Writes go to a
// temporary file that is synced and renamed over the target, so a crash never leaves a
// half-written file. A save stages all three files as .new first and renames them last, so
// Load can finish a save that was interrupted at any point.
type Store struct{ Dir string }

func (s Store) path(name string) string { return filepath.Join(s.Dir, name) }

// CheckWritable creates the state directory if needed and proves that files can be created in
// it. Enrollment calls it before redeeming the one-time token, so an unusable directory fails
// early and does not spend a token that could not be stored.
func (s Store) CheckWritable() error {
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return fmt.Errorf("state directory %s is not usable: %w", s.Dir, err)
	}
	probe, err := os.CreateTemp(s.Dir, ".write-probe-*")
	if err != nil {
		return fmt.Errorf("state directory %s is not writable: %w", s.Dir, err)
	}
	name := probe.Name()
	if err := probe.Close(); err != nil {
		_ = os.Remove(name)
		return fmt.Errorf("state directory %s is not writable: %w", s.Dir, err)
	}
	if err := os.Remove(name); err != nil {
		return fmt.Errorf("state directory %s is not writable: %w", s.Dir, err)
	}
	return nil
}

// stored is one credential file as found on disk: the final file and/or its staged .new copy.
type stored struct {
	name          string
	final, staged []byte
}

func (s Store) read(name string) (stored, error) {
	out := stored{name: name}
	var err error
	if out.final, err = readOptional(s.path(name)); err != nil {
		return out, err
	}
	out.staged, err = readOptional(s.path(name + pending))
	return out, err
}

func readOptional(path string) ([]byte, error) {
	b, err := os.ReadFile(path)
	// ENOTDIR: the state path runs through a regular file; there are no credentials there, and
	// CheckWritable reports the real problem before a token is redeemed.
	if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) {
		return nil, nil
	}
	return b, err
}

// pick returns the staged content when useStaged and present, the final one otherwise.
func (f stored) pick(useStaged bool) (data []byte, staged, ok bool) {
	if useStaged && f.staged != nil {
		return f.staged, true, true
	}
	if f.final != nil {
		return f.final, false, true
	}
	return nil, false, false
}

// Load returns the stored credentials, or ErrNoCredentials when there is no key or no
// certificate at all (final or staged). Anything else wrong with the files is a real error,
// never a reason to enroll again: the one-time token is gone.
//
// A save that was interrupted leaves a mix of final and staged (.new) files, whether it was the
// first save or a renewal. Load tries the combinations of those files, newest first, and uses the
// first set that forms a valid credential (key matches certificate, which chains to the CA); it
// then promotes the staged files it used. A complete staged save always yields such a set.
func (s Store) Load() (*Credentials, error) {
	key, err := s.read(keyFile)
	if err != nil {
		return nil, err
	}
	cert, err := s.read(certFile)
	if err != nil {
		return nil, err
	}
	ca, err := s.read(caFile)
	if err != nil {
		return nil, err
	}
	if (key.final == nil && key.staged == nil) || (cert.final == nil && cert.staged == nil) {
		return nil, ErrNoCredentials
	}
	var firstErr error
	// Bit i set means "use the staged copy of file i"; more staged files first.
	for _, mask := range []int{7, 3, 5, 6, 1, 2, 4, 0} {
		k, ks, kok := key.pick(mask&1 != 0)
		c, cs, cok := cert.pick(mask&2 != 0)
		a, as, aok := ca.pick(mask&4 != 0)
		if !kok || !cok || !aok || (ks != (mask&1 != 0)) || (cs != (mask&2 != 0)) || (as != (mask&4 != 0)) {
			continue
		}
		creds, err := NewCredentials(k, c, a)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		for _, f := range []struct {
			name   string
			staged bool
		}{{caFile, as}, {keyFile, ks}, {certFile, cs}} {
			if f.staged {
				if err := os.Rename(s.path(f.name+pending), s.path(f.name)); err != nil {
					return nil, err
				}
			}
		}
		return creds, nil
	}
	if firstErr == nil {
		firstErr = errors.New("a credential file is missing")
	}
	return nil, fmt.Errorf("stored credentials in %s are unusable: %w", s.Dir, firstErr)
}

// Save writes the credentials: all three files are staged first, then renamed (CA, key,
// certificate). At every point of the renames some combination of final and staged files is a
// complete valid set, which Load finds.
func (s Store) Save(c *Credentials) error {
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return fmt.Errorf("create state directory: %w", err)
	}
	if err := writeFile(s.path(caFile+pending), c.CAPEM, 0o644); err != nil {
		return err
	}
	if err := writeFile(s.path(keyFile+pending), c.KeyPEM, 0o600); err != nil {
		return err
	}
	if err := writeFile(s.path(certFile+pending), c.CertPEM, 0o644); err != nil {
		return err
	}
	for _, name := range []string{caFile, keyFile, certFile} {
		if err := os.Rename(s.path(name+pending), s.path(name)); err != nil {
			return err
		}
	}
	return syncDir(s.Dir)
}

// TokenUsed reports whether token is the one this agent already enrolled with. Only its
// hash is stored.
func (s Store) TokenUsed(token string) bool {
	got, err := os.ReadFile(s.path(tokenFile))
	return err == nil && bytes.Equal(bytes.TrimSpace(got), []byte(tokenHash(token)))
}

// RecordToken remembers that token was redeemed.
func (s Store) RecordToken(token string) error {
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return err
	}
	return writeFile(s.path(tokenFile), []byte(tokenHash(token)+"\n"), 0o600)
}

func tokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// writeFile writes data to a temporary file in the same directory, syncs it and renames it
// over path.
func writeFile(path string, data []byte, perm os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // no-op after a successful rename
	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
