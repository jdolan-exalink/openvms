//go:build integration

package faces_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/faces"
)

// cancelOnPutBlobs cancels the request context from inside Put and then fails, the way a
// client disconnect mid-upload does.
type cancelOnPutBlobs struct {
	*memBlobs
	cancel context.CancelFunc
}

func (c *cancelOnPutBlobs) Put(ctx context.Context, key string, body []byte, ct string) error {
	c.cancel()
	return errors.New("upload interrupted")
}

// failDeleteBlobs fails Delete while fail is set.
type failDeleteBlobs struct {
	*memBlobs
	mu   sync.Mutex
	fail bool
	err  error // last ctx.Err() seen by Delete
}

func (b *failDeleteBlobs) Delete(ctx context.Context, key string) error {
	b.mu.Lock()
	b.err = ctx.Err()
	fail := b.fail
	b.mu.Unlock()
	if fail {
		return errors.New("object store down")
	}
	return b.memBlobs.Delete(ctx, key)
}

func TestAddImageCapIsRaceSafe(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	manager := f.user(f.tenant, "mgr", authz.FacesManage, authz.FacesView)
	p, err := f.svc.CreatePerson(ctx, manager, f.tenant, faces.PersonInput{DisplayName: "Ana"})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < faces.MaxImagesPerPerson-2; i++ {
		if _, err := f.svc.AddImage(ctx, manager, f.tenant, p.ID, pngOf(t, uint8(i+1)), "image/png"); err != nil {
			t.Fatal(err)
		}
	}
	const racers = 8
	var wg sync.WaitGroup
	results := make([]error, racers)
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, results[i] = f.svc.AddImage(ctx, manager, f.tenant, p.ID, pngOf(t, uint8(100+i)), "image/png")
		}()
	}
	wg.Wait()
	ok := 0
	for _, err := range results {
		if err == nil {
			ok++
		}
	}
	if ok != 2 {
		t.Fatalf("%d concurrent adds succeeded with 2 free slots, want exactly 2: %v", ok, results)
	}
	if n := f.scanInt(`SELECT count(*) FROM face_person_images WHERE person_id = $1`, p.ID); n != faces.MaxImagesPerPerson {
		t.Fatalf("image rows = %d, want %d", n, faces.MaxImagesPerPerson)
	}
}

func TestAddImageRollbackSurvivesCanceledRequestContext(t *testing.T) {
	f := newFixture(t)
	manager := f.user(f.tenant, "mgr", authz.FacesManage, authz.FacesView)
	p, err := f.svc.CreatePerson(context.Background(), manager, f.tenant, faces.PersonInput{DisplayName: "Ana"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	svc := &faces.Service{Store: f.env.Store, Blobs: &cancelOnPutBlobs{memBlobs: f.blobs, cancel: cancel}, Log: f.svc.Log}
	if _, err := svc.AddImage(ctx, manager, f.tenant, p.ID, pngOf(t, 1), "image/png"); err == nil {
		t.Fatal("want upload failure")
	}
	if n := f.scanInt(`SELECT count(*) FROM face_person_images WHERE person_id = $1`, p.ID); n != 0 {
		t.Fatalf("metadata row left behind after canceled upload: %d", n)
	}
}

func TestBlobCleanupIsDurableAndIndependentOfRequestContext(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	blobs := &failDeleteBlobs{memBlobs: f.blobs}
	svc := &faces.Service{Store: f.env.Store, Blobs: blobs, Log: f.svc.Log}
	manager := f.user(f.tenant, "mgr", authz.FacesManage, authz.FacesView)
	p, err := svc.CreatePerson(ctx, manager, f.tenant, faces.PersonInput{DisplayName: "Ana"})
	if err != nil {
		t.Fatal(err)
	}
	img, err := svc.AddImage(ctx, manager, f.tenant, p.ID, pngOf(t, 1), "image/png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AddImage(ctx, manager, f.tenant, p.ID, pngOf(t, 2), "image/png"); err != nil {
		t.Fatal(err)
	}

	// Removing an image only queues the blob; deletion happens in the purge pass.
	if err := svc.RemoveImage(ctx, manager, f.tenant, p.ID, img.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeletePerson(ctx, manager, f.tenant, p.ID); err != nil {
		t.Fatal(err)
	}
	if got := f.scanInt(`SELECT count(*) FROM face_blob_purge`); got != 2 {
		t.Fatalf("queued blobs = %d, want 2", got)
	}

	// A failing object store keeps the entries for the next pass.
	blobs.fail = true
	if err := svc.PurgeBlobs(ctx, 10); err != nil {
		t.Fatal(err)
	}
	if got := f.scanInt(`SELECT count(*) FROM face_blob_purge`); got != 2 {
		t.Fatalf("queued blobs after failed purge = %d, want 2", got)
	}
	blobs.fail = false
	if err := svc.PurgeBlobs(ctx, 10); err != nil {
		t.Fatal(err)
	}
	if f.blobs.keys() != 0 || f.scanInt(`SELECT count(*) FROM face_blob_purge`) != 0 {
		t.Fatalf("blobs=%d queue=%d after purge, want 0/0", f.blobs.keys(), f.scanInt(`SELECT count(*) FROM face_blob_purge`))
	}
	if blobs.err != nil {
		t.Fatalf("blob delete saw a done context: %v", blobs.err)
	}
}

func TestConcurrentCreatesResolveFrigateNameCollisions(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	manager := f.user(f.tenant, "mgr", authz.FacesManage, authz.FacesView)
	const n = 10
	var wg sync.WaitGroup
	errs := make([]error, n)
	names := make([]string, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// All normalize to "ana_gomez" but are distinct display names.
			p, err := f.svc.CreatePerson(ctx, manager, f.tenant, faces.PersonInput{DisplayName: fmt.Sprintf("Ana Gomez%s", string(rune('!'+i)))})
			errs[i], names[i] = err, p.FrigateName
		}()
	}
	wg.Wait()
	seen := map[string]bool{}
	for i := range errs {
		if errs[i] != nil {
			t.Fatalf("create %d: %v", i, errs[i])
		}
		if seen[names[i]] {
			t.Fatalf("duplicate frigate name %q", names[i])
		}
		seen[names[i]] = true
	}
}
