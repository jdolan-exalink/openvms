// Package secrets seals credentials (e.g. Frigate service accounts) with AES-256-GCM
// under a master key kept outside the database (PRD §73).
package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strings"
)

const formatV1 byte = 1

// ErrNoKey means no master key was configured.
var ErrNoKey = errors.New("master key not configured: set OPENVMS_MASTER_KEY or OPENVMS_MASTER_KEY_FILE")

type Sealer struct {
	aead cipher.AEAD
}

// NewSealer takes a 32-byte key.
func NewSealer(key []byte) (*Sealer, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("master key must be 32 bytes, got %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Sealer{aead: aead}, nil
}

// FromEnv loads a base64 key from OPENVMS_MASTER_KEY, or from the file named by
// OPENVMS_MASTER_KEY_FILE (Docker/Kubernetes secret).
func FromEnv() (*Sealer, error) {
	raw := os.Getenv("OPENVMS_MASTER_KEY")
	if path := os.Getenv("OPENVMS_MASTER_KEY_FILE"); raw == "" && path != "" {
		b, err := os.ReadFile(path) //nolint:gosec // path comes from operator configuration
		if err != nil {
			return nil, fmt.Errorf("read master key file: %w", err)
		}
		raw = string(b)
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, ErrNoKey
	}
	key, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf("master key is not valid base64: %w", err)
	}
	return NewSealer(key)
}

// Seal encrypts plaintext. aad binds the ciphertext to its owner (e.g. the row id),
// so a sealed value copied to another row fails to open.
func (s *Sealer) Seal(plaintext, aad []byte) ([]byte, error) {
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	out := make([]byte, 0, 1+len(nonce)+len(plaintext)+s.aead.Overhead())
	out = append(out, formatV1)
	out = append(out, nonce...)
	return s.aead.Seal(out, nonce, plaintext, aad), nil
}

func (s *Sealer) Open(sealed, aad []byte) ([]byte, error) {
	ns := s.aead.NonceSize()
	if len(sealed) < 1+ns || sealed[0] != formatV1 {
		return nil, errors.New("sealed value has an unknown format")
	}
	pt, err := s.aead.Open(nil, sealed[1:1+ns], sealed[1+ns:], aad)
	if err != nil {
		return nil, errors.New("sealed value cannot be opened with this key")
	}
	return pt, nil
}
