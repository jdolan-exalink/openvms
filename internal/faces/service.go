// Package faces is the central per-tenant face registry: persons and their enrollment images.
// Image bytes live in the object store, only metadata in Postgres. Biometric data is sensitive:
// faces.view reveals names and images, faces.manage enrolls, edits and deletes, every mutation
// and every image read is audited, and audit details carry identifiers only (never names,
// notes or image bytes). Syncing the registry to Frigate servers is a separate worker (FACE-02).
package faces

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/jdolan-exalink/openvms/internal/access"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/platform/httpx"
	"github.com/jdolan-exalink/openvms/internal/platform/logging"
	"github.com/jdolan-exalink/openvms/internal/platform/objectstore"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

const (
	maxNameAttempts = 8
	// cleanupTimeout bounds detached cleanup work (metadata rollback, blob deletion) that must
	// finish even when the request context is already canceled.
	cleanupTimeout = 15 * time.Second
)

// Audit actions (PRD §66 naming convention).
const (
	ActionPersonCreated = "FACE_PERSON_CREATED"
	ActionPersonUpdated = "FACE_PERSON_UPDATED"
	ActionPersonDeleted = "FACE_PERSON_DELETED"
	ActionImageAdded    = "FACE_IMAGE_ADDED"
	ActionImageRemoved  = "FACE_IMAGE_REMOVED"
	ActionImageRead     = "FACE_IMAGE_READ"
	// ActionSyncRetried records a manual sync retry (FACE-02).
	ActionSyncRetried = "FACE_SYNC_RETRIED"
	// ActionPersonRead records a read of person data (list, detail, image list). Details hold
	// counts and ids only.
	ActionPersonRead = "FACE_PERSON_READ"
	// ActionRecognitionsRead records a read of the recognition timeline.
	ActionRecognitionsRead = "FACE_RECOGNITIONS_READ"
	// ActionTrainRead records a read of Frigate's unclassified attempts (list or image).
	ActionTrainRead = "FACE_TRAIN_READ"
	// ActionTrainAssigned records an attempt becoming an enrollment image.
	ActionTrainAssigned = "FACE_TRAIN_ASSIGNED"
	// ActionSightingIngested records a background Frigate attempt entering the durable review inbox.
	ActionSightingIngested = "FACE_SIGHTING_INGESTED"
)

// Blobs stores enrollment image bytes; *objectstore.Store implements it.
type Blobs interface {
	Put(ctx context.Context, key string, body []byte, contentType string) error
	Get(ctx context.Context, key string) ([]byte, string, error)
	Delete(ctx context.Context, key string) error
}

type Service struct {
	Store *store.Store
	Blobs Blobs
	Log   *slog.Logger
	// Adapters reaches the Frigate servers for the train-from-detections feature; nil disables it.
	Adapters AdapterSource
}

