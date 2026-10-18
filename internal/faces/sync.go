package faces

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/jdolan-exalink/openvms/internal/frigate"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

// Sync status values (face_server_sync.status).
const (
	SyncPending    = "pending"
	SyncProcessing = "processing"
	SyncSynced     = "synced"
	SyncFailed     = "failed"
	SyncConflict   = "conflict"
	SyncDisabled   = "disabled"
)

// Audit actions written by the sync worker (actor: system).
const (
	ActionSubLabelScrubbed = "FACE_SUBLABEL_SCRUBBED"
	ActionPersonPurged     = "FACE_PERSON_PURGED"
	systemActor            = "system:faces-sync"
)

const (
	syncLeaseSeconds = 120
	syncMaxHard      = 8
	maxBackoff       = 15 * time.Minute
	disabledBackoff  = 10 * time.Minute
	scrubBatch       = 1000
	// emptyFolderProbeID is a deliberately non-existent image id. Frigate removes a face folder
	// once a delete leaves it empty, which is the only way to drop an empty folder through its API.
	emptyFolderProbeID = "ovms-empty-folder-cleanup.webp"
)

// AdapterSource hands out connected Frigate adapters; *inventory.Adapters implements it.
type AdapterSource interface {
	Get(ctx context.Context, srv db.FrigateServer) (frigate.Adapter, error)
}

// Syncer converges every Frigate face library to the central registry. Desired state is
// versioned (face_persons.sync_revision), work is one durable job per (server, person), jobs are
// claimed with leases and SKIP LOCKED, errors back off with a cap, and every remote write
// happens under the person's advisory lock so a stale revision cannot overwrite a newer one.
// Credentials are the server's own: authorization was enforced when the registry changed.
//
// Ownership: only faces the sync created (or adopted while empty) are modified or deleted. A
// pre-existing face with the same name and images is a conflict and is left untouched.
type Syncer struct {
	Store    *store.Store
	Blobs    Blobs
	Adapters AdapterSource
	Log      *slog.Logger
}

// RunOnce is one worker pass: enqueue missing jobs, process a bounded batch, then purge
// converged tombstones.
func (y *Syncer) RunOnce(ctx context.Context, limit int) error {
	if err := y.Enqueue(ctx); err != nil {
		return err
	}
	jobs, err := y.Claim(ctx, limit)
	if err != nil {
		return err
	}
	var first error
	for _, j := range jobs {
		if err := y.Process(ctx, j); err != nil && first == nil {
			first = err
		}
	}
	if err := y.PurgeTombstones(ctx, limit); err != nil && first == nil {
		first = err
	}
	if err := y.RunRemoteCleanups(ctx, limit); err != nil && first == nil {
		first = err
	}
	return first
}

// Enqueue creates the missing (server, person) jobs for live persons (new persons, new
// servers) and reconciles the disabled/pending split with the servers' reported capability.
func (y *Syncer) Enqueue(ctx context.Context) error {
	return y.Store.Tx(ctx, store.AllTenants, func(q *db.Queries) error {
		if _, err := q.EnqueueFaceSyncRows(ctx); err != nil {
			return err
		}
		if _, err := q.DisableFaceSyncWithoutCapability(ctx); err != nil {
			return err
		}
		_, err := q.EnableFaceSyncWithCapability(ctx)
		return err
	})
}

// Claim leases up to limit due jobs. A job whose lease expired (worker crash) is claimed again.
func (y *Syncer) Claim(ctx context.Context, limit int) ([]db.FaceServerSync, error) {
	if limit < 1 {
		return nil, nil
	}
	var jobs []db.FaceServerSync
	err := y.Store.Tx(ctx, store.AllTenants, func(q *db.Queries) error {
		var err error
		jobs, err = q.ClaimFaceSyncJobs(ctx, db.ClaimFaceSyncJobsParams{LeaseSeconds: syncLeaseSeconds, MaxRows: int32(min(limit, 1000))}) //nolint:gosec // bounded
		return err
	})
	return jobs, err
}

