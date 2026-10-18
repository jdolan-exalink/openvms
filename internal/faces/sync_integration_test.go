//go:build integration

package faces_test

import (
	"bytes"
	"context"
	"errors"
	"sort"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/jdolan-exalink/openvms/internal/access"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/faces"
	"github.com/jdolan-exalink/openvms/internal/frigatemock"
	"github.com/jdolan-exalink/openvms/internal/inventory"
	"github.com/jdolan-exalink/openvms/internal/store"
)

type syncFixture struct {
	*fixture
	y       *faces.Syncer
	manager authz.Actor
	h01     *frigatemock.Server
	c01     *frigatemock.Server
	h01ID   uuid.UUID
	c01ID   uuid.UUID
}

func newSyncFixture(t *testing.T) *syncFixture {
	t.Helper()
	f := newFixture(t)
	sf := &syncFixture{fixture: f,
		y:       &faces.Syncer{Store: f.env.Store, Blobs: f.blobs, Adapters: inventory.NewAdapters(f.env.Svc), Log: f.svc.Log},
		manager: f.user(f.tenant, "mgr", authz.FacesManage, authz.FacesView),
		h01:     f.env.Mocks["frigate-h01"].Server, c01: f.env.Mocks["frigate-c01"].Server,
		h01ID: f.env.Cameras["frigate-h01/acceso_norte"].ServerID, c01ID: f.env.Cameras["frigate-c01/muelle"].ServerID,
	}
	return sf
}

// setCapability flips the face_recognition capability the sync reads from the server row, and
// the mock's own switch.
func (sf *syncFixture) setCapability(serverID uuid.UUID, mock *frigatemock.Server, on bool) {
	sf.t.Helper()
	mock.FaceRecognition = on
	v := "false"
	if on {
		v = "true"
	}
	sf.exec(`UPDATE frigate_servers SET capabilities = capabilities || jsonb_build_object('face_recognition', $2::boolean) WHERE id = $1`, serverID, v == "true")
}

func (sf *syncFixture) person(name string, images ...uint8) faces.Person {
	sf.t.Helper()
	p, err := sf.svc.CreatePerson(context.Background(), sf.manager, sf.tenant, faces.PersonInput{DisplayName: name})
	if err != nil {
		sf.t.Fatal(err)
	}
	for _, seed := range images {
		sf.addImage(p.ID, seed)
	}
	return p
}

func (sf *syncFixture) addImage(personID uuid.UUID, seed uint8) faces.Image {
	sf.t.Helper()
	img, err := sf.svc.AddImage(context.Background(), sf.manager, sf.tenant, personID, pngOf(sf.t, seed), "image/png")
	if err != nil {
		sf.t.Fatal(err)
	}
	return img
}

// run executes sync passes until nothing is due (bounded).
func (sf *syncFixture) run() {
	sf.t.Helper()
	for i := 0; i < 4; i++ {
		if err := sf.y.RunOnce(context.Background(), 50); err != nil {
			sf.t.Fatal(err)
		}
	}
}

func (sf *syncFixture) status(personID, serverID uuid.UUID) (status, lastErr string) {
	sf.t.Helper()
	list, err := sf.svc.ListSyncStatus(context.Background(), sf.manager, sf.tenant, personID)
	if err != nil {
		sf.t.Fatal(err)
	}
	for _, s := range list {
		if s.ServerID == serverID {
			return s.Status, s.LastError
		}
	}
	sf.t.Fatalf("no sync status for server %s", serverID)
	return "", ""
}

func libraryBytes(m *frigatemock.Server, name string) [][]byte {
	var out [][]byte
	for _, id := range m.FaceLibrary()[name] {
		out = append(out, m.FaceImageBytes(name, id))
	}
	sort.Slice(out, func(i, j int) bool { return bytes.Compare(out[i], out[j]) < 0 })
	return out
}