// Person is one registered person. FrigateName is the stable identifier used in every Frigate
// face library; it never changes after creation.
type Person struct {
	ID          uuid.UUID
	TenantID    uuid.UUID
	DisplayName string
	FrigateName string
	Notes       string
	ImageCount  int
	// Sync folds the per-server sync jobs of the person into counters (list and detail reads).
	Sync SyncSummary
	// LastRecognition is the newest sighting on a camera the actor may view (list reads only).
	LastRecognition *LastRecognition
	// FirstImageID is the oldest enrollment image, the card thumbnail fallback (list reads only).
	FirstImageID *uuid.UUID
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// LastRecognition points at the event of a person's most recent sighting.
type LastRecognition struct {
	EventID      uuid.UUID
	StartTime    time.Time
	HasThumbnail bool
}

// SyncSummary counts a person's per-server sync jobs by state. Pending includes jobs being
// processed.
type SyncSummary struct {
	Total, Synced, Pending, Failed, Conflict, Disabled int
}

func (s *SyncSummary) add(status string, n int) {
	s.Total += n
	switch status {
	case SyncSynced:
		s.Synced += n
	case SyncFailed:
		s.Failed += n
	case SyncConflict:
		s.Conflict += n
	case SyncDisabled:
		s.Disabled += n
	default:
		s.Pending += n
	}
}

// Image is the metadata of one enrollment image (bytes are read with ReadImage).
type Image struct {
	ID          uuid.UUID
	PersonID    uuid.UUID
	ContentType string
	SizeBytes   int
	SHA256      string
	Width       int
	Height      int
	CreatedAt   time.Time
}

type PersonInput struct {
	DisplayName string
	Notes       string
}

// PersonUpdate is a partial update: nil fields keep the current value.
type PersonUpdate struct {
	DisplayName *string
	Notes       *string
}

func toPerson(p db.FacePerson, images int) Person {
	return Person{ID: p.ID, TenantID: p.TenantID, DisplayName: p.DisplayName, FrigateName: p.FrigateName, Notes: p.Notes,
		ImageCount: images, CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt}
}

func toImage(i db.FacePersonImage) Image {
	img := Image{ID: i.ID, PersonID: i.PersonID, ContentType: i.ContentType, SizeBytes: int(i.SizeBytes), SHA256: i.Sha256, CreatedAt: i.CreatedAt}
	if i.Width != nil {
		img.Width = int(*i.Width)
	}
	if i.Height != nil {
		img.Height = int(*i.Height)
	}
	return img
}

func imageKey(tenantID, personID, imageID uuid.UUID) string {
	return "tenant/" + tenantID.String() + "/faces/" + personID.String() + "/" + imageID.String()
}

// authorizeTenant answers store.ErrNotFound for another tenant (never leak that it exists).
func authorizeTenant(actor authz.Actor, tenantID uuid.UUID) error {
	if !actor.IsPlatform() && *actor.TenantID != tenantID {
		return store.ErrNotFound
	}
	return nil
}

func (s *Service) tx(ctx context.Context, actor authz.Actor, tenantID uuid.UUID, perm authz.Permission, fn func(q *db.Queries) error) error {
	if err := authorizeTenant(actor, tenantID); err != nil {
		return err
	}
	return s.Store.Tx(ctx, store.ScopeFor(actor), func(q *db.Queries) error {
		c, err := access.Load(ctx, q, actor)
		if err != nil {
			return err
		}
		if err := c.Require(perm, access.Tenant(tenantID)); err != nil {
			return err
		}
		return fn(q)
	})
}

func (s *Service) audit(ctx context.Context, q *db.Queries, actor authz.Actor, tenantID uuid.UUID, action, targetType string, targetID uuid.UUID, details map[string]any) error {
	b, err := json.Marshal(details)
	if err != nil {
		return err
	}
	var target *uuid.UUID
	if targetID != uuid.Nil {
		target = &targetID
	}
	return q.InsertAudit(ctx, db.InsertAuditParams{
		TenantID: &tenantID, ActorID: &actor.UserID, ActorName: actor.Username, Action: action,
		TargetType: targetType, TargetID: target, RequestID: logging.RequestID(ctx), Ip: httpx.ClientIP(ctx), Details: b,
	})
}

// personLockKey is the advisory-lock key serializing everything that changes or syncs one
// person: registry writers take it before their row locks, the sync worker holds it while it
// reads desired state and writes to Frigate, so a stale revision can never overwrite a newer one.
func personLockKey(personID uuid.UUID) string { return "face:" + personID.String() }

func lockPerson(ctx context.Context, q *db.Queries, personID uuid.UUID) error {
	return q.LockFacePersonSync(ctx, personLockKey(personID))
}

// bumpRevision records a new desired state for the person and makes every non-disabled
// per-server job due again. Callers hold the person lock.
func bumpRevision(ctx context.Context, q *db.Queries, tenantID, personID uuid.UUID) error {
	rev, err := q.BumpFacePersonRevision(ctx, db.BumpFacePersonRevisionParams{TenantID: tenantID, ID: personID})
	if err != nil {
		return store.Classify(err)
	}
	return q.PropagateFaceRevision(ctx, db.PropagateFaceRevisionParams{TenantID: tenantID, PersonID: personID, DesiredRevision: rev})
}

func cleanName(name string) (string, error) {
	n := strings.TrimSpace(name)
	switch {
	case n == "":
		return "", invalid("display name is required")
	case utf8.RuneCountInString(n) > MaxDisplayNameLen:
		return "", invalid("display name is too long (max %d characters)", MaxDisplayNameLen)
	}
	return n, nil
}

func cleanNotes(notes string) (string, error) {
	n := strings.TrimSpace(notes)
	if utf8.RuneCountInString(n) > MaxNotesLen {
		return "", invalid("notes are too long (max %d characters)", MaxNotesLen)
	}
	return n, nil
}

// CreatePerson registers a person (faces.manage). The Frigate identifier is derived from the
// display name once and made unique per tenant.
func (s *Service) CreatePerson(ctx context.Context, actor authz.Actor, tenantID uuid.UUID, in PersonInput) (Person, error) {
	name, err := cleanName(in.DisplayName)
	if err != nil {
		return Person{}, err
	}
	notes, err := cleanNotes(in.Notes)
	if err != nil {
		return Person{}, err
	}
	var out Person
	err = s.tx(ctx, actor, tenantID, authz.FacesManage, func(q *db.Queries) error {
		// The unique (tenant, frigate_name) index is the arbiter: a taken name yields no row and
		// the insert is retried with a random suffix, so concurrent creates cannot collide.
		base := NormalizeFrigateName(name)
		frigateName := base
		for attempt := 0; ; attempt++ {
			row, err := q.CreateFacePersonIfNameFree(ctx, db.CreateFacePersonIfNameFreeParams{
				TenantID: tenantID, DisplayName: name, FrigateName: frigateName, Notes: notes, CreatedBy: &actor.UserID,
			})
			if err == nil {
				// A new person owns the name again: recognitions must be ingested normally.
				if err := q.ClearFaceErasedName(ctx, db.ClearFaceErasedNameParams{TenantID: tenantID, Name: frigateName}); err != nil {
					return err
				}
				out = toPerson(row, 0)
				return s.audit(ctx, q, actor, tenantID, ActionPersonCreated, "face_person", row.ID, map[string]any{"person_id": row.ID})
			}
			if !errors.Is(err, pgx.ErrNoRows) {
				return store.Classify(err)
			}
			if attempt >= maxNameAttempts {
				return fmt.Errorf("could not allocate a unique Frigate name after %d attempts", attempt+1)
			}
			frigateName = base + "_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:6]
		}
	})
	return out, err
}

// ListPersons lists the tenant's persons (faces.view).
func (s *Service) ListPersons(ctx context.Context, actor authz.Actor, tenantID uuid.UUID) ([]Person, error) {
	out := []Person{}
	err := s.tx(ctx, actor, tenantID, authz.FacesView, func(q *db.Queries) error {
		rows, err := q.ListFacePersons(ctx, tenantID)
		if err != nil {
			return err
		}
		sums, err := q.ListFaceSyncSummary(ctx, tenantID)
		if err != nil {
			return err
		}
		byPerson := map[uuid.UUID]*SyncSummary{}
		for _, r := range sums {
			if byPerson[r.PersonID] == nil {
				byPerson[r.PersonID] = &SyncSummary{}
			}
			byPerson[r.PersonID].add(r.Status, int(r.N))
		}
		// Latest sighting per person, only on cameras the actor may view (as the recognitions do).
		c, err := access.Load(ctx, q, actor)
		if err != nil {
			return err
		}
		cams, err := c.CameraIDs(ctx, authz.EventsView)
		if err != nil {
			return err
		}
		last := map[uuid.UUID]*LastRecognition{}
		if len(cams) > 0 {
			recs, err := q.ListFacePersonLastRecognitions(ctx, db.ListFacePersonLastRecognitionsParams{TenantID: tenantID, CameraIds: cams})
			if err != nil {
				return err
			}
			for _, r := range recs {
				last[r.PersonID] = &LastRecognition{EventID: r.EventID, StartTime: r.StartTime, HasThumbnail: r.HasThumbnail}
			}
		}
		for _, r := range rows {
			p := Person{ID: r.ID, TenantID: r.TenantID, DisplayName: r.DisplayName, FrigateName: r.FrigateName,
				Notes: r.Notes, ImageCount: int(r.ImageCount), LastRecognition: last[r.ID], CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt}
			if r.FirstImageID != uuid.Nil {
				id := r.FirstImageID
				p.FirstImageID = &id
			}
			if sum := byPerson[r.ID]; sum != nil {
				p.Sync = *sum
			}
			out = append(out, p)
		}
		return s.audit(ctx, q, actor, tenantID, ActionPersonRead, "face_person", uuid.Nil, map[string]any{"scope": "list", "count": len(out)})
	})
	return out, err
}

// GetPerson reads one person (faces.view).
func (s *Service) GetPerson(ctx context.Context, actor authz.Actor, tenantID, id uuid.UUID) (Person, error) {
	var out Person
	err := s.tx(ctx, actor, tenantID, authz.FacesView, func(q *db.Queries) error {
		row, err := q.GetFacePerson(ctx, db.GetFacePersonParams{TenantID: tenantID, ID: id})
		if err != nil {
			return store.Classify(err)
		}
		n, err := q.CountFacePersonImages(ctx, db.CountFacePersonImagesParams{TenantID: tenantID, PersonID: id})
		if err != nil {
			return err
		}
		out = toPerson(row, int(n))
		sums, err := q.ListFaceSyncSummary(ctx, tenantID)
		if err != nil {
			return err
		}
		for _, r := range sums {
			if r.PersonID == id {
				out.Sync.add(r.Status, int(r.N))
			}
		}
		return s.audit(ctx, q, actor, tenantID, ActionPersonRead, "face_person", id, map[string]any{"scope": "detail", "person_id": id})
	})
	return out, err
}

// UpdatePerson renames or edits notes (faces.manage). FrigateName never changes.
func (s *Service) UpdatePerson(ctx context.Context, actor authz.Actor, tenantID, id uuid.UUID, in PersonUpdate) (Person, error) {
	var name, notes *string
	if in.DisplayName != nil {
		n, err := cleanName(*in.DisplayName)
		if err != nil {
			return Person{}, err
		}
		name = &n
	}
	if in.Notes != nil {
		n, err := cleanNotes(*in.Notes)
		if err != nil {
			return Person{}, err
		}
		notes = &n
	}
	var out Person
	err := s.tx(ctx, actor, tenantID, authz.FacesManage, func(q *db.Queries) error {
		cur, err := q.GetFacePerson(ctx, db.GetFacePersonParams{TenantID: tenantID, ID: id})
		if err != nil {
			return store.Classify(err)
		}
		next := db.UpdateFacePersonParams{TenantID: tenantID, ID: id, DisplayName: cur.DisplayName, Notes: cur.Notes, UpdatedBy: &actor.UserID}
		changed := []string{}
		if name != nil && *name != cur.DisplayName {
			next.DisplayName = *name
			changed = append(changed, "display_name")
		}
		if notes != nil && *notes != cur.Notes {
			next.Notes = *notes
			changed = append(changed, "notes")
		}
		row, err := q.UpdateFacePerson(ctx, next)
		if err != nil {
			return store.Classify(err)
		}
		n, err := q.CountFacePersonImages(ctx, db.CountFacePersonImagesParams{TenantID: tenantID, PersonID: id})
		if err != nil {
			return err
		}
		out = toPerson(row, int(n))
		return s.audit(ctx, q, actor, tenantID, ActionPersonUpdated, "face_person", id, map[string]any{"person_id": id, "changed": changed})
	})
	return out, err
}

// DeletePerson removes a person and every central image (faces.manage). The person row stays
// as a tombstone so the registry sync can propagate the deletion to every Frigate.
func (s *Service) DeletePerson(ctx context.Context, actor authz.Actor, tenantID, id uuid.UUID) error {
	err := s.tx(ctx, actor, tenantID, authz.FacesManage, func(q *db.Queries) error {
		if err := lockPerson(ctx, q, id); err != nil {
			return err
		}
		if _, err := q.SoftDeleteFacePerson(ctx, db.SoftDeleteFacePersonParams{TenantID: tenantID, ID: id, UpdatedBy: &actor.UserID}); err != nil {
			return store.Classify(err)
		}
		imgs, err := q.ListFacePersonImages(ctx, db.ListFacePersonImagesParams{TenantID: tenantID, PersonID: id})
		if err != nil {
			return err
		}
		for _, i := range imgs {
			if err := q.EnqueueFaceBlobPurge(ctx, db.EnqueueFaceBlobPurgeParams{StorageKey: i.StorageKey, TenantID: tenantID}); err != nil {
				return err
			}
		}
		if err := q.DeleteFacePersonImages(ctx, db.DeleteFacePersonImagesParams{TenantID: tenantID, PersonID: id}); err != nil {
			return err
		}
		// A tombstone must not stay a member of any watchlist.
		if err := q.RemoveFacePersonFromWatchlists(ctx, db.RemoveFacePersonFromWatchlistsParams{TenantID: tenantID, PersonID: id}); err != nil {
			return err
		}
		if err := bumpRevision(ctx, q, tenantID, id); err != nil {
			return err
		}
		return s.audit(ctx, q, actor, tenantID, ActionPersonDeleted, "face_person", id, map[string]any{"person_id": id, "images_removed": len(imgs)})
	})
	return err
}

// PurgeBlobs deletes queued object-store bytes (at most limit per call). Blob deletion is
// deliberately not part of any request: keys are queued in the transaction that drops the image
// row and removed here, with each delete bounded by its own detached timeout, so a canceled
// request or a down object store never strands bytes. Failed keys stay queued for the next pass.
func (s *Service) PurgeBlobs(ctx context.Context, limit int) error {
	if limit < 1 {
		return nil
	}
	var rows []db.FaceBlobPurge
	if err := s.Store.Tx(ctx, store.AllTenants, func(q *db.Queries) error {
		var err error
		rows, err = q.ListFaceBlobPurge(ctx, int32(min(limit, 1000))) //nolint:gosec // bounded above
		return err
	}); err != nil {
		return err
	}
	for _, r := range rows {
		dctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cleanupTimeout)
		derr := s.Blobs.Delete(dctx, r.StorageKey)
		cancel()
		if derr != nil && !errors.Is(derr, store.ErrNotFound) {
			if s.Log != nil {
				s.Log.WarnContext(ctx, "faces: delete image blob failed", "key", r.StorageKey, "error", derr)
			}
			if err := s.Store.Tx(ctx, store.AllTenants, func(q *db.Queries) error {
				return q.BumpFaceBlobPurgeAttempts(ctx, r.StorageKey)
			}); err != nil {
				return err
			}
			continue
		}
		if err := s.Store.Tx(ctx, store.AllTenants, func(q *db.Queries) error {
			return q.DeleteFaceBlobPurge(ctx, r.StorageKey)
		}); err != nil {
			return err
		}
	}
	return nil
}

