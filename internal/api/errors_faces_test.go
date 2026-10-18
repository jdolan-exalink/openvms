package api

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/jdolan-exalink/openvms/internal/faces"
)

func TestStatusForFaceErrors(t *testing.T) {
	status, code, msg := statusFor(fmt.Errorf("wrapped: %w", &faces.RemoteError{Reason: "the server could not be reached"}))
	if status != http.StatusBadGateway || code != "frigate_unreachable" || !strings.Contains(msg, "could not be reached") {
		t.Fatalf("remote error = %d %s %q", status, code, msg)
	}
	status, code, msg = statusFor(&faces.ValidationError{Msg: "bad picture"})
	if status != http.StatusBadRequest || code != "invalid" || msg != "bad picture" {
		t.Fatalf("validation error = %d %s %q", status, code, msg)
	}
	if status, _, _ := statusFor(errors.New("boom")); status != http.StatusInternalServerError {
		t.Fatalf("unknown error = %d", status)
	}
}
