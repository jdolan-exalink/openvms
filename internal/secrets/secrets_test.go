package secrets

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"testing"
)

func newTestSealer(t *testing.T) *Sealer {
	t.Helper()
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	s, err := NewSealer(key)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestRoundTrip(t *testing.T) {
	s := newTestSealer(t)
	sealed, err := s.Seal([]byte("frigate-pass"), []byte("server-1"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealed, []byte("frigate-pass")) {
		t.Fatal("plaintext visible in sealed value")
	}
	got, err := s.Open(sealed, []byte("server-1"))
	if err != nil || string(got) != "frigate-pass" {
		t.Fatalf("open: %q, %v", got, err)
	}
}

func TestBoundToAAD(t *testing.T) {
	s := newTestSealer(t)
	sealed, _ := s.Seal([]byte("x"), []byte("server-1"))
	if _, err := s.Open(sealed, []byte("server-2")); err == nil {
		t.Fatal("value sealed for one row must not open for another")
	}
}

func TestWrongKey(t *testing.T) {
	sealed, _ := newTestSealer(t).Seal([]byte("x"), nil)
	if _, err := newTestSealer(t).Open(sealed, nil); err == nil {
		t.Fatal("another key must not open the value")
	}
}

func TestNoncesDiffer(t *testing.T) {
	s := newTestSealer(t)
	a, _ := s.Seal([]byte("x"), nil)
	b, _ := s.Seal([]byte("x"), nil)
	if bytes.Equal(a, b) {
		t.Fatal("sealing twice must not produce identical output")
	}
}

func TestFromEnv(t *testing.T) {
	t.Setenv("OPENVMS_MASTER_KEY", "")
	t.Setenv("OPENVMS_MASTER_KEY_FILE", "")
	if _, err := FromEnv(); !errors.Is(err, ErrNoKey) {
		t.Fatalf("want ErrNoKey, got %v", err)
	}
	t.Setenv("OPENVMS_MASTER_KEY", base64.StdEncoding.EncodeToString([]byte("short")))
	if _, err := FromEnv(); err == nil {
		t.Fatal("short key must be rejected")
	}
	t.Setenv("OPENVMS_MASTER_KEY", base64.StdEncoding.EncodeToString(make([]byte, 32)))
	if _, err := FromEnv(); err != nil {
		t.Fatal(err)
	}
}