// Process converges one claimed job. It never returns a remote error: those are recorded on the
// job (retry, disabled, conflict or failed). Only database errors and shutdown propagate.
func (y *Syncer) Process(ctx context.Context, claimed db.FaceServerSync) error {
	return y.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		q := db.New(tx)
		if err := lockPerson(ctx, q, claimed.PersonID); err != nil {
			return err
		}
		job, err := q.GetFaceServerSync(ctx, db.GetFaceServerSyncParams{ServerID: claimed.ServerID, PersonID: claimed.PersonID})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil
			}
			return err
		}
		// A newer revision (or a reset) while the job waited: the fresher job handles it.
		if job.Status != SyncProcessing || job.DesiredRevision != claimed.DesiredRevision {
			return nil
		}
		person, err := q.GetFacePersonAny(ctx, db.GetFacePersonAnyParams{TenantID: job.TenantID, ID: job.PersonID})
		if err != nil {
			return err
		}
		srv, err := q.GetFaceSyncServer(ctx, job.ServerID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil
			}
			return err
		}
		if !faceCapable(srv) {
			return y.finish(ctx, q, job, SyncDisabled, "face recognition is not enabled on this server", time.Now(), false)
		}
		adapter, err := y.Adapters.Get(ctx, srv)
		if err != nil {
			return y.fail(ctx, q, job, err)
		}
		res, err := y.converge(ctx, q, adapter, job, person)
		if err != nil {
			return y.fail(ctx, q, job, err)
		}
		return y.finish(ctx, q, job, res.status, res.message, time.Now(), false)
	})
}

type outcome struct {
	status  string
	message string
}

func synced() outcome { return outcome{status: SyncSynced} }

func faceCapable(srv db.FrigateServer) bool {
	var caps struct {
		FaceRecognition bool `json:"face_recognition"`
	}
	return json.Unmarshal(srv.Capabilities, &caps) == nil && caps.FaceRecognition
}

// markOwned durably records ownership in its own committed transaction BEFORE the remote write
// that creates the face, so a crash or lost response after the write is still recognised as ours.
func (y *Syncer) markOwned(ctx context.Context, job *db.FaceServerSync) error {
	if job.Owned {
		return nil
	}
	if err := y.Store.Tx(ctx, store.AllTenants, func(q *db.Queries) error {
		return q.SetFaceServerOwned(ctx, db.SetFaceServerOwnedParams{ServerID: job.ServerID, PersonID: job.PersonID})
	}); err != nil {
		return err
	}
	job.Owned = true
	return nil
}

// permanentErr marks a failure retries cannot fix on their own (counted toward 'failed').
type permanentErr struct{ msg string }

func (e *permanentErr) Error() string { return e.msg }