// ListImages lists a person's enrollment images (faces.view).
func (s *Service) ListImages(ctx context.Context, actor authz.Actor, tenantID, personID uuid.UUID) ([]Image, error) {
	out := []Image{}
	err := s.tx(ctx, actor, tenantID, authz.FacesView, func(q *db.Queries) error {
		if _, err := q.GetFacePerson(ctx, db.GetFacePersonParams{TenantID: tenantID, ID: personID}); err != nil {
			return store.Classify(err)
		}
		rows, err := q.ListFacePersonImages(ctx, db.ListFacePersonImagesParams{TenantID: tenantID, PersonID: personID})
		if err != nil {
			return err
		}
		for _, r := range rows {
			out = append(out, toImage(r))
		}
		return s.audit(ctx, q, actor, tenantID, ActionPersonRead, "face_person", personID,
			map[string]any{"scope": "images", "person_id": personID, "count": len(out)})
	})
	return out, err
}

// AddImage validates and stores one enrollment image (faces.manage). Metadata commits first;
// the bytes are written to the object store afterwards, and a failed write removes the
// metadata again so no row ever points at nothing.
func (s *Service) AddImage(ctx context.Context, actor authz.Actor, tenantID, personID uuid.UUID, data []byte, contentType string) (Image, error) {
	meta, err := validateImage(data, contentType)
	if err != nil {
		return Image{}, err
	}
	imageID := uuid.New()
	key := imageKey(tenantID, personID, imageID)
	var out Image
	err = s.tx(ctx, actor, tenantID, authz.FacesManage, func(q *db.Queries) error {
		if err := lockPerson(ctx, q, personID); err != nil {
			return err
		}
		// The row lock serializes concurrent adds for this person, so the cap check below
		// cannot be raced past by parallel uploads.
		if _, err := q.LockFacePersonForUpdate(ctx, db.LockFacePersonForUpdateParams{TenantID: tenantID, ID: personID}); err != nil {
			return store.Classify(err)
		}
		n, err := q.CountFacePersonImages(ctx, db.CountFacePersonImagesParams{TenantID: tenantID, PersonID: personID})
		if err != nil {
			return err
		}
		if int(n) >= MaxImagesPerPerson {
			return invalid("a person can have at most %d images", MaxImagesPerPerson)
		}
		w, h := int32(meta.Width), int32(meta.Height) //nolint:gosec // bounded by MaxImagePixels
		row, err := q.InsertFacePersonImage(ctx, db.InsertFacePersonImageParams{
			ID: imageID, TenantID: tenantID, PersonID: personID, StorageKey: key, ContentType: meta.ContentType,
			SizeBytes: int32(meta.Size), //nolint:gosec // bounded by MaxImageBytes
			Sha256:    meta.SHA256, Width: &w, Height: &h, CreatedBy: &actor.UserID,
		})
		if err != nil {
			return store.Classify(err)
		}
		out = toImage(row)
		if err := bumpRevision(ctx, q, tenantID, personID); err != nil {
			return err
		}
		return s.audit(ctx, q, actor, tenantID, ActionImageAdded, "face_person_image", imageID,
			map[string]any{"person_id": personID, "image_id": imageID, "sha256": meta.SHA256, "size_bytes": meta.Size})
	})
	if err != nil {
		return Image{}, err
	}
	if err := s.Blobs.Put(ctx, key, data, meta.ContentType); err != nil {
		s.rollbackImage(ctx, actor, tenantID, personID, imageID, key)
		return Image{}, fmt.Errorf("store face image: %w", err)
	}
	return out, nil
}