func sameBytes(t *testing.T, got [][]byte, want ...[]byte) {
	t.Helper()
	sort.Slice(want, func(i, j int) bool { return bytes.Compare(want[i], want[j]) < 0 })
	if len(got) != len(want) {
		t.Fatalf("library has %d images, want %d", len(got), len(want))
	}
	for i := range got {
		if !bytes.Equal(got[i], want[i]) {
			t.Fatalf("library image %d differs from the central image", i)
		}
	}
}

func TestSyncConvergesCreateRegisterAndDelete(t *testing.T) {
	sf := newSyncFixture(t)
	sf.setCapability(sf.h01ID, sf.h01, true)
	p := sf.person("Ana", 1, 2)

	sf.run()
	sameBytes(t, libraryBytes(sf.h01, p.FrigateName), pngOf(t, 1), pngOf(t, 2))
	if st, _ := sf.status(p.ID, sf.h01ID); st != "synced" {
		t.Fatalf("h01 status = %q, want synced", st)
	}
	// The other server has face recognition off: nothing is written and the job says so.
	if len(sf.c01.FaceLibrary()) != 0 || len(sf.c01.FaceCalls()) != 0 {
		t.Fatalf("disabled server was written to: %v", sf.c01.FaceCalls())
	}
	if st, msg := sf.status(p.ID, sf.c01ID); st != "disabled" || msg == "" {
		t.Fatalf("c01 status = %q (%q), want disabled with a reason", st, msg)
	}

	// Added and removed images propagate; display-name edits do not touch Frigate.
	img3 := sf.addImage(p.ID, 3)
	sf.run()
	sameBytes(t, libraryBytes(sf.h01, p.FrigateName), pngOf(t, 1), pngOf(t, 2), pngOf(t, 3))
	if err := sf.svc.RemoveImage(context.Background(), sf.manager, sf.tenant, p.ID, img3.ID); err != nil {
		t.Fatal(err)
	}
	sf.run()
	sameBytes(t, libraryBytes(sf.h01, p.FrigateName), pngOf(t, 1), pngOf(t, 2))

	before := len(sf.h01.FaceCalls())
	newName := "Ana Maria"
	if _, err := sf.svc.UpdatePerson(context.Background(), sf.manager, sf.tenant, p.ID, faces.PersonUpdate{DisplayName: &newName}); err != nil {
		t.Fatal(err)
	}
	sf.run()
	if after := len(sf.h01.FaceCalls()); after != before {
		t.Fatalf("a display-name edit caused %d Frigate writes", after-before)
	}
}

func TestSyncIsIdempotentAndDedupesByImage(t *testing.T) {
	sf := newSyncFixture(t)
	sf.setCapability(sf.h01ID, sf.h01, true)
	p := sf.person("Ana", 1)
	sf.run()
	calls := len(sf.h01.FaceCalls())

	// The same bytes cannot be enrolled twice, so Frigate never receives a duplicate.
	if _, err := sf.svc.AddImage(context.Background(), sf.manager, sf.tenant, p.ID, pngOf(t, 1), "image/png"); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("duplicate = %v, want conflict", err)
	}
	// Re-running with unchanged desired state performs no Frigate writes at all.
	sf.exec(`UPDATE face_server_sync SET status = 'pending', next_attempt_at = now() WHERE person_id = $1`, p.ID)
	sf.run()
	if got := len(sf.h01.FaceCalls()); got != calls {
		t.Fatalf("re-sync made %d extra writes: %v", got-calls, sf.h01.FaceCalls()[calls:])
	}
	sameBytes(t, libraryBytes(sf.h01, p.FrigateName), pngOf(t, 1))
}