func (y *Syncer) converge(ctx context.Context, q *db.Queries, a frigate.Adapter, job db.FaceServerSync, person db.FacePerson) (outcome, error) {
	lib, err := a.ListFaces(ctx)
	if err != nil {
		return outcome{}, err
	}
	name := person.FrigateName
	observed, exists := lib.Faces[name]

	if person.DeletedAt != nil {
		return y.convergeDelete(ctx, q, a, job, name, observed, exists)
	}

	if !exists {
		if err := y.markOwned(ctx, &job); err != nil {
			return outcome{}, err
		}
		if err := a.CreateFace(ctx, name); err != nil {
			return outcome{}, err
		}
		observed = nil
	} else if !job.Owned {
		if len(observed) > 0 {
			return outcome{status: SyncConflict, message: "a face with this name already exists in Frigate and is not managed by OpenVMS"}, nil
		}
		if err := y.markOwned(ctx, &job); err != nil {
			return outcome{}, err
		}
	}

	images, err := q.ListFacePersonImages(ctx, db.ListFacePersonImagesParams{TenantID: job.TenantID, PersonID: job.PersonID})
	if err != nil {
		return outcome{}, err
	}
	maps, err := q.ListFaceServerImages(ctx, db.ListFaceServerImagesParams{ServerID: job.ServerID, PersonID: job.PersonID})
	if err != nil {
		return outcome{}, err
	}
	seen := make(map[string]bool, len(observed))
	for _, id := range observed {
		seen[id] = true
	}
	central := make(map[uuid.UUID]bool, len(images))
	for _, im := range images {
		central[im.ID] = true
	}
	mapped := map[uuid.UUID]string{}
	for _, m := range maps {
		if !seen[m.FrigateImageID] || !central[m.ImageID] {
			// The image vanished remotely (or centrally): forget it so it is re-registered.
			if err := q.DeleteFaceServerImage(ctx, db.DeleteFaceServerImageParams{ServerID: job.ServerID, ImageID: m.ImageID}); err != nil {
				return outcome{}, err
			}
			continue
		}
		mapped[m.ImageID] = m.FrigateImageID
	}

	// Register what is missing, one image at a time, learning Frigate's id by diffing the
	// library (Frigate re-encodes and renames uploads, so the id is only known afterwards).
	for _, im := range images {
		if _, ok := mapped[im.ID]; ok {
			continue
		}
		data, _, err := y.Blobs.Get(ctx, im.StorageKey)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return outcome{}, &permanentErr{msg: "central image bytes are not available"}
			}
			return outcome{}, err
		}
		if err := a.RegisterFaceImage(ctx, name, im.ID.String()+extFor(im.ContentType), data); err != nil {
			return outcome{}, err
		}
		after, err := a.ListFaces(ctx)
		if err != nil {
			return outcome{}, err
		}
		var fresh []string
		for _, id := range after.Faces[name] {
			if !seen[id] {
				fresh = append(fresh, id)
			}
		}
		for _, id := range after.Faces[name] {
			seen[id] = true
		}
		if len(fresh) != 1 {
			// Zero or several new ids: cannot attribute the write. The next pass removes the
			// unmapped ids as orphans and registers again, so nothing is duplicated.
			return outcome{}, fmt.Errorf("registered image could not be identified in the library (%d new ids)", len(fresh))
		}
		if err := q.InsertFaceServerImage(ctx, db.InsertFaceServerImageParams{
			ServerID: job.ServerID, ImageID: im.ID, PersonID: job.PersonID, TenantID: job.TenantID, FrigateImageID: fresh[0],
		}); err != nil {
			return outcome{}, err
		}
		mapped[im.ID] = fresh[0]
	}

	// Anything in an owned face that the sync did not register is drift: remove it.
	keep := make(map[string]bool, len(mapped))
	for _, id := range mapped {
		keep[id] = true
	}
	var orphans []string
	for id := range seen {
		if !keep[id] {
			orphans = append(orphans, id)
		}
	}
	sort.Strings(orphans)
	if len(orphans) > 0 {
		if err := a.DeleteFaceImages(ctx, name, orphans); err != nil {
			return outcome{}, err
		}
	}
	return synced(), nil
}

func (y *Syncer) convergeDelete(ctx context.Context, q *db.Queries, a frigate.Adapter, job db.FaceServerSync, name string, observed []string, exists bool) (outcome, error) {
	// A face this sync never owned is a foreign library entry that happens to share the name.
	if exists && job.Owned {
		ids := observed
		if len(ids) == 0 {
			ids = []string{emptyFolderProbeID}
		}
		if err := a.DeleteFaceImages(ctx, name, ids); err != nil {
			if len(observed) > 0 || errors.Is(err, frigate.ErrFaceRecognitionDisabled) {
				return outcome{}, err
			}
			// An already-empty folder is harmless if Frigate refuses the probe.
		}
	}
	if err := q.DeleteFaceServerImagesForPerson(ctx, db.DeleteFaceServerImagesForPersonParams{ServerID: job.ServerID, PersonID: job.PersonID}); err != nil {
		return outcome{}, err
	}
	return synced(), nil
}

func extFor(contentType string) string {
	switch contentType {
	case "image/png":
		return ".png"
	case "image/webp":
		return ".webp"
	default:
		return ".jpg"
	}
}

// fail records a remote or setup error. Transient failures (unreachable server, 5xx) back off
// without limit; permanent ones count toward 'failed'. The error text stored never carries
// request paths (they contain the Frigate name) or response bodies.
func (y *Syncer) fail(ctx context.Context, q *db.Queries, job db.FaceServerSync, cause error) error {
	if ctx.Err() != nil {
		return ctx.Err() // shutting down: the lease expires and the job is claimed again
	}
	msg, kind := describeSyncError(cause)
	if y.Log != nil {
		y.Log.WarnContext(ctx, "faces: sync attempt failed", "server_id", job.ServerID, "person_id", job.PersonID, "reason", msg)
	}
	switch kind {
	case errDisabled:
		return y.finish(ctx, q, job, SyncDisabled, msg, time.Now().Add(disabledBackoff), false)
	case errTransient:
		return y.finish(ctx, q, job, SyncPending, msg, time.Now().Add(backoff(job.Attempts)), false)
	default:
		if job.HardFailures+1 >= syncMaxHard {
			return y.finish(ctx, q, job, SyncFailed, msg, time.Now(), true)
		}
		return y.finish(ctx, q, job, SyncPending, msg, time.Now().Add(backoff(job.Attempts)), true)
	}
}

