package provisionlpr

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newCaptureRequest(path string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, path, nil)
	req.Header.Set("Content-Type", "application/octet-stream")
	return req
}

func TestCaptureStoreKeepsTheRequestVerbatimWithoutCredentials(t *testing.T) {
	store, err := NewCaptureStore(filepath.Join(t.TempDir(), "captures"), 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	req := newCaptureRequest("/odd/path?device=7")
	req.Header.Set("Authorization", "Basic c2VjcmV0")
	req.Header.Set("Cookie", "session=secret")
	req.Header.Set("X-Camera-Serial", "SN-123")
	body := []byte{0x00, 0xff, 'P', 'K', 0x03, 0x04, '\n', 0x80}
	now := time.Date(2026, 10, 23, 18, 0, 5, 123456789, time.UTC)

	saved, err := store.Save(req, body, CaptureUnparsed, "no plate found", "10.0.15.107", now)
	if err != nil {
		t.Fatal(err)
	}
	got, gotBody, err := store.Get(saved.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(gotBody, body) {
		t.Fatalf("body = %x, want the original bytes %x", gotBody, body)
	}
	if got.SourceIP != "10.0.15.107" || got.Method != "POST" || got.Path != "/odd/path" || got.Query != "device=7" ||
		got.Reason != CaptureUnparsed || got.ParseError != "no plate found" || got.BodySize != len(body) || len(got.BodySHA256) != 64 {
		t.Fatalf("capture = %+v", got)
	}
	if got.Headers["X-Camera-Serial"][0] != "SN-123" || got.Headers["Content-Type"][0] != "application/octet-stream" {
		t.Fatalf("headers lost: %+v", got.Headers)
	}
	for _, name := range []string{"Authorization", "Cookie"} {
		if _, kept := got.Headers[name]; kept {
			t.Fatalf("credential header %s was stored", name)
		}
	}
	for _, ext := range []string{".json", ".body"} {
		info, err := os.Stat(filepath.Join(store.dir, saved.ID+ext))
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("%s file mode = %v, %v; want 0600", ext, info, err)
		}
	}
}

func TestCaptureStoreListsNewestFirstAndHonoursTheLimit(t *testing.T) {
	store, _ := NewCaptureStore(t.TempDir(), 0, 0)
	base := time.Date(2026, 10, 23, 0, 0, 1, 0, time.UTC) // an early hour: the ID width must not depend on it
	var ids []string
	for i := 0; i < 4; i++ {
		c, err := store.Save(newCaptureRequest("/p"), []byte{byte(i)}, CaptureAll, "", "10.0.0.1", base.Add(time.Duration(i)*time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, c.ID)
	}
	list, err := store.List(3)
	if err != nil || len(list) != 3 {
		t.Fatalf("list = %d, %v; want 3", len(list), err)
	}
	for i, c := range list {
		if want := ids[len(ids)-1-i]; c.ID != want {
			t.Fatalf("list[%d] = %s, want %s (newest first)", i, c.ID, want)
		}
	}
	if all, _ := store.List(0); len(all) != 4 {
		t.Fatalf("list without a limit = %d, want 4", len(all))
	}
}

func TestCaptureStorePrunesTheOldestByCountAndBySize(t *testing.T) {
	now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	byCount, _ := NewCaptureStore(t.TempDir(), 3, 1<<20)
	var ids []string
	for i := 0; i < 5; i++ {
		c, _ := byCount.Save(newCaptureRequest("/p"), []byte("x"), CaptureAll, "", "10.0.0.1", now.Add(time.Duration(i)*time.Second))
		ids = append(ids, c.ID)
	}
	list, _ := byCount.List(0)
	if len(list) != 3 || list[0].ID != ids[4] || list[2].ID != ids[2] {
		t.Fatalf("after count pruning kept %v, want the 3 newest", list)
	}
	if _, err := os.Stat(filepath.Join(byCount.dir, ids[0]+".body")); !os.IsNotExist(err) {
		t.Fatalf("pruned capture left its body behind: %v", err)
	}

	bySize, _ := NewCaptureStore(t.TempDir(), 100, 3000)
	big := bytes.Repeat([]byte("a"), 1200)
	var last string
	for i := 0; i < 6; i++ {
		c, _ := bySize.Save(newCaptureRequest("/p"), big, CaptureAll, "", "10.0.0.1", now.Add(time.Duration(i)*time.Second))
		last = c.ID
	}
	list, _ = bySize.List(0)
	if len(list) == 0 || len(list) > 2 || list[0].ID != last {
		t.Fatalf("after size pruning kept %d captures (newest %v); the newest must stay and the total fit 3000 bytes", len(list), list)
	}
}

func TestCaptureStoreRefusesIDsThatCouldLeaveTheDirectory(t *testing.T) {
	root := t.TempDir()
	store, _ := NewCaptureStore(filepath.Join(root, "captures"), 0, 0)
	if err := os.WriteFile(filepath.Join(root, "secret.json"), []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"../secret", "..", "", "../secret.json", "20261023T180005123456789Z-aaaaaaaaaaaa/../../secret", "20261023T180005123456789Z-aaaaaaaaaaaa"} {
		if _, _, err := store.Get(id); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("Get(%q) = %v, want not exist", id, err)
		}
	}
}

func TestNewCaptureStoreRequiresADirectory(t *testing.T) {
	if _, err := NewCaptureStore("  ", 0, 0); err == nil {
		t.Fatal("an empty directory was accepted")
	}
}
