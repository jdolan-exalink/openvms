package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/jdolan-exalink/openvms/internal/agent/selfupdate"
)

const (
	updateChecksumHeader = "X-OpenVMS-SHA256"
	updateVersionHeader  = "X-OpenVMS-Agent-Version"
	updateMaxBytes       = 80 << 20
)

// newUpdateReplacer returns the function behind POST /v1/update: it hands the uploaded binary to the
// updater, which verifies it, keeps the previous one and arms the rollback. OpenVMS sends the
// checksum and the version it expects as headers; an uploader that sends none gets the same checks
// that need no input (executable for this machine, runs, reports a version).
func newUpdateReplacer(u *selfupdate.Updater) func(*http.Request) error {
	return func(r *http.Request) error {
		data, err := io.ReadAll(io.LimitReader(r.Body, updateMaxBytes+1))
		if err != nil {
			return err
		}
		if len(data) > updateMaxBytes {
			return fmt.Errorf("update payload is too large (limit %d bytes)", updateMaxBytes)
		}
		res, err := u.Apply(r.Context(), data, strings.TrimSpace(r.Header.Get(updateChecksumHeader)), strings.TrimSpace(r.Header.Get(updateVersionHeader)))
		if err != nil {
			return err
		}
		slog.Info("agent update staged", "version", res.Version, "confirm_by", res.Deadline, "rollback_watchdog", res.WatchdogArmed)
		return nil
	}
}

// updateStatus is the HTTP status for a refused update: 409 while an earlier one waits to be
// confirmed, 400 for anything wrong with the payload.
func updateStatus(err error) int {
	if errors.Is(err, selfupdate.ErrUpdatePending) {
		return http.StatusConflict
	}
	return http.StatusBadRequest
}

// runRollbackApply is `openvms-agent rollback-apply`, started by the timer an update arms and run from
// the previous binary: if the update was never confirmed it restores that binary and restarts the
// service. With nothing pending it does nothing.
func runRollbackApply(args []string, stdout, stderr io.Writer) int {
	return rollbackApply(args, stdout, stderr, restartService)
}

// restartService restarts the agent unit through systemd.
func restartService(ctx context.Context) error {
	if out, err := exec.CommandContext(ctx, "systemctl", "restart", "openvms-agent").CombinedOutput(); err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func rollbackApply(args []string, stdout, stderr io.Writer, restartUnit func(context.Context) error) int {
	var stateDir, exe string
	for i := 0; i+1 < len(args); i += 2 {
		switch args[i] {
		case "--state-dir":
			stateDir = args[i+1]
		case "--exe":
			exe = args[i+1]
		}
	}
	if stateDir == "" || exe == "" {
		fmt.Fprintln(stderr, "usage: openvms-agent rollback-apply --state-dir DIR --exe PATH")
		return 2
	}
	u := &selfupdate.Updater{ExePath: exe, StateDir: stateDir}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	rolled, err := u.RollbackIfPending(ctx)
	if err != nil {
		fmt.Fprintln(stderr, "rollback:", err)
		return 1
	}
	if !rolled {
		fmt.Fprintln(stdout, "no pending update; nothing to roll back")
		return 0
	}
	fmt.Fprintln(stdout, "the update was not confirmed; the previous agent binary is back")
	if err := restartUnit(ctx); err != nil {
		fmt.Fprintln(stderr, "restart:", err)
		return 1
	}
	return 0
}

// confirmUpdateWhenHealthy marks a pending update as good once this process answers its own API. It
// is the new binary proving it works: until then the rollback net stays armed.
func confirmUpdateWhenHealthy(u *selfupdate.Updater, listen, secret string, log *slog.Logger) {
	port := listen
	if i := strings.LastIndex(listen, ":"); i >= 0 {
		port = listen[i+1:]
	}
	if _, err := strconv.Atoi(port); err != nil {
		log.Warn("update confirmation skipped: cannot tell the agent port", "listen", listen)
		return
	}
	client := &http.Client{Timeout: 3 * time.Second}
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://127.0.0.1:"+port+"/v1/metrics", nil)
		if err == nil {
			req.Header.Set("Authorization", "Bearer "+secret)
			if resp, err := client.Do(req); err == nil {
				_ = resp.Body.Close()
				if resp.StatusCode == http.StatusOK {
					if err := u.Confirm(context.Background()); err != nil {
						log.Error("agent update could not be confirmed", "error", err)
					} else {
						log.Info("agent update confirmed")
					}
					return
				}
			}
		}
		time.Sleep(time.Second)
	}
	log.Error("agent update not confirmed: this process did not answer its own API in time; the rollback net will act")
}

// newUpdater builds the updater for the running binary.
func newUpdater(stateDir string) (*selfupdate.Updater, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	return &selfupdate.Updater{ExePath: exe, StateDir: stateDir}, nil
}