// rollbackImage removes the metadata of an image whose bytes could not be stored. It runs on a
// detached, bounded context: the failure that triggers it is often the request context itself
// being canceled, and a canceled rollback would leave a row pointing at nothing. The key is
// also queued for blob deletion in case the store kept a partial write.
func (s *Service) rollbackImage(ctx context.Context, actor authz.Actor, tenantID, personID, imageID uuid.UUID, key string) {
	rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cleanupTimeout)
	defer cancel()
	err := s.Store.Tx(rctx, store.ScopeFor(actor), func(q *db.Queries) error {
		if err := q.DeleteFacePersonImage(rctx, db.DeleteFacePersonImageParams{TenantID: tenantID, PersonID: personID, ID: imageID}); err != nil {
			return err
		}
		if err := q.EnqueueFaceBlobPurge(rctx, db.EnqueueFaceBlobPurgeParams{StorageKey: key, TenantID: tenantID}); err != nil {
			return err
		}
		return s.audit(rctx, q, actor, tenantID, ActionImageRemoved, "face_person_image", imageID,
			map[string]any{"person_id": personID, "image_id": imageID, "reason": "storage_failed"})
	})
	if err != nil && s.Log != nil {
		s.Log.ErrorContext(rctx, "faces: rollback of image metadata failed", "image_id", imageID, "error", err)
	}
}