func backoff(attempts int32) time.Duration {
	d := time.Duration(1<<min(int(attempts), 10)) * time.Second
	return min(d, maxBackoff)
}

type errKind int

const (
	errTransient errKind = iota
	errPermanent
	errDisabled
)

func describeSyncError(err error) (string, errKind) {
	var perm *permanentErr
	var se *frigate.StatusError
	switch {
	case errors.Is(err, frigate.ErrFaceRecognitionDisabled):
		return "face recognition is disabled in Frigate", errDisabled
	case errors.As(err, &perm):
		return perm.msg, errPermanent
	case errors.Is(err, frigate.ErrUnauthorized):
		return "frigate rejected the sync credentials", errPermanent
	case errors.As(err, &se):
		if se.Status >= 500 || se.Status == http.StatusTooManyRequests {
			return fmt.Sprintf("frigate answered status %d", se.Status), errTransient
		}
		return fmt.Sprintf("frigate rejected the request (status %d)", se.Status), errPermanent
	case errors.Is(err, frigate.ErrNotFound):
		return "frigate does not have the requested resource", errPermanent
	case errors.Is(err, frigate.ErrInvalidFaceName):
		return "the face name is not valid for frigate", errPermanent
	}
	var ne net.Error
	if errors.As(err, &ne) || errors.Is(err, context.DeadlineExceeded) {
		return "frigate is unreachable", errTransient
	}
	return "sync attempt failed", errTransient
}

func (y *Syncer) finish(ctx context.Context, q *db.Queries, job db.FaceServerSync, status, msg string, next time.Time, hard bool) error {
	var cause *string
	if msg != "" {
		cause = &msg
	}
	_, err := q.FinishFaceServerSync(ctx, db.FinishFaceServerSyncParams{
		Status: status, LastError: cause, NextAttemptAt: next, HardFailure: hard,
		ServerID: job.ServerID, PersonID: job.PersonID, DesiredRevision: job.DesiredRevision,
	})
	return err
}

// ErasedNameRetention is how long an erased name keeps being dropped from ingested reviews:
// the longest a review stays in Frigate (its default review retention is 30 days). It must
// match events.ErasedFaceNameRetention.
const ErasedNameRetention = 30 * 24 * time.Hour

// PurgeTombstones finishes the deletion of persons whose Frigate faces are gone everywhere:
// it scrubs the person's name from stored event sub_labels (batched, idempotent, audited per
// batch) and then removes the person row. Image blobs were queued for deletion when the person
// was deleted. Order matters: the row is the memory of what to scrub, so it goes last and a
// crash anywhere simply repeats the idempotent steps.
func (y *Syncer) PurgeTombstones(ctx context.Context, limit int) error {
	if limit < 1 {
		return nil
	}
	if err := y.Store.Tx(ctx, store.AllTenants, func(q *db.Queries) error {
		_, err := q.PruneFaceErasedNames(ctx, ErasedNameRetention.Seconds())
		return err
	}); err != nil {
		return err
	}
	var persons []db.FacePerson
	if err := y.Store.Tx(ctx, store.AllTenants, func(q *db.Queries) error {
		var err error
		persons, err = q.ListPurgeableFaceTombstones(ctx, int32(min(limit, 1000))) //nolint:gosec // bounded
		return err
	}); err != nil {
		return err
	}
	for _, p := range persons {
		if err := y.purgeOne(ctx, p); err != nil {
			return err
		}
	}
	return nil
}