func TestSyncDriftIsCorrectedInOwnedFacesOnly(t *testing.T) {
	sf := newSyncFixture(t)
	sf.setCapability(sf.h01ID, sf.h01, true)
	p := sf.person("Ana", 1)
	sf.run()
	// Someone adds a stray image to the owned face, deletes none, and a foreign face exists.
	sf.h01.SeedFaceImage(p.FrigateName, "stray.webp", []byte("stray"))
	sf.h01.SeedFaceImage("visitas", "v1.webp", []byte("v1"))
	sf.exec(`UPDATE face_persons SET sync_revision = sync_revision + 1 WHERE id = $1`, p.ID)
	sf.exec(`UPDATE face_server_sync SET desired_revision = desired_revision + 1, status = 'pending', next_attempt_at = now() WHERE person_id = $1`, p.ID)
	sf.run()
	sameBytes(t, libraryBytes(sf.h01, p.FrigateName), pngOf(t, 1))
	if got := sf.h01.FaceLibrary()["visitas"]; len(got) != 1 {
		t.Fatalf("foreign face was modified: %v", got)
	}
}

func TestSyncNeverTouchesForeignFaces(t *testing.T) {
	sf := newSyncFixture(t)
	sf.setCapability(sf.h01ID, sf.h01, true)
	// A face that already exists in Frigate with the same name as a central person, plus an
	// unrelated one and an empty folder that may be adopted.
	sf.h01.SeedFaceImage("bea", "b1.webp", []byte("theirs"))
	sf.h01.SeedFaceImage("visitas", "v1.webp", []byte("v1"))
	sf.h01.SeedFaceImage("carla", "", nil)
	bea := sf.person("Bea", 1)
	carla := sf.person("Carla", 2)
	sf.run()

	if st, msg := sf.status(bea.ID, sf.h01ID); st != "conflict" || msg == "" {
		t.Fatalf("bea status = %q (%q), want conflict", st, msg)
	}
	if got := sf.h01.FaceLibrary()["bea"]; len(got) != 1 || !bytes.Equal(sf.h01.FaceImageBytes("bea", got[0]), []byte("theirs")) {
		t.Fatalf("foreign face bea was modified: %v", got)
	}
	if st, _ := sf.status(carla.ID, sf.h01ID); st != "synced" {
		t.Fatalf("empty same-named folder was not adopted: %q", st)
	}
	sameBytes(t, libraryBytes(sf.h01, "carla"), pngOf(t, 2))

	// Deleting the conflicted person must not delete the foreign face either.
	if err := sf.svc.DeletePerson(context.Background(), sf.manager, sf.tenant, bea.ID); err != nil {
		t.Fatal(err)
	}
	sf.run()
	if got := sf.h01.FaceLibrary()["bea"]; len(got) != 1 {
		t.Fatalf("foreign face bea deleted with the central person: %v", got)
	}
	if got := sf.h01.FaceLibrary()["visitas"]; len(got) != 1 {
		t.Fatalf("unrelated face modified: %v", got)
	}
	if n := sf.scanInt(`SELECT count(*) FROM face_persons WHERE id = $1`, bea.ID); n != 0 {
		t.Fatalf("conflicted tombstone was not purged (%d rows)", n)
	}
}

func TestSyncConflictClearsOnManualRetry(t *testing.T) {
	sf := newSyncFixture(t)
	sf.setCapability(sf.h01ID, sf.h01, true)
	sf.h01.SeedFaceImage("dana", "d1.webp", []byte("theirs"))
	p := sf.person("Dana", 1)
	sf.run()
	if st, _ := sf.status(p.ID, sf.h01ID); st != "conflict" {
		t.Fatalf("status = %q, want conflict", st)
	}
	viewer := sf.user(sf.tenant, "viewer", authz.FacesView)
	if _, err := sf.svc.RetrySync(context.Background(), viewer, sf.tenant, p.ID); !errors.Is(err, access.ErrForbidden) {
		t.Fatalf("viewer retry = %v, want forbidden", err)
	}
	// The operator removes the foreign face, then retries.
	sf.h01.DeleteFaceForTest("dana")
	n, err := sf.svc.RetrySync(context.Background(), sf.manager, sf.tenant, p.ID)
	if err != nil || n != 1 {
		t.Fatalf("retry = %d, %v", n, err)
	}
	sf.run()
	if st, _ := sf.status(p.ID, sf.h01ID); st != "synced" {
		t.Fatalf("status after retry = %q, want synced", st)
	}
	sameBytes(t, libraryBytes(sf.h01, "dana"), pngOf(t, 1))
}

