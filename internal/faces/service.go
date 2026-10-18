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

	"github.com/jdolan-exalink/openvms/internal/access"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/platform/httpx"
	"github.com/jdolan-exalink/openvms/internal/platform/logging"
	"github.com/jdolan-exalink/openvms/internal/platform/objectstore"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

// Audit actions (PRD §66 naming convention).
const (
	ActionPersonCreated = "FACE_PERSON_CREATED"
	ActionPersonUpdated = "FACE_PERSON_UPDATED"
	ActionPersonDeleted = "FACE_PERSON_DELETED"
	ActionImageAdded    = "FACE_IMAGE_ADDED"
	ActionImageRemoved  = "FACE_IMAGE_REMOVED"
	ActionImageRead     = "FACE_IMAGE_READ"
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
	CreatedAt   time.Time
	UpdatedAt   time.Time
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
	return q.InsertAudit(ctx, db.InsertAuditParams{
		TenantID: &tenantID, ActorID: &actor.UserID, ActorName: actor.Username, Action: action,
		TargetType: targetType, TargetID: &targetID, RequestID: logging.RequestID(ctx), Ip: httpx.ClientIP(ctx), Details: b,
	})
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
		frigateName := NormalizeFrigateName(name)
		for attempt := 0; ; attempt++ {
			taken, err := q.FaceFrigateNameTaken(ctx, db.FaceFrigateNameTakenParams{TenantID: tenantID, FrigateName: frigateName})
			if err != nil {
				return err
			}
			if !taken {
				break
			}
			if attempt >= 5 {
				return fmt.Errorf("could not allocate a unique Frigate name for %d attempts", attempt)
			}
			frigateName = NormalizeFrigateName(name) + "_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:6]
		}
		row, err := q.CreateFacePerson(ctx, db.CreateFacePersonParams{
			TenantID: tenantID, DisplayName: name, FrigateName: frigateName, Notes: notes, CreatedBy: &actor.UserID,
		})
		if err != nil {
			return store.Classify(err)
		}
		out = toPerson(row, 0)
		return s.audit(ctx, q, actor, tenantID, ActionPersonCreated, "face_person", row.ID, map[string]any{"person_id": row.ID})
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
		for _, r := range rows {
			out = append(out, Person{ID: r.ID, TenantID: r.TenantID, DisplayName: r.DisplayName, FrigateName: r.FrigateName,
				Notes: r.Notes, ImageCount: int(r.ImageCount), CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt})
		}
		return nil
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
		return nil
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
	var keys []string
	err := s.tx(ctx, actor, tenantID, authz.FacesManage, func(q *db.Queries) error {
		if _, err := q.SoftDeleteFacePerson(ctx, db.SoftDeleteFacePersonParams{TenantID: tenantID, ID: id, UpdatedBy: &actor.UserID}); err != nil {
			return store.Classify(err)
		}
		imgs, err := q.ListFacePersonImages(ctx, db.ListFacePersonImagesParams{TenantID: tenantID, PersonID: id})
		if err != nil {
			return err
		}
		for _, i := range imgs {
			keys = append(keys, i.StorageKey)
		}
		if err := q.DeleteFacePersonImages(ctx, db.DeleteFacePersonImagesParams{TenantID: tenantID, PersonID: id}); err != nil {
			return err
		}
		return s.audit(ctx, q, actor, tenantID, ActionPersonDeleted, "face_person", id, map[string]any{"person_id": id, "images_removed": len(keys)})
	})
	if err != nil {
		return err
	}
	s.deleteBlobs(ctx, keys)
	return nil
}

// deleteBlobs removes object-store bytes after the metadata commit. A failure leaves an
// unreferenced blob (logged by key only), never a dangling metadata row.
func (s *Service) deleteBlobs(ctx context.Context, keys []string) {
	for _, k := range keys {
		if err := s.Blobs.Delete(ctx, k); err != nil && s.Log != nil {
			s.Log.WarnContext(ctx, "faces: delete image blob failed", "key", k, "error", err)
		}
	}
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
		return nil
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
		if _, err := q.GetFacePerson(ctx, db.GetFacePersonParams{TenantID: tenantID, ID: personID}); err != nil {
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
		return s.audit(ctx, q, actor, tenantID, ActionImageAdded, "face_person_image", imageID,
			map[string]any{"person_id": personID, "image_id": imageID, "sha256": meta.SHA256, "size_bytes": meta.Size})
	})
	if err != nil {
		return Image{}, err
	}
	if err := s.Blobs.Put(ctx, key, data, meta.ContentType); err != nil {
		s.rollbackImage(ctx, actor, tenantID, personID, imageID)
		return Image{}, fmt.Errorf("store face image: %w", err)
	}
	return out, nil
}

func (s *Service) rollbackImage(ctx context.Context, actor authz.Actor, tenantID, personID, imageID uuid.UUID) {
	err := s.Store.Tx(ctx, store.ScopeFor(actor), func(q *db.Queries) error {
		if err := q.DeleteFacePersonImage(ctx, db.DeleteFacePersonImageParams{TenantID: tenantID, PersonID: personID, ID: imageID}); err != nil {
			return err
		}
		return s.audit(ctx, q, actor, tenantID, ActionImageRemoved, "face_person_image", imageID,
			map[string]any{"person_id": personID, "image_id": imageID, "reason": "storage_failed"})
	})
	if err != nil && s.Log != nil {
		s.Log.ErrorContext(ctx, "faces: rollback of image metadata failed", "image_id", imageID, "error", err)
	}
}

// RemoveImage deletes one enrollment image (faces.manage).
func (s *Service) RemoveImage(ctx context.Context, actor authz.Actor, tenantID, personID, imageID uuid.UUID) error {
	var key string
	err := s.tx(ctx, actor, tenantID, authz.FacesManage, func(q *db.Queries) error {
		row, err := q.GetFacePersonImage(ctx, db.GetFacePersonImageParams{TenantID: tenantID, PersonID: personID, ID: imageID})
		if err != nil {
			return store.Classify(err)
		}
		key = row.StorageKey
		if err := q.DeleteFacePersonImage(ctx, db.DeleteFacePersonImageParams{TenantID: tenantID, PersonID: personID, ID: imageID}); err != nil {
			return err
		}
		return s.audit(ctx, q, actor, tenantID, ActionImageRemoved, "face_person_image", imageID,
			map[string]any{"person_id": personID, "image_id": imageID})
	})
	if err != nil {
		return err
	}
	s.deleteBlobs(ctx, []string{key})
	return nil
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
