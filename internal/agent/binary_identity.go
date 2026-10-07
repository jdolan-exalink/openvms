package agent

import (
	"bytes"
	"crypto/sha256"
	"debug/buildinfo"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"runtime"
	"strings"
	"syscall"

	platformbuildinfo "github.com/jdolan-exalink/openvms/internal/platform/buildinfo"
)

// MaxBinarySize bounds the trusted agent artifact and the running executable
// read used to identify it.
const MaxBinarySize int64 = 128 << 20

// MaxHealthResponseBytes is the authenticated TLS health JSON body limit.
const MaxHealthResponseBytes = 1024

// BinaryIdentity describes one exact Go executable. Version and Commit are
// empty when the executable does not contain suitable release metadata; the
// digest and architecture remain independently useful.
type BinaryIdentity struct {
	SHA256       string `json:"sha256,omitempty"`
	Architecture string `json:"architecture,omitempty"`
	Version      string `json:"version,omitempty"`
	Commit       string `json:"commit,omitempty"`
}

// IdentityFromBinary derives identity from the supplied bytes, never from
// control-plane linker labels. Development and missing build metadata remain
// unknown rather than being presented as a release.
func IdentityFromBinary(binary []byte) (BinaryIdentity, error) {
	if len(binary) == 0 || int64(len(binary)) > MaxBinarySize {
		return BinaryIdentity{}, errors.New("agent binary size is outside the allowed bound")
	}
	digest := sha256.Sum256(binary)
	identity := BinaryIdentity{SHA256: hex.EncodeToString(digest[:]), Architecture: runtime.GOARCH}
	info, err := buildinfo.Read(bytes.NewReader(binary))
	if err != nil {
		return identity, nil
	}
	if info.Main.Version != "(devel)" && info.Main.Version != "" {
		identity.Version = info.Main.Version
	}
	for _, setting := range info.Settings {
		switch setting.Key {
		case "GOARCH":
			if setting.Value != "" {
				identity.Architecture = setting.Value
			}
		case "vcs.revision":
			identity.Commit = setting.Value
		}
	}
	return identity, nil
}

// RunningBinaryIdentity reads and identifies the current executable once.
// O_NOFOLLOW and fstat ensure the path resolves to a bounded regular file.
func RunningBinaryIdentity() (BinaryIdentity, error) {
	path, err := os.Executable()
	if err != nil {
		return BinaryIdentity{}, errors.New("cannot resolve running agent executable")
	}
	identity, err := identityFromExecutablePath(path)
	if err != nil {
		return BinaryIdentity{}, err
	}
	if ValidReleaseVersion(platformbuildinfo.Version) && platformbuildinfo.Version != "dev" && platformbuildinfo.Version != "unknown" {
		identity.Version = platformbuildinfo.Version
	}
	return identity, nil
}

// ValidReleaseVersion accepts a bounded, single-line build label suitable for
// an artifact manifest and UI display. Development placeholders are not releases.
func ValidReleaseVersion(version string) bool {
	if len(version) == 0 || len(version) > 64 || version == "dev" || version == "unknown" {
		return false
	}
	for i, r := range version {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || (i > 0 && (r == '.' || r == '+' || r == '-' || r == '_')) {
			continue
		}
		return false
	}
	return true
}

func identityFromExecutablePath(path string) (BinaryIdentity, error) {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return BinaryIdentity{}, errors.New("cannot open running agent executable")
	}
	file := os.NewFile(uintptr(fd), "running-agent")
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > MaxBinarySize {
		return BinaryIdentity{}, errors.New("running agent executable is not a bounded regular file")
	}
	binary, err := io.ReadAll(io.LimitReader(file, MaxBinarySize+1))
	if err != nil || int64(len(binary)) > MaxBinarySize {
		return BinaryIdentity{}, errors.New("running agent executable exceeded size bound")
	}
	return IdentityFromBinary(binary)
}

// ReleaseMetadataAvailable reports whether a release label is meaningful.
func (identity BinaryIdentity) ReleaseMetadataAvailable() bool {
	return strings.TrimSpace(identity.Version) != "" || strings.TrimSpace(identity.Commit) != ""
}