func TestSyncOfflineServerStaysPendingAndRecovers(t *testing.T) {
	sf := newSyncFixture(t)
	sf.setCapability(sf.h01ID, sf.h01, true)
	var goodURL string
	sf.scan(func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), `SELECT base_url FROM frigate_servers WHERE id = $1`, sf.h01ID).Scan(&goodURL)
	})
	sf.exec(`UPDATE frigate_servers SET base_url = 'http://127.0.0.1:1' WHERE id = $1`, sf.h01ID)
	p := sf.person("Ana", 1)

	// Far more attempts than the permanent-failure limit: an unreachable server never fails the job.
	for i := 0; i < 12; i++ {
		sf.run()
		sf.exec(`UPDATE face_server_sync SET next_attempt_at = now() WHERE person_id = $1 AND status = 'pending'`, p.ID)
	}
	st, msg := sf.status(p.ID, sf.h01ID)
	if st != "pending" || msg == "" {
		t.Fatalf("offline status = %q (%q), want pending with a reason", st, msg)
	}
	if strings.Contains(msg, p.FrigateName) || strings.Contains(msg, "/api/") {
		t.Fatalf("status message leaks request details: %q", msg)
	}
	var backoff bool
	sf.scan(func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), `SELECT attempts > 0 FROM face_server_sync WHERE person_id = $1 AND server_id = $2`, p.ID, sf.h01ID).Scan(&backoff)
	})
	if !backoff {
		t.Fatal("attempts were not counted")
	}

	sf.exec(`UPDATE frigate_servers SET base_url = $2 WHERE id = $1`, sf.h01ID, goodURL)
	sf.exec(`UPDATE face_server_sync SET next_attempt_at = now() WHERE person_id = $1`, p.ID)
	sf.run()
	if st, _ := sf.status(p.ID, sf.h01ID); st != "synced" {
		t.Fatalf("status after recovery = %q, want synced", st)
	}
	sameBytes(t, libraryBytes(sf.h01, p.FrigateName), pngOf(t, 1))
}

func (f *fixture) scan(fn func(tx pgx.Tx) error) {
	f.t.Helper()
	if err := f.env.Store.TxRaw(context.Background(), store.AllTenants, fn); err != nil {
		f.t.Fatal(err)
	}
}

func TestSyncCapabilityFlipAndNewServersEnqueueLivePersons(t *testing.T) {
	sf := newSyncFixture(t)
	p := sf.person("Ana", 1)
	sf.run()
	// Both servers start without face recognition: jobs exist, blocked and explained.
	for _, id := range []uuid.UUID{sf.h01ID, sf.c01ID} {
		if st, msg := sf.status(p.ID, id); st != "disabled" || msg == "" {
			t.Fatalf("server %s status = %q (%q), want disabled", id, st, msg)
		}
	}
	if len(sf.h01.FaceCalls()) != 0 {
		t.Fatalf("writes while disabled: %v", sf.h01.FaceCalls())
	}
	// The capability flips on: the job becomes due and converges.
	sf.setCapability(sf.c01ID, sf.c01, true)
	sf.run()
	if st, _ := sf.status(p.ID, sf.c01ID); st != "synced" {
		t.Fatalf("c01 after flip = %q, want synced", st)
	}
	sameBytes(t, libraryBytes(sf.c01, p.FrigateName), pngOf(t, 1))
	if st, _ := sf.status(p.ID, sf.h01ID); st != "disabled" {
		t.Fatalf("h01 = %q, want still disabled", st)
	}

	// A server registered later gets a job for every live person.
	sf.exec(`INSERT INTO frigate_servers (id, tenant_id, site_id, name, base_url, username, password_sealed, capabilities)
	         SELECT gen_random_uuid(), tenant_id, site_id, 'frigate-new', base_url, username, password_sealed, '{"face_recognition": false}'
	         FROM frigate_servers WHERE id = $1`, sf.h01ID)
	sf.run()
	if n := sf.scanInt(`SELECT count(*) FROM face_server_sync WHERE person_id = $1`, p.ID); n != 3 {
		t.Fatalf("jobs for the person = %d, want 3 (one per server)", n)
	}
}

