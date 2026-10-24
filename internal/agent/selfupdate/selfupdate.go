// Package selfupdate replaces the running agent binary without trusting the new one blindly. An
// update is checked before it is installed (it is an executable for this machine, it matches the
// checksum OpenVMS computed, it runs and reports the expected version), the previous binary is kept,
// and the update only stays if the new process proves it is healthy: otherwise the previous binary is
// put back, either by the new process (it started and kept failing) or by a timer that runs the
// previous binary (it could not start at all).
package selfupdate

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"crypto/sha256"
)

const (
	markerFile = "update-pending.json"

	defaultMaxSize       = 80 << 20
	defaultConfirmWindow = 3 * time.Minute
	defaultMaxStarts     = 3
	// watchdogSlack is how long after the deadline a pending update is still left to the watchdog.
	watchdogSlack = 2 * time.Minute
	smokeTimeout  = 10 * time.Second
)

// ErrUpdatePending refuses a new update while the previous one has not been confirmed: its
// predecessor is the last known-good binary and must not be overwritten.
var ErrUpdatePending = errors.New("an earlier agent update is still waiting to be confirmed")

var sha256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// Runner runs a command and returns its output; tests replace it.
type Runner func(ctx context.Context, name string, args ...string) ([]byte, error)

// Updater applies, confirms and rolls back updates of one agent binary.
type Updater struct {
	ExePath  string
	StateDir string

	GOARCH           string
	Now              func() time.Time
	Run              Runner
	MaxSize          int64
	MaxStartAttempts int
	ConfirmWindow    time.Duration
}

// Result describes an applied update.
type Result struct {
	Version       string
	Deadline      time.Time
	WatchdogArmed bool
}

type marker struct {
	Previous     string    `json:"previous"`
	NewSHA256    string    `json:"new_sha256"`
	Version      string    `json:"version"`
	Deadline     time.Time `json:"deadline"`
	Attempts     int       `json:"attempts"`
	WatchdogUnit string    `json:"watchdog_unit"`
}

func (u *Updater) now() time.Time {
	if u.Now != nil {
		return u.Now()
	}
	return time.Now()
}

func (u *Updater) run(ctx context.Context, name string, args ...string) ([]byte, error) {
	if u.Run != nil {
		return u.Run(ctx, name, args...)
	}
	return exec.CommandContext(ctx, name, args...).Output() //nolint:gosec // fixed commands: the staged binary, systemd-run and systemctl
}

func (u *Updater) markerPath() string { return filepath.Join(u.StateDir, markerFile) }

// Apply verifies data, installs it as the agent binary and arms the safety net. It does not restart
// the agent; the caller does, after answering the request.
func (u *Updater) Apply(ctx context.Context, data []byte, wantSHA256, wantVersion string) (Result, error) {
	if err := u.check(data, wantSHA256); err != nil {
		return Result{}, err
	}
	if m, err := u.readMarker(); err != nil {
		return Result{}, err
	} else if m != nil {
		if u.now().Before(m.Deadline.Add(watchdogSlack)) {
			return Result{}, ErrUpdatePending
		}
		if _, err := u.RollbackIfPending(ctx); err != nil {
			return Result{}, fmt.Errorf("an earlier update never confirmed and could not be rolled back: %w", err)
		}
	}

	next := u.ExePath + ".new"
	prev := u.ExePath + ".prev"
	cleanup := func() { _ = os.Remove(next) }
	if err := writeFileAtomic(next, data, 0o755); err != nil {
		return Result{}, fmt.Errorf("stage the update: %w", err)
	}
	version, err := u.smoke(ctx, next, wantVersion)
	if err != nil {
		cleanup()
		return Result{}, err
	}
	current, err := os.ReadFile(u.ExePath) //nolint:gosec // the agent's own binary
	if err != nil {
		cleanup()
		return Result{}, fmt.Errorf("keep the previous binary: %w", err)
	}
	if err := writeFileAtomic(prev, current, 0o755); err != nil {
		cleanup()
		return Result{}, fmt.Errorf("keep the previous binary: %w", err)
	}

	window := u.ConfirmWindow
	if window <= 0 {
		window = defaultConfirmWindow
	}
	sum := sha256.Sum256(data)
	m := marker{Previous: prev, NewSHA256: hex.EncodeToString(sum[:]), Version: version, Deadline: u.now().Add(window),
		WatchdogUnit: fmt.Sprintf("openvms-agent-rollback-%d", u.now().Unix())}
	if err := u.writeMarker(m); err != nil {
		cleanup()
		_ = os.Remove(prev)
		return Result{}, fmt.Errorf("record the pending update: %w", err)
	}
	armed := u.armWatchdog(ctx, m, window)
	if err := os.Rename(next, u.ExePath); err != nil {
		cleanup()
		_ = os.Remove(u.markerPath())
		u.stopWatchdog(ctx, m)
		return Result{}, fmt.Errorf("install the update: %w", err)
	}
	return Result{Version: version, Deadline: m.Deadline, WatchdogArmed: armed}, nil
}

