package main

import (
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
)

// captureMode says what the agent keeps of the requests Provision-ISR cameras send it.
type captureMode string

const (
	// captureUnparsed keeps only the requests the parser cannot read (the default), so an
	// unknown firmware is recorded instead of silently dropped.
	captureUnparsed captureMode = "unparsed"
	// captureOff keeps nothing.
	captureOff captureMode = "off"
	// captureEverything keeps every event request and also accepts POSTs on any path, which is how
	// the real protocol of a camera is learned before its paths are known.
	captureEverything captureMode = "all"
)

// parseCaptureMode reads OPENVMS_AGENT_PROVISION_CAPTURE. An unknown value is an error rather than
// a silent default, so a typo cannot leave the agent in a mode nobody chose.
func parseCaptureMode(value string) (captureMode, error) {
	switch v := captureMode(strings.ToLower(strings.TrimSpace(value))); v {
	case "":
		return captureUnparsed, nil
	case captureUnparsed, captureOff, captureEverything:
		return v, nil
	default:
		return "", fmt.Errorf("OPENVMS_AGENT_PROVISION_CAPTURE must be unparsed, off or all, got %q", value)
	}
}

// captureDir is where captures live inside the agent state directory.
func captureDir(stateDir string) string { return filepath.Join(stateDir, "provision-captures") }

// postsAnywhere sends POSTs on paths nothing else claims to next and answers 404 to the rest.
func postsAnywhere(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}