func TestSyncTombstonePropagatesPurgesAndScrubsSubLabels(t *testing.T) {
	sf := newSyncFixture(t)
	sf.setCapability(sf.h01ID, sf.h01, true)
	sf.setCapability(sf.c01ID, sf.c01, true)
	ana := sf.person("Ana", 1, 2)
	other := sf.person("Otra", 3)
	sf.run()
	if len(sf.h01.FaceLibrary()[ana.FrigateName]) != 2 || len(sf.c01.FaceLibrary()[ana.FrigateName]) != 2 {
		t.Fatal("setup: faces not synced")
	}
	cam := sf.env.Cameras["frigate-h01/acceso_norte"]
	insertEvent := func(remote string, labels ...string) {
		sf.exec(`INSERT INTO events (tenant_id, site_id, server_id, camera_id, remote_id, severity, sub_labels, start_time)
		         VALUES ($1, $2, $3, $4, $5, 'alert', $6, now())`, cam.TenantID, cam.SiteID, cam.ServerID, cam.ID, remote, labels)
	}
	insertEvent("e1", ana.FrigateName, other.FrigateName)
	insertEvent("e2", ana.FrigateName)
	insertEvent("e3", "someone_else")

	if err := sf.svc.DeletePerson(context.Background(), sf.manager, sf.tenant, ana.ID); err != nil {
		t.Fatal(err)
	}
	// Until every server converged the tombstone and the names stay put.
	if n := sf.scanInt(`SELECT count(*) FROM face_persons WHERE id = $1`, ana.ID); n != 1 {
		t.Fatal("tombstone vanished before the sync")
	}
	sf.run()

	for name, m := range map[string]*frigatemock.Server{"h01": sf.h01, "c01": sf.c01} {
		if _, ok := m.FaceLibrary()[ana.FrigateName]; ok {
			t.Errorf("%s still has the deleted face", name)
		}
		if len(m.FaceLibrary()[other.FrigateName]) != 1 {
			t.Errorf("%s lost an unrelated person's face", name)
		}
	}
	if n := sf.scanInt(`SELECT count(*) FROM face_persons WHERE id = $1`, ana.ID); n != 0 {
		t.Fatal("tombstone was not purged after convergence")
	}
	if n := sf.scanInt(`SELECT count(*) FROM events WHERE $1 = ANY(sub_labels)`, ana.FrigateName); n != 0 {
		t.Fatalf("%d events still carry the deleted name", n)
	}
	if n := sf.scanInt(`SELECT count(*) FROM events WHERE sub_labels = ARRAY[$1]::text[]`, other.FrigateName); n != 1 {
		t.Fatalf("other labels were not preserved (%d)", n)
	}
	if n := sf.scanInt(`SELECT count(*) FROM events WHERE sub_labels = ARRAY['someone_else']::text[]`); n != 1 {
		t.Fatal("unrelated event was modified")
	}
	if err := sf.svc.PurgeBlobs(context.Background(), 100); err != nil {
		t.Fatal(err)
	}
	if sf.blobs.keys() != 1 {
		t.Fatalf("blobs left = %d, want only the other person's image", sf.blobs.keys())
	}

	// Audited, with ids and counts only; rerunning is a no-op.
	scrubbed := sf.scanInt(`SELECT count(*) FROM audit_log WHERE action = $1 AND (details->>'events')::int = 2`, faces.ActionSubLabelScrubbed)
	purged := sf.scanInt(`SELECT count(*) FROM audit_log WHERE action = $1 AND (details->>'person_id') = $2`, faces.ActionPersonPurged, ana.ID.String())
	if scrubbed != 1 || purged != 1 {
		t.Fatalf("audit rows scrub=%d purge=%d, want 1/1", scrubbed, purged)
	}
	if n := sf.scanInt(`SELECT count(*) FROM audit_log WHERE action LIKE 'FACE_%' AND details::text ILIKE '%ana%'`); n != 0 {
		t.Fatalf("audit details leak the name (%d rows)", n)
	}
	sf.run()
	if n := sf.scanInt(`SELECT count(*) FROM audit_log WHERE action IN ($1, $2)`, faces.ActionSubLabelScrubbed, faces.ActionPersonPurged); n != 2 {
		t.Fatalf("rerun wrote more audit rows: %d", n)
	}
}

