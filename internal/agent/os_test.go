package agent

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectOSFromReleaseFile(t *testing.T) {
	tempDir := t.TempDir()

	// 1. With PRETTY_NAME (Debian 13)
	debianRelease := `PRETTY_NAME="Debian GNU/Linux 13 (trixie)"
NAME="Debian GNU/Linux"
VERSION_ID="13"
VERSION="13 (trixie)"
ID=debian
`
	if err := os.WriteFile(filepath.Join(tempDir, "os-release"), []byte(debianRelease), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := detectOS(tempDir); got != "Debian GNU/Linux 13 (trixie)" {
		t.Errorf("expected Debian GNU/Linux 13 (trixie), got %q", got)
	}

	// 2. With Ubuntu 24.04
	ubuntuRelease := `PRETTY_NAME="Ubuntu 24.04.1 LTS"
NAME="Ubuntu"
VERSION_ID="24.04"
VERSION="24.04.1 LTS (Noble Numbat)"
ID=ubuntu
`
	if err := os.WriteFile(filepath.Join(tempDir, "os-release"), []byte(ubuntuRelease), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := detectOS(tempDir); got != "Ubuntu 24.04.1 LTS" {
		t.Errorf("expected Ubuntu 24.04.1 LTS, got %q", got)
	}

	// 3. Fallback to NAME + VERSION_ID if PRETTY_NAME is missing
	noPrettyRelease := `NAME="Debian GNU/Linux"
VERSION_ID="13"
`
	if err := os.WriteFile(filepath.Join(tempDir, "os-release"), []byte(noPrettyRelease), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := detectOS(tempDir); got != "Debian GNU/Linux 13" {
		t.Errorf("expected Debian GNU/Linux 13, got %q", got)
	}

	// 4. Default when file does not exist
	emptyDir := t.TempDir()
	if got := detectOS(emptyDir); got != "Debian 13 / Linux" {
		t.Errorf("expected default Debian 13 / Linux, got %q", got)
	}
}
