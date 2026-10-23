//go:build integration

package faces_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/access"
	"github.com/jdolan-exalink/openvms/internal/faces"
	"github.com/jdolan-exalink/openvms/internal/inventory"
	"github.com/jdolan-exalink/openvms/internal/store"
)

func suggestionTargets(t *testing.T, sf *syncFixture, id uuid.UUID) []uuid.UUID {
	t.Helper()
	page, err := sf.svc.ListReviewSightings(context.Background(), sf.manager, faces.TrainFilter{TenantID: &sf.tenant})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range page.Items {
		if item.ID == id {
			var out []uuid.UUID
			for _, s := range item.Suggestions {
				out = append(out, s.IdentityID)
			}
			return out
		}
	}
	t.Fatalf("sighting %s is not in the inbox", id)
	return nil
}

func TestSuggestionDecisionsHideDifferentPairsAndKeepTheLabel(t *testing.T) {
	sf := newSyncFixture(t)
	sf.setCapability(sf.h01ID, sf.h01, true)
	sf.svc.Adapters = inventory.NewAdapters(sf.env.Svc)
	seedFaceSightings(t, sf, 81, 82, 83)
	ctx := context.Background()
	emb := &faces.Embedder{Store: sf.env.Store, Blobs: sf.blobs, Runtime: &fakeEmbedder{profile: faces.EmbeddingProfileCPU}, ModelID: "m1", Log: sf.svc.Log}
	if n, err := emb.RunOnce(ctx, 10); err != nil || n != 3 {
		t.Fatalf("embedded %d, %v", n, err)
	}
	page, err := sf.svc.ListReviewSightings(ctx, sf.manager, faces.TrainFilter{TenantID: &sf.tenant})
	if err != nil || len(page.Items) != 3 {
		t.Fatalf("inbox sightings = %d, %v", len(page.Items), err)
	}
	ids := []uuid.UUID{page.Items[0].ID, page.Items[1].ID, page.Items[2].ID}
	a, b, c := ids[0], ids[1], ids[2]
	if got := suggestionTargets(t, sf, a); len(got) != 2 {
		t.Fatalf("suggestions for a = %v, want b and c", got)
	}

	if err := sf.svc.RecordSuggestionDecision(ctx, sf.manager, sf.tenant, b, a, faces.SuggestionDifferent, 0.91); err != nil {
		t.Fatal(err)
	}
	if got := suggestionTargets(t, sf, a); len(got) != 1 || got[0] != c {
		t.Fatalf("suggestions for a after ruling out b = %v, want only c", got)
	}
	if got := suggestionTargets(t, sf, b); len(got) != 1 || got[0] != c {
		t.Fatalf("suggestions for b = %v, want only c (the verdict applies both ways)", got)
	}
	var decision string
	var similarity float32
	var by *uuid.UUID
	sf.queryRow(`SELECT decision FROM face_suggestion_decisions WHERE tenant_id = $1`, &decision, sf.tenant)
	sf.queryRow(`SELECT similarity FROM face_suggestion_decisions WHERE tenant_id = $1`, &similarity, sf.tenant)
	sf.queryRow(`SELECT decided_by FROM face_suggestion_decisions WHERE tenant_id = $1`, &by, sf.tenant)
	if decision != faces.SuggestionDifferent || similarity < 0.909 || similarity > 0.911 || by == nil || *by != sf.manager.UserID {
		t.Fatalf("stored decision = %q %v by %v", decision, similarity, by)
	}

	// A later verdict replaces the earlier one, and "same" does not hide anything.
	if err := sf.svc.RecordSuggestionDecision(ctx, sf.manager, sf.tenant, a, b, faces.SuggestionSame, 0.91); err != nil {
		t.Fatal(err)
	}
	if got := suggestionTargets(t, sf, a); len(got) != 2 {
		t.Fatalf("suggestions for a after a same verdict = %v, want b and c", got)
	}
	if n := sf.scanInt(`SELECT count(*) FROM face_suggestion_decisions WHERE tenant_id = $1`, sf.tenant); n != 1 {
		t.Fatalf("decision rows = %d, want 1 (the pair is upserted)", n)
	}

	// The label outlives the crops it refers to.
	if err := sf.svc.RejectReviewSighting(ctx, sf.manager, sf.tenant, a); err != nil {
		t.Fatal(err)
	}
	if n := sf.scanInt(`SELECT count(*) FROM face_suggestion_decisions WHERE tenant_id = $1`, sf.tenant); n != 1 {
		t.Fatalf("decision rows after rejecting a sighting = %d, want 1", n)
	}
}

func TestSuggestionDecisionsValidateAndAuthorize(t *testing.T) {
	sf := newSyncFixture(t)
	sf.setCapability(sf.h01ID, sf.h01, true)
	sf.svc.Adapters = inventory.NewAdapters(sf.env.Svc)
	seedFaceSightings(t, sf, 84, 85, 86)
	ctx := context.Background()
	var a, b uuid.UUID
	sf.queryRow(`SELECT id FROM face_sightings WHERE tenant_id = $1 ORDER BY first_seen_at, id LIMIT 1`, &a, sf.tenant)
	sf.queryRow(`SELECT id FROM face_sightings WHERE tenant_id = $1 ORDER BY first_seen_at DESC, id DESC LIMIT 1`, &b, sf.tenant)

	var bad *faces.ValidationError
	for name, call := range map[string]func() error{
		"same identity": func() error {
			return sf.svc.RecordSuggestionDecision(ctx, sf.manager, sf.tenant, a, a, faces.SuggestionSame, 0.6)
		},
		"nil identity": func() error {
			return sf.svc.RecordSuggestionDecision(ctx, sf.manager, sf.tenant, uuid.Nil, b, faces.SuggestionSame, 0.6)
		},
		"unknown verdict": func() error { return sf.svc.RecordSuggestionDecision(ctx, sf.manager, sf.tenant, a, b, "maybe", 0.6) },
		"similarity > 1": func() error {
			return sf.svc.RecordSuggestionDecision(ctx, sf.manager, sf.tenant, a, b, faces.SuggestionSame, 1.5)
		},
		"similarity < 0": func() error {
			return sf.svc.RecordSuggestionDecision(ctx, sf.manager, sf.tenant, a, b, faces.SuggestionSame, -0.1)
		},
	} {
		if err := call(); !errors.As(err, &bad) {
			t.Fatalf("%s: err = %v, want a validation error", name, err)
		}
	}
	if err := sf.svc.RecordSuggestionDecision(ctx, sf.manager, sf.tenant, a, uuid.New(), faces.SuggestionDifferent, 0.6); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unknown identity: err = %v, want not found", err)
	}
	viewer := sf.user(sf.tenant, "viewer", "faces.view")
	if err := sf.svc.RecordSuggestionDecision(ctx, viewer, sf.tenant, a, b, faces.SuggestionDifferent, 0.6); !errors.Is(err, access.ErrForbidden) {
		t.Fatalf("viewer: err = %v, want forbidden", err)
	}
	if n := sf.scanInt(`SELECT count(*) FROM face_suggestion_decisions WHERE tenant_id = $1`, sf.tenant); n != 0 {
		t.Fatalf("decision rows after rejected calls = %d, want 0", n)
	}
}
