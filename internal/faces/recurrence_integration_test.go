//go:build integration

package faces_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jdolan-exalink/openvms/internal/access"
	"github.com/jdolan-exalink/openvms/internal/faces"
)

func TestRecurrenceRuleIsSavedAuditedAndUsedAsTheDefault(t *testing.T) {
	sf := newSyncFixture(t)
	ctx := context.Background()
	admin := sf.env.Admin

	got, err := sf.svc.SavedClusterRule(ctx)
	if err != nil || got != faces.DefaultClusterRule {
		t.Fatalf("default rule = %+v, %v; want %+v", got, err, faces.DefaultClusterRule)
	}
	saved, err := sf.svc.UpdateRecurrence(ctx, admin, faces.ClusterRule{MinAppearances: 5, WindowDays: 14})
	if err != nil {
		t.Fatal(err)
	}
	if saved.Rule.MinAppearances != 5 || saved.Rule.WindowDays != 14 || saved.UpdatedBy == nil || *saved.UpdatedBy != admin.UserID {
		t.Fatalf("saved = %+v", saved)
	}
	// Anyone reading the clusters gets the saved rule, with no permission beyond their own.
	if got, err = sf.svc.SavedClusterRule(ctx); err != nil || got.MinAppearances != 5 || got.WindowDays != 14 {
		t.Fatalf("saved rule read back = %+v, %v", got, err)
	}
	if n := sf.scanInt(`SELECT count(*) FROM audit_log WHERE action = $1 AND details->'before'->>'min_appearances' = '3' AND details->'after'->>'min_appearances' = '5'`, faces.ActionRecurrenceUpdated); n != 1 {
		t.Fatalf("audit entries with the old and new value = %d, want 1", n)
	}
}

func TestRecurrenceRuleRejectsInvalidValuesAndNonPlatformManagers(t *testing.T) {
	sf := newSyncFixture(t)
	ctx := context.Background()

	var v *faces.ValidationError
	if _, err := sf.svc.UpdateRecurrence(ctx, sf.env.Admin, faces.ClusterRule{MinAppearances: 0, WindowDays: 7}); !errors.As(err, &v) {
		t.Fatalf("invalid rule err = %v, want a ValidationError", err)
	}
	// A tenant-level faces manager cannot change a platform-wide rule, nor read it through the settings API.
	if _, err := sf.svc.UpdateRecurrence(ctx, sf.manager, faces.ClusterRule{MinAppearances: 9, WindowDays: 9}); !errors.Is(err, access.ErrForbidden) {
		t.Fatalf("manager update err = %v, want forbidden", err)
	}
	if _, err := sf.svc.GetRecurrence(ctx, sf.manager); !errors.Is(err, access.ErrForbidden) {
		t.Fatalf("manager get err = %v, want forbidden", err)
	}
	if got, err := sf.svc.SavedClusterRule(ctx); err != nil || got != faces.DefaultClusterRule {
		t.Fatalf("rule after refused changes = %+v, %v; want the default", got, err)
	}
	if n := sf.scanInt(`SELECT count(*) FROM audit_log WHERE action = $1`, faces.ActionRecurrenceUpdated); n != 0 {
		t.Fatalf("audit entries = %d, want 0 after refused changes", n)
	}
}
