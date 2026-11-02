//go:build integration

package faces_test

import (
	"context"
	"testing"
	"time"

	"github.com/jdolan-exalink/openvms/internal/retention"
)

// sightingAt inserts a pending sighting with a crop and a 512-dimension vector, seen `age` ago.
func (sf *syncFixture) sightingAt(name string, age time.Duration, status string) {
	sf.t.Helper()
	person := "NULL"
	reviewed := "NULL"
	if status == "assigned" {
		sf.exec(`INSERT INTO face_persons (id, tenant_id, display_name, frigate_name) VALUES ('00000000-0000-0000-0000-0000000000aa', $1, 'Ana', 'ana') ON CONFLICT DO NOTHING`, sf.tenant)
		person = "'00000000-0000-0000-0000-0000000000aa'"
		reviewed = "now()"
	}
	sf.exec(`INSERT INTO face_sightings (tenant_id, server_id, source_file, source_seen_at, first_seen_at, crop_key, crop_content_type,
	           crop_size_bytes, crop_sha256, review_status, person_id, reviewed_at, embedding, embedding_profile, embedding_model, embedded_at)
	         VALUES ($1, $2, $3, now() - $4::interval, now() - $4::interval, $5, 'image/jpeg', 1000, repeat('a', 64), $6, `+person+`, `+reviewed+`,
	           ARRAY(SELECT 0.1::real FROM generate_series(1, 512)), 'cpu', 'arcface', now())`,
		sf.tenant, sf.h01ID, name+".jpg", age.String(), "faces/crops/"+name, status)
}

func TestRetentionErasesOldUnnamedFacesAndNothingElse(t *testing.T) {
	sf := newSyncFixture(t)
	ctx := context.Background()
	if _, err := retention.Save(ctx, sf.env.Store, retention.Settings{FaceUnknownDays: 30, PressureCleanup: true, PressurePercent: 85}, sf.manager.UserID); err != nil {
		t.Fatal(err)
	}
	sf.sightingAt("old", 45*24*time.Hour, "pending")
	sf.sightingAt("fresh", 2*24*time.Hour, "pending")
	sf.sightingAt("named", 90*24*time.Hour, "assigned")

	p := &retention.Purger{Store: sf.env.Store}
	res, err := p.Sweep(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if res.Faces != 1 || res.Pressure || res.Days != 30 {
		t.Fatalf("result = %+v, want 1 face erased at 30 days with no pressure", res)
	}
	// The old face keeps its row (as rejected) but loses the crop, the vector and its description.
	if n := sf.scanInt(`SELECT count(*) FROM face_sightings WHERE source_file = 'old.jpg' AND review_status = 'rejected'
	                   AND crop_key IS NULL AND embedding IS NULL AND embedding_profile IS NULL AND embedding_model IS NULL AND embedded_at IS NULL`); n != 1 {
		t.Fatalf("old sighting not erased completely, matches = %d", n)
	}
	if n := sf.scanInt(`SELECT count(*) FROM face_blob_purge WHERE storage_key = 'faces/crops/old'`); n != 1 {
		t.Fatalf("the crop was not queued for deletion, queue entries = %d", n)
	}
	// A recent unnamed face and a named one are untouched.
	if n := sf.scanInt(`SELECT count(*) FROM face_sightings WHERE source_file = 'fresh.jpg' AND review_status = 'pending' AND embedding IS NOT NULL AND crop_key IS NOT NULL`); n != 1 {
		t.Fatal("a recent unnamed face was touched")
	}
	if n := sf.scanInt(`SELECT count(*) FROM face_sightings WHERE source_file = 'named.jpg' AND review_status = 'assigned' AND embedding IS NOT NULL AND crop_key IS NOT NULL`); n != 1 {
		t.Fatal("a named face was touched")
	}
	if n := sf.scanInt(`SELECT count(*) FROM audit_log WHERE action = $1`, retention.ActionPurged); n != 1 {
		t.Fatalf("audit entries = %d, want 1", n)
	}
	// Running again finds nothing and writes no second audit entry.
	if res, err = p.Sweep(ctx); err != nil || res.Faces != 0 {
		t.Fatalf("second sweep = %+v, %v", res, err)
	}
	if n := sf.scanInt(`SELECT count(*) FROM audit_log WHERE action = $1`, retention.ActionPurged); n != 1 {
		t.Fatalf("a sweep that erased nothing wrote an audit entry (%d)", n)
	}
}

func TestRetentionZeroKeepsEverythingAndPressureShortensIt(t *testing.T) {
	sf := newSyncFixture(t)
	ctx := context.Background()
	sf.sightingAt("a", 20*24*time.Hour, "pending")
	sf.sightingAt("b", 3*24*time.Hour, "pending")

	forever := retention.Settings{FaceUnknownDays: 0, PressureCleanup: false, PressurePercent: 85}
	if _, err := retention.Save(ctx, sf.env.Store, forever, sf.manager.UserID); err != nil {
		t.Fatal(err)
	}
	full := func() (float64, bool) { return 92, true }
	p := &retention.Purger{Store: sf.env.Store, VolumePercent: full}
	if res, err := p.Sweep(ctx); err != nil || res.Faces != 0 || res.Pressure {
		t.Fatalf("0 days with the cleanup option off must erase nothing: %+v, %v", res, err)
	}

	// With the option on and the volume over the limit, "forever" behaves as 15 days: the 20-day-old face goes.
	forever.PressureCleanup = true
	if _, err := retention.Save(ctx, sf.env.Store, forever, sf.manager.UserID); err != nil {
		t.Fatal(err)
	}
	res, err := p.Sweep(ctx)
	if err != nil || res.Faces != 1 || !res.Pressure || res.Days != 15 {
		t.Fatalf("pressure sweep = %+v, %v, want 1 face at 15 days", res, err)
	}
	// Under the limit the same setting erases nothing.
	p.VolumePercent = func() (float64, bool) { return 40, true }
	if res, err = p.Sweep(ctx); err != nil || res.Faces != 0 || res.Pressure {
		t.Fatalf("below the limit: %+v, %v", res, err)
	}
	// An unmeasurable volume is not pressure.
	p.VolumePercent = func() (float64, bool) { return 0, false }
	if res, err = p.Sweep(ctx); err != nil || res.Pressure {
		t.Fatalf("unknown volume: %+v, %v", res, err)
	}
}

func TestRetentionPreviewCountsWhatWouldBeErased(t *testing.T) {
	sf := newSyncFixture(t)
	ctx := context.Background()
	sf.sightingAt("old1", 45*24*time.Hour, "pending")
	sf.sightingAt("old2", 40*24*time.Hour, "pending")
	sf.sightingAt("fresh", 1*24*time.Hour, "pending")
	records, bytes, err := retention.FacePreview(ctx, sf.env.Store, 30, time.Now())
	if err != nil || records != 2 || bytes != 2000 {
		t.Fatalf("preview = %d records, %d bytes, %v; want 2 and 2000", records, bytes, err)
	}
	if r, b, err := retention.FacePreview(ctx, sf.env.Store, 0, time.Now()); err != nil || r != 0 || b != 0 {
		t.Fatalf("0 days must preview nothing, got %d/%d/%v", r, b, err)
	}
}