func TestSyncRecoversFromLostWriteResponses(t *testing.T) {
	sf := newSyncFixture(t)
	sf.setCapability(sf.h01ID, sf.h01, true)
	p := sf.person("Ana", 1, 2)
	// The first create and the first register are applied by Frigate but the answers are lost.
	sf.h01.DropFaceResponses("create", 1)
	sf.h01.DropFaceResponses("register", 1)

	for i := 0; i < 6; i++ {
		sf.run()
		sf.exec(`UPDATE face_server_sync SET next_attempt_at = now() WHERE person_id = $1 AND status = 'pending'`, p.ID)
	}
	if st, msg := sf.status(p.ID, sf.h01ID); st != "synced" {
		t.Fatalf("status = %q (%q), want synced", st, msg)
	}
	// No duplicates and no leftover orphan from the lost response.
	sameBytes(t, libraryBytes(sf.h01, p.FrigateName), pngOf(t, 1), pngOf(t, 2))
	if n := sf.scanInt(`SELECT count(*) FROM face_server_images WHERE person_id = $1`, p.ID); n != 2 {
		t.Fatalf("mapped images = %d, want 2", n)
	}
}

func TestSyncStaleRevisionCannotOverwriteNewer(t *testing.T) {
	sf := newSyncFixture(t)
	sf.setCapability(sf.h01ID, sf.h01, true)
	p := sf.person("Ana", 1)
	ctx := context.Background()
	if err := sf.y.Enqueue(ctx); err != nil {
		t.Fatal(err)
	}
	claimed, err := sf.y.Claim(ctx, 10)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claim = %v, %v", claimed, err)
	}
	// A newer desired state lands while the job is in flight.
	sf.addImage(p.ID, 2)
	if err := sf.y.Process(ctx, claimed[0]); err != nil {
		t.Fatal(err)
	}
	if len(sf.h01.FaceCalls()) != 0 {
		t.Fatalf("stale job wrote to Frigate: %v", sf.h01.FaceCalls())
	}
	if st, _ := sf.status(p.ID, sf.h01ID); st != "pending" {
		t.Fatalf("status = %q, want pending for the newer revision", st)
	}
	sf.run()
	sameBytes(t, libraryBytes(sf.h01, p.FrigateName), pngOf(t, 1), pngOf(t, 2))
	if n := sf.scanInt(`SELECT count(*) FROM face_server_sync WHERE person_id = $1 AND synced_revision = desired_revision`, p.ID); n < 1 {
		t.Fatal("newer revision never marked synced")
	}
}