// RemoveImage deletes one enrollment image (faces.manage).
func (s *Service) RemoveImage(ctx context.Context, actor authz.Actor, tenantID, personID, imageID uuid.UUID) error {
	return s.tx(ctx, actor, tenantID, authz.FacesManage, func(q *db.Queries) error {
		if err := lockPerson(ctx, q, personID); err != nil {
			return err
		}
		row, err := q.GetFacePersonImage(ctx, db.GetFacePersonImageParams{TenantID: tenantID, PersonID: personID, ID: imageID})
		if err != nil {
			return store.Classify(err)
		}
		if err := q.EnqueueFaceBlobPurge(ctx, db.EnqueueFaceBlobPurgeParams{StorageKey: row.StorageKey, TenantID: tenantID}); err != nil {
			return err
		}
		if err := q.DeleteFacePersonImage(ctx, db.DeleteFacePersonImageParams{TenantID: tenantID, PersonID: personID, ID: imageID}); err != nil {
			return err
		}
		if err := bumpRevision(ctx, q, tenantID, personID); err != nil {
			return err
		}
		return s.audit(ctx, q, actor, tenantID, ActionImageRemoved, "face_person_image", imageID,
			map[string]any{"person_id": personID, "image_id": imageID})
	})
}

// SyncStatus is the per-server sync state of one person, for the Faces UI.
type SyncStatus struct {
	ServerID        uuid.UUID
	ServerName      string
	Status          string // pending, processing, synced, failed, conflict, disabled
	DesiredRevision int64
	SyncedRevision  int64
	Owned           bool
	LastError       string
	LastSyncedAt    *time.Time
	UpdatedAt       time.Time
}

