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
// half-written file.
type Store struct{ Dir string }

func (s Store) path(name string) string { return filepath.Join(s.Dir, name) }

// Load returns the stored credentials, or ErrNoCredentials when there is no key and
// certificate at all. Anything else wrong with the files is a real error, never a reason to
// enroll again: the one-time token is gone.
func (s Store) Load() (*Credentials, error) {
	key, err := os.ReadFile(s.path(keyFile))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNoCredentials
	}
	if err != nil {
		return nil, err
	}
	cert, err := os.ReadFile(s.path(certFile))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNoCredentials
	}
	if err != nil {
		return nil, err
	}
	ca, err := os.ReadFile(s.path(caFile))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", caFile, err)
	}
	creds, err := NewCredentials(key, cert, ca)
	if err == nil {
		return creds, nil
	}
	// A renewal that crashed between its two renames leaves the new key next to the old
	// certificate and the new certificate in cert.pem.new: finish it.
	if next, nerr := os.ReadFile(s.path(certFile + pending)); nerr == nil {
		if recovered, rerr := NewCredentials(key, next, ca); rerr == nil {
			if werr := os.Rename(s.path(certFile+pending), s.path(certFile)); werr != nil {
				return nil, werr
			}
			return recovered, nil
		}
	}
	return nil, fmt.Errorf("stored credentials in %s are unusable: %w", s.Dir, err)
}

// Save writes the credentials. The new key and certificate are staged as .new files first and
// renamed last, certificate after key, so Load can finish an interrupted save.
func (s Store) Save(c *Credentials) error {
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return fmt.Errorf("create state directory: %w", err)
	}
	if err := writeFile(s.path(caFile), c.CAPEM, 0o644); err != nil {
		return err
	}
	if err := writeFile(s.path(keyFile+pending), c.KeyPEM, 0o600); err != nil {
		return err
	}
	if err := writeFile(s.path(certFile+pending), c.CertPEM, 0o644); err != nil {
		return err
	}
	if err := os.Rename(s.path(keyFile+pending), s.path(keyFile)); err != nil {
		return err
	}
	if err := os.Rename(s.path(certFile+pending), s.path(certFile)); err != nil {
		return err
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