func TestSyncLeaseRecovery(t *testing.T) {
	sf := newSyncFixture(t)
	sf.setCapability(sf.h01ID, sf.h01, true)
	p := sf.person("Ana", 1)
	ctx := context.Background()
	if err := sf.y.Enqueue(ctx); err != nil {
		t.Fatal(err)
	}
	first, err := sf.y.Claim(ctx, 10)
	if err != nil || len(first) != 1 {
		t.Fatalf("first claim = %v, %v", first, err)
	}
	// The worker "crashes" holding the lease: nobody else may take the job yet...
	again, err := sf.y.Claim(ctx, 10)
	if err != nil || len(again) != 0 {
		t.Fatalf("job claimed while leased: %v, %v", again, err)
	}
	// ...but once the lease expires another worker does, and finishes it.
	sf.exec(`UPDATE face_server_sync SET lease_until = now() - interval '1 second' WHERE person_id = $1`, p.ID)
	second, err := sf.y.Claim(ctx, 10)
	if err != nil || len(second) != 1 {
		t.Fatalf("claim after lease expiry = %v, %v", second, err)
	}
	if err := sf.y.Process(ctx, second[0]); err != nil {
		t.Fatal(err)
	}
	if st, _ := sf.status(p.ID, sf.h01ID); st != "synced" {
		t.Fatalf("status = %q, want synced", st)
	}
}

func TestSyncPermanentFailureIsBoundedAndRetryable(t *testing.T) {
	sf := newSyncFixture(t)
	sf.setCapability(sf.h01ID, sf.h01, true)
	p := sf.person("Ana", 1)
	// The central bytes disappear from the object store.
	var key string
	sf.scan(func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), `SELECT storage_key FROM face_person_images WHERE person_id = $1`, p.ID).Scan(&key)
	})
	saved, _, err := sf.blobs.Get(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	_ = sf.blobs.Delete(context.Background(), key)

	for i := 0; i < 12; i++ {
		sf.run()
		sf.exec(`UPDATE face_server_sync SET next_attempt_at = now() WHERE person_id = $1 AND status = 'pending'`, p.ID)
	}
	st, msg := sf.status(p.ID, sf.h01ID)
	if st != "failed" || msg == "" {
		t.Fatalf("status = %q (%q), want failed with a reason", st, msg)
	}
	if err := sf.blobs.Put(context.Background(), key, saved, "image/png"); err != nil {
		t.Fatal(err)
	}
	if n, err := sf.svc.RetrySync(context.Background(), sf.manager, sf.tenant, p.ID); err != nil || n != 1 {
		t.Fatalf("retry = %d, %v", n, err)
	}
	sf.run()
	if st, _ := sf.status(p.ID, sf.h01ID); st != "synced" {
		t.Fatalf("status after retry = %q, want synced", st)
	}
	if n := sf.scanInt(`SELECT count(*) FROM audit_log WHERE action = $1`, faces.ActionSyncRetried); n != 1 {
		t.Fatalf("retry audit rows = %d, want 1", n)
	}
}

func TestSyncRemoteDisabledBacksOffInsteadOfSpinning(t *testing.T) {
	sf := newSyncFixture(t)
	// The server row claims the capability but Frigate itself answers "not enabled".
	sf.exec(`UPDATE frigate_servers SET capabilities = capabilities || '{"face_recognition": true}' WHERE id = $1`, sf.h01ID)
	sf.h01.FaceRecognition = false
	p := sf.person("Ana", 1)
	sf.run()
	calls := len(sf.h01.FaceCalls())
	for i := 0; i < 3; i++ {
		sf.run()
	}
	if st, _ := sf.status(p.ID, sf.h01ID); st != "disabled" {
		t.Fatalf("status = %q, want disabled", st)
	}
	if got := len(sf.h01.FaceCalls()); got != calls {
		t.Fatalf("worker keeps hammering a disabled Frigate (%d new calls)", got-calls)
	}
}

