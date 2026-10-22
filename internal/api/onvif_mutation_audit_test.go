package api

import (
	"errors"
	"reflect"
	"testing"

	"github.com/google/uuid"
)

func TestExecuteAuditedOnvifMutation(t *testing.T) {
	t.Run("intent precedes execution and terminal event shares operation ID", func(t *testing.T) {
		var ids []uuid.UUID
		var events []string
		err := executeAuditedOnvifMutation("ptz", func() error { return nil }, func(id uuid.UUID, operation, status, category string) error {
			ids = append(ids, id)
			if operation != "ptz" {
				t.Errorf("event operation = %q, want ptz", operation)
			}
			events = append(events, status+":"+category)
			return nil
		}, func() error {
			if !reflect.DeepEqual(events, []string{"started:"}) {
				t.Fatalf("events before remote call = %v, want intent only", events)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(ids) != 2 || ids[0] == uuid.Nil || ids[0] != ids[1] {
			t.Fatalf("operation IDs = %v, want one shared nonzero ID", ids)
		}
		if !reflect.DeepEqual(events, []string{"started:", "succeeded:"}) {
			t.Fatalf("events = %v, want start then success", events)
		}
	})

	t.Run("authorization denial prevents intent audit and remote call", func(t *testing.T) {
		denied := errors.New("permission denied")
		auditCalled, operationCalled := false, false
		err := executeAuditedOnvifMutation("imaging", func() error { return denied }, func(_ uuid.UUID, _, _, _ string) error {
			auditCalled = true
			return nil
		}, func() error {
			operationCalled = true
			return nil
		})
		if !errors.Is(err, denied) {
			t.Fatalf("error = %v, want permission denial", err)
		}
		if auditCalled || operationCalled {
			t.Fatalf("audit/remote called after authorization denial: %v/%v", auditCalled, operationCalled)
		}
	})

	t.Run("intent audit denial prevents remote call", func(t *testing.T) {
		denied := errors.New("audit store unavailable")
		called := false
		err := executeAuditedOnvifMutation("imaging", func() error { return nil }, func(_ uuid.UUID, _, status, _ string) error {
			if status != "started" {
				t.Fatalf("status = %q, want started", status)
			}
			return denied
		}, func() error {
			called = true
			return nil
		})
		if !errors.Is(err, denied) || called {
			t.Fatalf("error=%v remote-called=%v, want intent failure and no remote call", err, called)
		}
	})

	t.Run("remote error is recorded as unknown without leaking details", func(t *testing.T) {
		var statuses, categories []string
		remoteErr := errors.New("camera at http://private.local failed with password=hunter2")
		err := executeAuditedOnvifMutation("video_config", func() error { return nil }, func(_ uuid.UUID, _, status, category string) error {
			statuses = append(statuses, status)
			categories = append(categories, category)
			return nil
		}, func() error { return remoteErr })
		if !errors.Is(err, errOnvifMutationOutcomeUnknown) {
			t.Fatalf("error = %v, want unknown outcome", err)
		}
		if reflect.DeepEqual(statuses, []string{"started", "outcome_unknown"}) == false || reflect.DeepEqual(categories, []string{"", "remote_outcome_unknown"}) == false {
			t.Fatalf("audit statuses/categories = %v/%v", statuses, categories)
		}
		if err.Error() == remoteErr.Error() {
			t.Fatal("API error exposed the raw remote error")
		}
	})

	t.Run("missing terminal audit leaves outcome unknown", func(t *testing.T) {
		writes := 0
		err := executeAuditedOnvifMutation("sync_time", func() error { return nil }, func(_ uuid.UUID, _, _, _ string) error {
			writes++
			if writes == 2 {
				return errors.New("database unavailable")
			}
			return nil
		}, func() error { return nil })
		if !errors.Is(err, errOnvifMutationOutcomeUnknown) {
			t.Fatalf("error = %v, want unknown outcome", err)
		}
	})
}