func (y *Syncer) purgeOne(ctx context.Context, p db.FacePerson) error {
	// Before scrubbing, so reviews ingested while the scrub runs (and later ones Frigate still
	// serves) cannot write the name back; see ErasedNameRetention.
	if err := y.Store.Tx(ctx, store.AllTenants, func(q *db.Queries) error {
		return q.RecordFaceErasedName(ctx, db.RecordFaceErasedNameParams{TenantID: p.TenantID, Name: p.FrigateName})
	}); err != nil {
		return err
	}
	total := int64(0)
	for {
		var n int64
		err := y.Store.Tx(ctx, store.AllTenants, func(q *db.Queries) error {
			if err := lockPerson(ctx, q, p.ID); err != nil {
				return err
			}
			var err error
			n, err = q.ScrubEventSubLabel(ctx, db.ScrubEventSubLabelParams{TenantID: p.TenantID, Name: p.FrigateName, MaxRows: scrubBatch})
			if err != nil || n == 0 {
				return err
			}
			return y.auditSystem(ctx, q, p, ActionSubLabelScrubbed, map[string]any{"person_id": p.ID, "events": n})
		})
		if err != nil {
			return err
		}
		total += n
		if n < scrubBatch {
			break
		}
	}
	return y.Store.Tx(ctx, store.AllTenants, func(q *db.Queries) error {
		if err := lockPerson(ctx, q, p.ID); err != nil {
			return err
		}
		// Re-check under the lock: a retry or a late job could have reopened the person.
		ok, err := q.FaceTombstonePurgeable(ctx, db.FaceTombstonePurgeableParams{TenantID: p.TenantID, ID: p.ID})
		if err != nil || !ok {
			return err
		}
		// The row is the memory of what to scrub: never drop it while a stored event still
		// carries the name. The next pass resumes the scrub.
		left, err := q.CountEventsWithSubLabel(ctx, db.CountEventsWithSubLabelParams{TenantID: p.TenantID, Name: p.FrigateName})
		if err != nil || left > 0 {
			return err
		}
		if err := q.RecordFaceRemoteCleanup(ctx, db.RecordFaceRemoteCleanupParams{TenantID: p.TenantID, ID: p.ID, Name: p.FrigateName}); err != nil {
			return err
		}
		if _, err := q.PurgeFacePerson(ctx, db.PurgeFacePersonParams{TenantID: p.TenantID, ID: p.ID}); err != nil {
			return err
		}
		return y.auditSystem(ctx, q, p, ActionPersonPurged, map[string]any{"person_id": p.ID, "events_scrubbed": total})
	})
}

// RunRemoteCleanups deletes faces recorded when a tombstone was purged while its server was
// face-disabled. Remote failures keep the obligation; it is retried on the next pass.
func (y *Syncer) RunRemoteCleanups(ctx context.Context, limit int) error {
	if limit < 1 {
		return nil
	}
	var due []db.FaceRemoteCleanup
	if err := y.Store.Tx(ctx, store.AllTenants, func(q *db.Queries) error {
		var err error
		due, err = q.ListDueFaceRemoteCleanups(ctx, int32(min(limit, 1000))) //nolint:gosec // bounded
		return err
	}); err != nil {
		return err
	}
	for _, c := range due {
		err := y.Store.Tx(ctx, store.AllTenants, func(q *db.Queries) error {
			srv, err := q.GetFaceSyncServer(ctx, c.ServerID)
			if err != nil {
				return err
			}
			if err := y.deleteRemoteFace(ctx, srv, c.FrigateName); err != nil {
				return err
			}
			return q.DeleteFaceRemoteCleanup(ctx, db.DeleteFaceRemoteCleanupParams{ServerID: c.ServerID, FrigateName: c.FrigateName})
		})
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil && y.Log != nil {
			y.Log.WarnContext(ctx, "faces: remote cleanup failed", "server_id", c.ServerID)
		}
	}
	return nil
}

func (y *Syncer) deleteRemoteFace(ctx context.Context, srv db.FrigateServer, name string) error {
	a, err := y.Adapters.Get(ctx, srv)
	if err != nil {
		return err
	}
	lib, err := a.ListFaces(ctx)
	ids, exists := lib.Faces[name]
	if err != nil || !exists {
		return err
	}
	probe := len(ids) == 0 // an empty folder only goes away through a delete; a refusal is harmless
	if probe {
		ids = []string{emptyFolderProbeID}
	}
	if err := a.DeleteFaceImages(ctx, name, ids); err != nil && !probe {
		return err
	}
	return nil
}

func (y *Syncer) auditSystem(ctx context.Context, q *db.Queries, p db.FacePerson, action string, details map[string]any) error {
	b, err := json.Marshal(details)
	if err != nil {
		return err
	}
	tid, pid := p.TenantID, p.ID
	return q.InsertAudit(ctx, db.InsertAuditParams{
		TenantID: &tid, ActorName: systemActor, Action: action, TargetType: "face_person", TargetID: &pid, Details: b,
	})
}
