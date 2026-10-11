package mtls

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeState(t *testing.T, dir string, files map[string][]byte) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func exists(dir, name string) bool {
	_, err := os.Stat(filepath.Join(dir, name))
	return err == nil
}

func twoCredentials(t *testing.T) (a, b *Credentials) {
	t.Helper()
	var err error
	if a, err = newFakeAPI(t).enroll(context.Background(), "good-token"); err != nil {
		t.Fatal(err)
	}
	// A second enrollment against another API has another key, certificate and CA.
	if b, err = newFakeAPI(t).enroll(context.Background(), "good-token"); err != nil {
		t.Fatal(err)
	}
	return a, b
}

// An interrupted FIRST save leaves staged files and (at most) some finals. Every window must
// load the staged pair instead of reporting no credentials, or the agent would enroll again
// with a token it already spent.
func TestStoreLoadRecoversAnInterruptedFirstSave(t *testing.T) {
	windows := map[string]func(c *Credentials) map[string][]byte{
		"everything staged": func(c *Credentials) map[string][]byte {
			return map[string][]byte{"key.pem.new": c.KeyPEM, "cert.pem.new": c.CertPEM, "ca.pem.new": c.CAPEM}
		},
		"key renamed, certificate and CA staged": func(c *Credentials) map[string][]byte {
			return map[string][]byte{"key.pem": c.KeyPEM, "cert.pem.new": c.CertPEM, "ca.pem.new": c.CAPEM}
		},
		"key and CA renamed, certificate staged": func(c *Credentials) map[string][]byte {
			return map[string][]byte{"key.pem": c.KeyPEM, "ca.pem": c.CAPEM, "cert.pem.new": c.CertPEM}
		},
	}
	for name, files := range windows {
		t.Run(name, func(t *testing.T) {
			api := newFakeAPI(t)
			c, err := api.enroll(context.Background(), "good-token")
			if err != nil {
				t.Fatal(err)
			}
			dir := filepath.Join(t.TempDir(), "agent-mtls")
			writeState(t, dir, files(c))
			store := Store{Dir: dir}

			got, err := Ensure(context.Background(), store, "good-token", func(context.Context, string) (*Credentials, error) {
				t.Fatal("enrolled again although a complete save was staged")
				return nil, nil
			}, time.Now)
			if err != nil {
				t.Fatalf("Ensure: %v", err)
			}
			if got.Leaf.SerialNumber.Cmp(c.Leaf.SerialNumber) != 0 {
				t.Fatal("loaded another certificate")
			}
			for _, n := range []string{"key.pem", "cert.pem", "ca.pem"} {
				if !exists(dir, n) {
					t.Errorf("%s was not promoted", n)
				}
			}
			for _, n := range []string{"key.pem.new", "cert.pem.new", "ca.pem.new"} {
				if exists(dir, n) {
					t.Errorf("%s was left staged", n)
				}
			}
		})
	}
}

// A staged set that does not form a valid pair is an error, not a reason to enroll again.
func TestStoreLoadRefusesAStagedMismatch(t *testing.T) {
	a, b := twoCredentials(t)
	dir := filepath.Join(t.TempDir(), "agent-mtls")
	writeState(t, dir, map[string][]byte{"key.pem.new": a.KeyPEM, "cert.pem.new": b.CertPEM, "ca.pem.new": b.CAPEM})
	if _, err := (Store{Dir: dir}).Load(); err == nil || err == ErrNoCredentials {
		t.Fatalf("Load = %v, want an unusable-state error", err)
	}
	// Nothing at all is still "no credentials".
	if _, err := (Store{Dir: filepath.Join(t.TempDir(), "empty")}).Load(); err != ErrNoCredentials {
		t.Fatalf("Load of an empty directory = %v, want ErrNoCredentials", err)
	}
}

// The CA is staged and renamed with the key and certificate, so a CA change cannot be left half
// applied: an interrupted renewal that also changed the CA still loads the new, consistent set.
func TestStoreLoadRecoversAnInterruptedSaveThatChangesTheCA(t *testing.T) {
	old, next := twoCredentials(t)
	windows := map[string]map[string][]byte{
		"CA renamed first": {
			"key.pem": old.KeyPEM, "cert.pem": old.CertPEM, "ca.pem": next.CAPEM,
			"key.pem.new": next.KeyPEM, "cert.pem.new": next.CertPEM,
		},
		"nothing renamed yet": {
			"key.pem": old.KeyPEM, "cert.pem": old.CertPEM, "ca.pem": old.CAPEM,
			"key.pem.new": next.KeyPEM, "cert.pem.new": next.CertPEM, "ca.pem.new": next.CAPEM,
		},
		"key and CA renamed": {
			"key.pem": next.KeyPEM, "cert.pem": old.CertPEM, "ca.pem": next.CAPEM, "cert.pem.new": next.CertPEM,
		},
	}
	for name, files := range windows {
		t.Run(name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "agent-mtls")
			writeState(t, dir, files)
			got, err := (Store{Dir: dir}).Load()
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if got.Leaf.SerialNumber.Cmp(next.Leaf.SerialNumber) != 0 {
				t.Fatal("loaded the old set instead of the complete staged one")
			}
		})
	}
}

