package media

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestSealFileHashesAndRenamesACapture(t *testing.T) {
	dir := t.TempDir()
	part, final := filepath.Join(dir, "a.mp4.part"), filepath.Join(dir, "a.mp4")
	data := []byte("video bytes")
	if err := os.WriteFile(part, data, 0o600); err != nil {
		t.Fatal(err)
	}
	hash, size, err := sealFile(part, final)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	if hash != hex.EncodeToString(sum[:]) || size != int64(len(data)) {
		t.Fatalf("hash/size = %s/%d", hash, size)
	}
	if _, err := os.Stat(part); !os.IsNotExist(err) {
		t.Fatal("the .part file must be gone")
	}
	if b, err := os.ReadFile(final); err != nil || string(b) != string(data) { // #nosec G304 -- temp dir
		t.Fatalf("final = %q, %v", b, err)
	}
}

func TestSealFileTreatsAnEmptyCaptureAsNoRecording(t *testing.T) {
	dir := t.TempDir()
	part := filepath.Join(dir, "a.mp4.part")
	if err := os.WriteFile(part, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := sealFile(part, filepath.Join(dir, "a.mp4")); !errors.Is(err, ErrNoRecording) {
		t.Fatalf("error = %v, want ErrNoRecording", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "a.mp4")); !os.IsNotExist(err) {
		t.Fatal("an empty capture must not become a final file")
	}
}