// check validates the payload before anything is written.
func (u *Updater) check(data []byte, wantSHA256 string) error {
	limit := u.MaxSize
	if limit <= 0 {
		limit = defaultMaxSize
	}
	if int64(len(data)) > limit {
		return fmt.Errorf("update payload is too large (limit %d bytes)", limit)
	}
	if len(data) < 20 || string(data[:4]) != "\x7fELF" || data[4] != 2 || data[5] != 1 {
		return errors.New("update payload is not an executable for this platform")
	}
	arch := u.GOARCH
	if arch == "" {
		arch = runtime.GOARCH
	}
	machines := map[string]uint16{"amd64": 62, "arm64": 183}
	want, known := machines[arch]
	if got := binary.LittleEndian.Uint16(data[18:20]); !known || got != want {
		return fmt.Errorf("update payload is built for another machine type than this host (%s)", arch)
	}
	if wantSHA256 != "" {
		if !sha256Pattern.MatchString(wantSHA256) {
			return errors.New("the checksum is not a SHA-256 in hex")
		}
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != wantSHA256 {
			return errors.New("update payload does not match its checksum")
		}
	}
	return nil
}

// smoke runs the staged binary with --version: it proves the file executes on this host (right libc,
// right architecture, not truncated) before the running binary is touched.
func (u *Updater) smoke(ctx context.Context, path, wantVersion string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, smokeTimeout)
	defer cancel()
	out, err := u.run(ctx, path, "--version")
	version := strings.TrimSpace(string(out))
	if err != nil || version == "" {
		return "", errors.New("the new binary does not run on this host")
	}
	if wantVersion != "" && version != wantVersion {
		return "", fmt.Errorf("the new binary reports version %q, OpenVMS sent %q", version, wantVersion)
	}
	return version, nil
}

// armWatchdog schedules a run of the previous binary that puts it back if the update is still
// unconfirmed when the window closes. It is best effort: without systemd-run the in-process crash
// loop check is the only net, and the result says so.
func (u *Updater) armWatchdog(ctx context.Context, m marker, window time.Duration) bool {
	_, err := u.run(ctx, "systemd-run", "--unit="+m.WatchdogUnit, fmt.Sprintf("--on-active=%ds", int(window.Seconds())),
		"--timer-property=AccuracySec=1s", m.Previous, "rollback-apply", "--state-dir", u.StateDir, "--exe", u.ExePath)
	return err == nil
}

func (u *Updater) stopWatchdog(ctx context.Context, m marker) {
	if m.WatchdogUnit != "" {
		_, _ = u.run(ctx, "systemctl", "stop", m.WatchdogUnit+".timer")
	}
}

// Confirm is called by the new process once it is healthy: the update is final.
func (u *Updater) Confirm(ctx context.Context) error {
	m, err := u.readMarker()
	if err != nil || m == nil {
		return err
	}
	if err := os.Remove(u.markerPath()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	u.stopWatchdog(ctx, *m)
	return nil
}

// NoteStart counts a start of the agent while an update is unconfirmed; once it has failed to
// confirm too many times in a row the previous binary is put back. It reports whether it did, so the
// caller can restart.
func (u *Updater) NoteStart(ctx context.Context) (bool, error) {
	m, err := u.readMarker()
	if err != nil || m == nil {
		return false, err
	}
	limit := u.MaxStartAttempts
	if limit <= 0 {
		limit = defaultMaxStarts
	}
	m.Attempts++
	if m.Attempts > limit {
		return u.RollbackIfPending(ctx)
	}
	return false, u.writeMarker(*m)
}

// RollbackIfPending restores the previous binary when an update is waiting for confirmation. It is
// what the watchdog runs (from the previous binary) and what a crash loop triggers.
func (u *Updater) RollbackIfPending(ctx context.Context) (bool, error) {
	m, err := u.readMarker()
	if err != nil || m == nil {
		return false, err
	}
	prev, err := os.ReadFile(m.Previous) //nolint:gosec // the agent's own previous binary
	if err != nil {
		return false, fmt.Errorf("the previous binary is not available: %w", err)
	}
	if err := writeFileAtomic(u.ExePath, prev, 0o755); err != nil {
		return false, fmt.Errorf("restore the previous binary: %w", err)
	}
	if err := os.Remove(u.markerPath()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return true, err
	}
	u.stopWatchdog(ctx, *m)
	return true, nil
}

func (u *Updater) readMarker() (*marker, error) {
	data, err := os.ReadFile(u.markerPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var m marker
	if err := json.Unmarshal(data, &m); err != nil || m.Previous == "" {
		return nil, errors.New("the pending update record is unreadable")
	}
	return &m, nil
}

func (u *Updater) writeMarker(m marker) error {
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return writeFileAtomic(u.markerPath(), data, 0o600)
}

// writeFileAtomic writes next to the target and renames, so a reader never sees half a file.
func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, mode); err != nil { //nolint:gosec // the agent's own files
		return err
	}
	if err := os.Chmod(tmp, mode); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
