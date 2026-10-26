package backup

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// #nosec G101 -- a fake RTSP URL used as a test fixture, not a credential.
const sample = "mqtt:\n  enabled: false\ncameras:\n  front:\n    ffmpeg:\n      inputs:\n        - path: rtsp://admin:secret@10.0.0.5/stream\n"

func TestSaveStoresTheConfigAndMetadataPrivately(t *testing.T) {
	state := t.TempDir()
	s := New(state)
	meta, err := s.Save(sample, "0.16.1", "OpenVMS Central https://vms.example.com")
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(sample))
	if meta.SHA256 != hex.EncodeToString(sum[:]) || meta.Size != len(sample) || meta.FrigateVersion != "0.16.1" || meta.RegisteredBy == "" || meta.SavedAt.IsZero() {
		t.Fatalf("meta = %+v", meta)
	}
	if meta.SavedAt.Location().String() != "UTC" {
		t.Fatalf("saved_at must be UTC, got %v", meta.SavedAt.Location())
	}
	dir := filepath.Join(state, "backup")
	if st, err := os.Stat(dir); err != nil || st.Mode().Perm() != 0o700 {
		t.Fatalf("dir = %v %v, want 0700", st, err)
	}
	for _, name := range []string{"frigate-config.original.yml", "frigate-config.original.json"} {
		st, err := os.Stat(filepath.Join(dir, name))
		if err != nil || st.Mode().Perm() != 0o600 {
			t.Fatalf("%s = %v %v, want 0600", name, st, err)
		}
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 2 {
		t.Fatalf("temp files left behind: %v", entries)
	}
	got, loaded, err := s.Load()
	if err != nil || got != sample || loaded != meta {
		t.Fatalf("load = %q %+v %v, want the same back", got, loaded, err)
	}
}

func TestFirstWriteWins(t *testing.T) {
	s := New(t.TempDir())
	first, err := s.Save(sample, "0.16.1", "a")
	if err != nil {
		t.Fatal(err)
	}
	existing, err := s.Save("other: 1\n", "0.17", "b")
	if !errors.Is(err, ErrExists) {
		t.Fatalf("second save = %v, want ErrExists", err)
	}
	if existing != first {
		t.Fatalf("ErrExists must carry the stored metadata: %+v vs %+v", existing, first)
	}
	if got, _, _ := s.Load(); got != sample {
		t.Fatalf("the original was overwritten: %q", got)
	}
}

func TestParallelWritersLeaveExactlyOneWinner(t *testing.T) {
	s := New(t.TempDir())
	const n = 24
	var wg sync.WaitGroup
	results := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, results[i] = s.Save("writer: "+strings.Repeat("x", i+1)+"\n", "v", "w")
		}()
	}
	wg.Wait()
	wins := 0
	for _, err := range results {
		switch {
		case err == nil:
			wins++
		case !errors.Is(err, ErrExists):
			t.Fatalf("unexpected error %v", err)
		}
	}
	if wins != 1 {
		t.Fatalf("%d writers won, want exactly 1", wins)
	}
	cfg, meta, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(cfg))
	if meta.SHA256 != hex.EncodeToString(sum[:]) {
		t.Fatal("metadata does not describe the stored file")
	}
	entries, _ := os.ReadDir(filepath.Join(s.dir))
	if len(entries) != 2 {
		t.Fatalf("temp files left behind: %v", entries)
	}
}

func TestSaveRejectsBadConfigs(t *testing.T) {
	for name, cfg := range map[string]string{
		"empty":        "",
		"blank":        "  \n\n",
		"oversize":     "a: " + strings.Repeat("x", MaxConfigBytes) + "\n",
		"not utf8":     "a: \xff\xfe\n",
		"not yaml":     "a: [unclosed\n",
		"not a map":    "- one\n- two\n",
		"plain scalar": "hello\n",
	} {
		s := New(t.TempDir())
		if _, err := s.Save(cfg, "v", "r"); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: err = %v, want ErrInvalid", name, err)
		}
		if _, _, err := s.Load(); !errors.Is(err, ErrNotFound) {
			t.Errorf("%s: a rejected config must leave nothing stored, load = %v", name, err)
		}
	}
}

func TestSaveBoundsTheFreeTextFields(t *testing.T) {
	s := New(t.TempDir())
	if _, err := s.Save(sample, "v", strings.Repeat("a", MaxRegisteredByLen+1)); !errors.Is(err, ErrInvalid) {
		t.Fatalf("long registered_by = %v, want ErrInvalid", err)
	}
	if _, err := s.Save(sample, strings.Repeat("a", 65), "r"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("long frigate_version = %v, want ErrInvalid", err)
	}
}

func TestLoadAndDeleteWithoutABackup(t *testing.T) {
	s := New(t.TempDir())
	if _, _, err := s.Load(); !errors.Is(err, ErrNotFound) {
		t.Fatalf("load = %v", err)
	}
	if _, err := s.Meta(); !errors.Is(err, ErrNotFound) {
		t.Fatalf("meta = %v", err)
	}
	if err := s.Delete(); !errors.Is(err, ErrNotFound) {
		t.Fatalf("delete = %v", err)
	}
}

func TestDeleteAllowsANewFirstWrite(t *testing.T) {
	s := New(t.TempDir())
	if _, err := s.Save(sample, "v", "r"); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Load(); !errors.Is(err, ErrNotFound) {
		t.Fatalf("after delete: %v", err)
	}
	if _, err := s.Save("again: 1\n", "v", "r"); err != nil {
		t.Fatalf("save after delete: %v", err)
	}
}

func TestMetaSurvivesAMissingMetadataFile(t *testing.T) {
	// A crash between the two renames leaves the yaml alone: the metadata is rebuilt from it.
	s := New(t.TempDir())
	want, _ := s.Save(sample, "v", "r")
	if err := os.Remove(filepath.Join(s.dir, metaName)); err != nil {
		t.Fatal(err)
	}
	got, err := s.Meta()
	if err != nil || got.SHA256 != want.SHA256 || got.Size != want.Size || got.SavedAt.IsZero() {
		t.Fatalf("meta = %+v %v", got, err)
	}
}