func tombstoneWithEvent(t *testing.T, mutate func(sf *syncFixture)) (*syncFixture, faces.Person) {
	sf := newSyncFixture(t)
	sf.setCapability(sf.h01ID, sf.h01, true)
	sf.setCapability(sf.c01ID, sf.c01, true)
	ana := sf.person("Ana", 1)
	sf.run()
	cam := sf.env.Cameras["frigate-h01/acceso_norte"]
	sf.exec(`INSERT INTO events (tenant_id, site_id, server_id, camera_id, remote_id, severity, sub_labels, start_time)
	         VALUES ($1, $2, $3, $4, 'e1', 'alert', $5, now())`, cam.TenantID, cam.SiteID, cam.ServerID, cam.ID, []string{ana.FrigateName})
	mutate(sf)
	if err := sf.svc.DeletePerson(context.Background(), sf.manager, sf.tenant, ana.ID); err != nil {
		t.Fatal(err)
	}
	sf.run()
	// Purged, and the name scrubbed from stored events.
	if n := sf.scanInt(`SELECT (SELECT count(*) FROM face_persons WHERE id = $1) + (SELECT count(*) FROM events WHERE $2 = ANY(sub_labels))`, ana.ID, ana.FrigateName); n != 0 {
		t.Fatalf("tombstone or scrubbed name left behind (%d)", n)
	}
	return sf, ana
}

func TestSyncTombstonePurgesWhenOwningServerWasSoftDeleted(t *testing.T) {
	tombstoneWithEvent(t, func(sf *syncFixture) {
		sf.exec(`UPDATE frigate_servers SET deleted_at = now() WHERE id = $1`, sf.c01ID)
	})
}

func TestSyncTombstonePurgesWhenServerLostFaceCapabilityAndCleansUpLater(t *testing.T) {
	sf, ana := tombstoneWithEvent(t, func(sf *syncFixture) { sf.setCapability(sf.c01ID, sf.c01, false) })
	if n := sf.scanInt(`SELECT count(*) FROM face_remote_cleanup WHERE server_id = $1`, sf.c01ID); n != 1 {
		t.Fatalf("remote cleanup obligations = %d, want 1", n)
	}
	if len(sf.c01.FaceLibrary()[ana.FrigateName]) == 0 {
		t.Fatal("setup: the remote face should still exist while the server is face-disabled")
	}
	// A new person must not reclaim a Frigate name that still has a pending remote cleanup,
	// or the cleanup would delete the new person's face.
	if again := sf.person("Ana"); again.FrigateName == ana.FrigateName {
		t.Fatalf("new person reused frigate name %q while its remote cleanup is pending", ana.FrigateName)
	}
	sf.setCapability(sf.c01ID, sf.c01, true)
	sf.run()
	if _, ok := sf.c01.FaceLibrary()[ana.FrigateName]; ok {
		t.Fatal("remote face was not deleted once the server was face-capable again")
	}
	if n := sf.scanInt(`SELECT count(*) FROM face_remote_cleanup`); n != 0 {
		t.Fatalf("cleanup obligations left = %d, want 0", n)
	}
}

func TestSyncTombstoneWithFailedDeleteIsRetryable(t *testing.T) {
	sf := newSyncFixture(t)
	sf.setCapability(sf.h01ID, sf.h01, true)
	p := sf.person("Ana", 1)
	sf.run()
	if err := sf.svc.DeletePerson(context.Background(), sf.manager, sf.tenant, p.ID); err != nil {
		t.Fatal(err)
	}
	// The remote delete exhausted its attempts (e.g. wrong credentials), so the tombstone is stuck.
	sf.exec(`UPDATE face_server_sync SET status = 'failed', last_error = 'unauthorized' WHERE person_id = $1 AND server_id = $2`, p.ID, sf.h01ID)
	sf.run()
	if n := sf.scanInt(`SELECT count(*) FROM face_persons WHERE id = $1`, p.ID); n != 1 {
		t.Fatal("setup: tombstone should still block on its failed delete")
	}
	if n, err := sf.svc.RetrySync(context.Background(), sf.manager, sf.tenant, p.ID); err != nil || n != 1 {
		t.Fatalf("retry on a deleted person = %d, %v; want 1 requeued job", n, err)
	}
	sf.run()
	if _, ok := sf.h01.FaceLibrary()[p.FrigateName]; ok {
		t.Fatal("remote face was not deleted after retrying the tombstone")
	}
	if n := sf.scanInt(`SELECT count(*) FROM face_persons WHERE id = $1`, p.ID); n != 0 {
		t.Fatal("tombstone was not purged after a successful retry")
	}
}