func TestStoreSaveChangingTheCAIsLoadable(t *testing.T) {
	old, next := twoCredentials(t)
	store := Store{Dir: filepath.Join(t.TempDir(), "agent-mtls")}
	if err := store.Save(old); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(next); err != nil {
		t.Fatal(err)
	}
	got, err := store.Load()
	if err != nil || got.Leaf.SerialNumber.Cmp(next.Leaf.SerialNumber) != 0 {
		t.Fatalf("Load after Save = %v, %v; want the new set", got, err)
	}
	for _, n := range []string{"key.pem.new", "cert.pem.new", "ca.pem.new"} {
		if exists(store.Dir, n) {
			t.Errorf("%s left behind by a completed Save", n)
		}
	}
}

// crashingStore returns a Store whose rename fails from the (okRenames+1)th call on, as a crash
// between two renames of a save would, and counts directory syncs.
func crashingStore(dir string, okRenames int) (Store, *int) {
	syncs := 0
	renames := 0
	return Store{
		Dir: dir,
		rename: func(from, to string) error {
			renames++
			if renames > okRenames {
				return errCrash
			}
			return os.Rename(from, to)
		},
		sync: func(string) error { syncs++; return nil },
	}, &syncs
}

var errCrash = errors.New("simulated crash")

// The first save renames ca.pem, then key.pem, then cert.pem. A crash after 0, 1, 2 or 3 of those
// renames must leave a state Load turns into the complete new credentials, never "no
// credentials" (which would spend another token).
func TestStoreFirstSaveCrashWindows(t *testing.T) {
	c, err := newFakeAPI(t).enroll(context.Background(), "good-token")
	if err != nil {
		t.Fatal(err)
	}
	for ok := 0; ok <= 3; ok++ {
		t.Run(fmt.Sprintf("crash after %d renames", ok), func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "agent-mtls")
			crashing, _ := crashingStore(dir, ok)
			err := crashing.Save(c)
			if (ok < 3) != (err != nil) {
				t.Fatalf("Save error = %v with %d renames allowed", err, ok)
			}
			got, err := (Store{Dir: dir}).Load()
			if err != nil {
				t.Fatalf("Load after the crash: %v", err)
			}
			if got.Leaf.SerialNumber.Cmp(c.Leaf.SerialNumber) != 0 {
				t.Fatal("loaded other credentials")
			}
			for _, n := range []string{"key.pem", "cert.pem", "ca.pem"} {
				if !exists(dir, n) {
					t.Errorf("%s missing after recovery", n)
				}
			}
			for _, n := range []string{"key.pem.new", "cert.pem.new", "ca.pem.new"} {
				if exists(dir, n) {
					t.Errorf("stale %s left after recovery", n)
				}
			}
		})
	}
}

// The same for a renewal that changes the CA: every window loads the new complete set.
func TestStoreRenewalCrashWindows(t *testing.T) {
	old, next := twoCredentials(t)
	for ok := 0; ok <= 2; ok++ {
		t.Run(fmt.Sprintf("crash after %d renames", ok), func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "agent-mtls")
			if err := (Store{Dir: dir}).Save(old); err != nil {
				t.Fatal(err)
			}
			crashing, _ := crashingStore(dir, ok)
			if err := crashing.Save(next); err == nil {
				t.Fatal("the simulated crash did not interrupt Save")
			}
			got, err := (Store{Dir: dir}).Load()
			if err != nil || got.Leaf.SerialNumber.Cmp(next.Leaf.SerialNumber) != 0 {
				t.Fatalf("Load = %v, %v; want the new credentials", got, err)
			}
			for _, n := range []string{"key.pem.new", "cert.pem.new", "ca.pem.new"} {
				if exists(dir, n) {
					t.Errorf("stale %s left after recovery", n)
				}
			}
		})
	}
}

// Leftovers that never formed a valid set must not linger: once the final files load, every
// staged file is removed.
func TestStoreLoadRemovesStaleStagedFiles(t *testing.T) {
	c, err := newFakeAPI(t).enroll(context.Background(), "good-token")
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "agent-mtls")
	if err := (Store{Dir: dir}).Save(c); err != nil {
		t.Fatal(err)
	}
	writeState(t, dir, map[string][]byte{"ca.pem.new": []byte("garbage"), "key.pem.new": []byte("garbage")})
	got, err := (Store{Dir: dir}).Load()
	if err != nil || got.Leaf.SerialNumber.Cmp(c.Leaf.SerialNumber) != 0 {
		t.Fatalf("Load = %v, %v", got, err)
	}
	for _, n := range []string{"key.pem.new", "cert.pem.new", "ca.pem.new"} {
		if exists(dir, n) {
			t.Errorf("stale %s left after Load", n)
		}
	}
}

// Renames are only durable once the directory is synced: Save syncs after staging (so the staged
// files survive a crash during the renames) and after the renames; recovery syncs too.
func TestStoreSyncsTheDirectoryAfterRenames(t *testing.T) {
	c, err := newFakeAPI(t).enroll(context.Background(), "good-token")
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "agent-mtls")
	s, syncs := crashingStore(dir, 99)
	if err := s.Save(c); err != nil {
		t.Fatal(err)
	}
	if *syncs < 2 {
		t.Fatalf("Save synced the directory %d times, want after staging and after the renames", *syncs)
	}

	recoverDir := filepath.Join(t.TempDir(), "agent-mtls")
	crashing, _ := crashingStore(recoverDir, 1)
	_ = crashing.Save(c)
	rec, recSyncs := crashingStore(recoverDir, 99)
	if _, err := rec.Load(); err != nil {
		t.Fatal(err)
	}
	if *recSyncs < 1 {
		t.Fatal("recovery promoted staged files without syncing the directory")
	}
}
