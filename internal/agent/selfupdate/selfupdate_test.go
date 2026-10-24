package selfupdate

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var t0 = time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)

// elf builds the smallest thing that looks like a Linux executable for a machine, plus a payload.
func elf(machine uint16, payload string) []byte {
	h := make([]byte, 64)
	copy(h, "\x7fELF")
	h[4], h[5], h[6] = 2, 1, 1 // 64-bit, little endian, version 1
	binary.LittleEndian.PutUint16(h[18:], machine)
	return append(h, payload...)
}

const (
	emX86_64  = 62
	emAArch64 = 183
)

func sum(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

type fixture struct {
	dir, exe, state string
	u               *Updater
	calls           [][]string
	smoke           func(path string) (string, error)
	systemdRunErr   error
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	dir := t.TempDir()
	f := &fixture{dir: dir, exe: filepath.Join(dir, "openvms-agent"), state: filepath.Join(dir, "state")}
	if err := os.MkdirAll(f.state, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.exe, elf(emX86_64, "OLD"), 0o755); err != nil { //nolint:gosec // test files inside t.TempDir()
		t.Fatal(err)
	}
	f.smoke = func(string) (string, error) { return "0.4.1\n", nil }
	f.u = &Updater{
		ExePath: f.exe, StateDir: f.state, GOARCH: "amd64", Now: func() time.Time { return t0 },
		Run: func(_ context.Context, name string, args ...string) ([]byte, error) {
			f.calls = append(f.calls, append([]string{name}, args...))
			switch {
			case name == "systemd-run":
				return nil, f.systemdRunErr
			case strings.HasSuffix(name, ".new"):
				out, err := f.smoke(name)
				return []byte(out), err
			}
			return nil, nil
		},
	}
	return f
}

func (f *fixture) read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path) //nolint:gosec // test files inside t.TempDir()
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func (f *fixture) exists(path string) bool { _, err := os.Stat(path); return err == nil }

func TestApplyVerifiesStagesAndSwapsKeepingThePreviousBinary(t *testing.T) {
	f := newFixture(t)
	next := elf(emX86_64, "NEW")
	res, err := f.u.Apply(context.Background(), next, sum(next), "0.4.1")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal([]byte(f.read(t, f.exe)), next) {
		t.Fatal("the new binary is not installed")
	}
	if got := f.read(t, f.exe+".prev"); got != string(elf(emX86_64, "OLD")) {
		t.Fatal("the previous binary was not kept")
	}
	if info, _ := os.Stat(f.exe); info.Mode().Perm() != 0o755 {
		t.Fatalf("mode %v", info.Mode().Perm())
	}
	if f.exists(f.exe + ".new") {
		t.Fatal("a staging file was left behind")
	}
	if !res.WatchdogArmed || res.Version != "0.4.1" {
		t.Fatalf("result = %+v", res)
	}
	if !f.exists(filepath.Join(f.state, markerFile)) {
		t.Fatal("no pending marker")
	}
}

func TestApplyRefusesBadUpdatesAndTouchesNothing(t *testing.T) {
	good := elf(emX86_64, "NEW")
	for name, tc := range map[string]struct {
		data         []byte
		sha, version string
		smoke        func(string) (string, error)
		want         string
	}{
		"not an executable": {data: []byte("#!/bin/sh\nrm -rf /\n"), sha: "", want: "not an executable"},
		"too small":         {data: []byte("\x7fELF"), sha: "", want: "not an executable"},
		"wrong machine":     {data: elf(emAArch64, "NEW"), sha: "", want: "built for"},
		"checksum":          {data: good, sha: strings.Repeat("0", 64), want: "checksum"},
		"bad checksum text": {data: good, sha: "zz", want: "checksum"},
		"smoke fails":       {data: good, smoke: func(string) (string, error) { return "", errors.New("exec format error") }, want: "does not run"},
		"wrong version":     {data: good, version: "0.9.9", want: "reports version"},
		"empty version":     {data: good, smoke: func(string) (string, error) { return "\n", nil }, want: "does not run"},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			if tc.smoke != nil {
				f.smoke = tc.smoke
			}
			_, err := f.u.Apply(context.Background(), tc.data, tc.sha, tc.version)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want it to mention %q", err, tc.want)
			}
			if f.read(t, f.exe) != string(elf(emX86_64, "OLD")) {
				t.Fatal("the running binary was changed by a refused update")
			}
			for _, p := range []string{f.exe + ".new", f.exe + ".prev", filepath.Join(f.state, markerFile)} {
				if f.exists(p) {
					t.Fatalf("%s left behind", p)
				}
			}
		})
	}
}

func TestApplyRefusesAnUpdateOverTheSizeLimit(t *testing.T) {
	f := newFixture(t)
	f.u.MaxSize = 100
	if _, err := f.u.Apply(context.Background(), elf(emX86_64, strings.Repeat("x", 200)), "", ""); err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("err = %v", err)
	}
}

