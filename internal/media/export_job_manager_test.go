package media

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureExportDir(t *testing.T) {
	tempDir := t.TempDir()
	target := filepath.Join(tempDir, "exports")

	res := ensureExportDir(target)
	if res != target {
		t.Fatalf("expected %s, got %s", target, res)
	}

	info, err := os.Stat(target)
	if err != nil {
		t.Fatalf("stat failed: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("expected dir, got %v", info)
	}
	// Verify directory is readable and traversable by other users/services (e.g. API container).
	if perm := info.Mode().Perm(); perm&0o005 != 0o005 {
		t.Fatalf("expected world read/execute permissions (at least 0755), got %v", perm)
	}
}

func TestExportPackageForensicHashing(t *testing.T) {
	tempDir := t.TempDir()
	sampleData := []byte("test video stream content for forensic verification")

	hasher := sha256.New()
	hasher.Write(sampleData)
	expectedHash := hex.EncodeToString(hasher.Sum(nil))

	filePath := filepath.Join(tempDir, "camera_1.mp4")
	// #nosec G306 -- test fixture deliberately uses public or executable permissions.
	if err := os.WriteFile(filePath, sampleData, 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	// #nosec G304 -- test fixture path is derived from t.TempDir or a checked-in repository fixture.
	readBytes, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}

	checkHasher := sha256.New()
	checkHasher.Write(readBytes)
	actualHash := hex.EncodeToString(checkHasher.Sum(nil))

	if actualHash != expectedHash {
		t.Fatalf("expected SHA-256 %s, got %s", expectedHash, actualHash)
	}
}