// ListSyncStatus reports how the person is synced on every server of the tenant (faces.view).
func (s *Service) ListSyncStatus(ctx context.Context, actor authz.Actor, tenantID, personID uuid.UUID) ([]SyncStatus, error) {
	out := []SyncStatus{}
	err := s.tx(ctx, actor, tenantID, authz.FacesView, func(q *db.Queries) error {
		if _, err := q.GetFacePerson(ctx, db.GetFacePersonParams{TenantID: tenantID, ID: personID}); err != nil {
			return store.Classify(err)
		}
		rows, err := q.ListFaceSyncStatus(ctx, db.ListFaceSyncStatusParams{TenantID: tenantID, PersonID: personID})
		if err != nil {
			return err
		}
		for _, r := range rows {
			st := SyncStatus{ServerID: r.ServerID, ServerName: r.ServerName, Status: r.Status, DesiredRevision: r.DesiredRevision,
				SyncedRevision: r.SyncedRevision, Owned: r.Owned, LastSyncedAt: r.LastSyncedAt, UpdatedAt: r.UpdatedAt}
			if r.LastError != nil {
				st.LastError = *r.LastError
			}
			out = append(out, st)
		}
		return nil
	})
	return out, err
}

// RetrySync requeues the person's failed and conflicting server jobs (faces.manage) and
// returns how many were requeued. A conflict is re-evaluated from scratch: it clears once the
// foreign face that caused it is gone. Deleted persons are included, so a remote delete that
// exhausted its attempts can still be retried and the tombstone purged.
func (s *Service) RetrySync(ctx context.Context, actor authz.Actor, tenantID, personID uuid.UUID) (int, error) {
	var n int64
	err := s.tx(ctx, actor, tenantID, authz.FacesManage, func(q *db.Queries) error {
		if err := lockPerson(ctx, q, personID); err != nil {
			return err
		}
		if _, err := q.GetFacePersonAny(ctx, db.GetFacePersonAnyParams{TenantID: tenantID, ID: personID}); err != nil {
			return store.Classify(err)
		}
		var err error
		if n, err = q.RetryFaceServerSync(ctx, db.RetryFaceServerSyncParams{TenantID: tenantID, PersonID: personID}); err != nil {
			return err
		}
		return s.audit(ctx, q, actor, tenantID, ActionSyncRetried, "face_person", personID, map[string]any{"person_id": personID, "jobs": n})
	})
	return int(n), err
}

// ReadImage returns the image bytes (faces.view) and audits the read.
func (s *Service) ReadImage(ctx context.Context, actor authz.Actor, tenantID, personID, imageID uuid.UUID) ([]byte, string, error) {
	var key string
	err := s.tx(ctx, actor, tenantID, authz.FacesView, func(q *db.Queries) error {
		row, err := q.GetFacePersonImage(ctx, db.GetFacePersonImageParams{TenantID: tenantID, PersonID: personID, ID: imageID})
		if err != nil {
			return store.Classify(err)
		}
		key = row.StorageKey
		return s.audit(ctx, q, actor, tenantID, ActionImageRead, "face_person_image", imageID,
			map[string]any{"person_id": personID, "image_id": imageID})
	})
	if err != nil {
		return nil, "", err
	}
	data, ct, err := s.Blobs.Get(ctx, key)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, "", err
		}
		return nil, "", fmt.Errorf("read face image: %w", err)
	}
	return data, ct, nil
}

var _ Blobs = (*objectstore.Store)(nil)