// If the new binary cannot even start, nothing in it can undo the update: a timer started now runs the
// previous, known-good binary later to put it back.
func TestApplyArmsAWatchdogRunByThePreviousBinary(t *testing.T) {
	f := newFixture(t)
	next := elf(emX86_64, "NEW")
	if _, err := f.u.Apply(context.Background(), next, "", ""); err != nil {
		t.Fatal(err)
	}
	var run []string
	for _, c := range f.calls {
		if c[0] == "systemd-run" {
			run = c
		}
	}
	joined := strings.Join(run, " ")
	if run == nil || !strings.Contains(joined, "--on-active=") || !strings.Contains(joined, f.exe+".prev rollback-apply") || !strings.Contains(joined, "--state-dir "+f.state) {
		t.Fatalf("watchdog command = %q", joined)
	}
}

func TestApplyWorksWithoutSystemdRunButSaysThereIsNoWatchdog(t *testing.T) {
	f := newFixture(t)
	f.systemdRunErr = errors.New("systemd-run: not found")
	res, err := f.u.Apply(context.Background(), elf(emX86_64, "NEW"), "", "")
	if err != nil {
		t.Fatal(err)
	}
	if res.WatchdogArmed {
		t.Fatal("no watchdog was armed")
	}
}

func TestConfirmClearsThePendingUpdateAndStopsTheWatchdog(t *testing.T) {
	f := newFixture(t)
	if _, err := f.u.Apply(context.Background(), elf(emX86_64, "NEW"), "", ""); err != nil {
		t.Fatal(err)
	}
	f.calls = nil
	if err := f.u.Confirm(context.Background()); err != nil {
		t.Fatal(err)
	}
	if f.exists(filepath.Join(f.state, markerFile)) {
		t.Fatal("the marker survived confirmation")
	}
	if !f.exists(f.exe + ".prev") {
		t.Fatal("the previous binary is the last known good one and stays")
	}
	stopped := false
	for _, c := range f.calls {
		stopped = stopped || (c[0] == "systemctl" && c[1] == "stop" && strings.HasSuffix(c[2], ".timer"))
	}
	if !stopped {
		t.Fatalf("the watchdog timer was not stopped: %v", f.calls)
	}
	if err := f.u.Confirm(context.Background()); err != nil {
		t.Fatalf("confirming with nothing pending is not an error: %v", err)
	}
}

func TestRollbackPutsThePreviousBinaryBackOnceAndOnlyWhenPending(t *testing.T) {
	f := newFixture(t)
	if rolled, err := f.u.RollbackIfPending(context.Background()); err != nil || rolled {
		t.Fatalf("nothing pending: rolled=%v err=%v", rolled, err)
	}
	if _, err := f.u.Apply(context.Background(), elf(emX86_64, "NEW"), "", ""); err != nil {
		t.Fatal(err)
	}
	rolled, err := f.u.RollbackIfPending(context.Background())
	if err != nil || !rolled {
		t.Fatalf("rolled=%v err=%v", rolled, err)
	}
	if f.read(t, f.exe) != string(elf(emX86_64, "OLD")) {
		t.Fatal("the previous binary is not back")
	}
	if f.exists(filepath.Join(f.state, markerFile)) {
		t.Fatal("the marker survived the rollback")
	}
	if rolled, _ := f.u.RollbackIfPending(context.Background()); rolled {
		t.Fatal("a second rollback must do nothing")
	}
}

func TestACrashLoopRollsBackByItself(t *testing.T) {
	f := newFixture(t)
	f.u.MaxStartAttempts = 3
	if _, err := f.u.Apply(context.Background(), elf(emX86_64, "NEW"), "", ""); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 3; i++ {
		rolled, err := f.u.NoteStart(context.Background())
		if err != nil || rolled {
			t.Fatalf("start %d: rolled=%v err=%v", i, rolled, err)
		}
	}
	rolled, err := f.u.NoteStart(context.Background())
	if err != nil || !rolled {
		t.Fatalf("start 4 should roll back: rolled=%v err=%v", rolled, err)
	}
	if f.read(t, f.exe) != string(elf(emX86_64, "OLD")) {
		t.Fatal("the previous binary is not back")
	}
}

func TestNoteStartWithNothingPendingDoesNothing(t *testing.T) {
	f := newFixture(t)
	if rolled, err := f.u.NoteStart(context.Background()); err != nil || rolled {
		t.Fatalf("rolled=%v err=%v", rolled, err)
	}
}

func TestAnUpdateCannotOverwriteThePreviousBinaryWhileAnotherIsPending(t *testing.T) {
	f := newFixture(t)
	if _, err := f.u.Apply(context.Background(), elf(emX86_64, "NEW"), "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := f.u.Apply(context.Background(), elf(emX86_64, "NEWER"), "", ""); !errors.Is(err, ErrUpdatePending) {
		t.Fatalf("err = %v, want ErrUpdatePending", err)
	}
	if f.read(t, f.exe+".prev") != string(elf(emX86_64, "OLD")) {
		t.Fatal("the known-good binary was overwritten by an unconfirmed one")
	}
	// A pending update whose deadline and the watchdog's slack are long past is stale: it is rolled
	// back first and the new one goes ahead.
	f.u.Now = func() time.Time { return t0.Add(time.Hour) }
	if _, err := f.u.Apply(context.Background(), elf(emX86_64, "NEWER"), "", ""); err != nil {
		t.Fatalf("a stale pending update must not block for ever: %v", err)
	}
}
