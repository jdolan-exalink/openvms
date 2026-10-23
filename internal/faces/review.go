package faces

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/jdolan-exalink/openvms/internal/access"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

const (
	ActionSightingRead     = "FACE_SIGHTING_READ"
	ActionSightingAssigned = "FACE_SIGHTING_ASSIGNED"
	ActionSightingRejected = "FACE_SIGHTING_REJECTED"
)

// ReviewSighting is one persisted, still-unclassified face attempt. Source filenames remain
// private; ID is the stable reference used by APIs and the operator interface.
type ReviewSighting struct {
	ID            uuid.UUID
	ServerID      uuid.UUID
	ServerName    string
	SiteID        uuid.UUID
	SeenAt        *time.Time
	Label         string
	Score         *float64
	FirstSeenAt   time.Time
	CropAvailable bool
	IngestError   string
}

type ReviewSightingsPage struct {
	Items []ReviewSighting
}

type ReviewAssignInput struct {
	PersonID      *uuid.UUID
	NewPersonName string
}

// ListReviewSightings returns every pending sighting in the tenant/site/server scope, including
// rows whose image is still being retried. An empty result is still audited by tenant.
func (s *Service) ListReviewSightings(ctx context.Context, actor authz.Actor, f TrainFilter) (ReviewSightingsPage, error) {
	var tenants []uuid.UUID
	var rows []db.ListFaceReviewSightingsRow
	if err := s.Store.TxRaw(ctx, store.ScopeFor(actor), func(tx pgx.Tx) error {
		q := db.New(tx)
		c, err := access.Load(ctx, q, actor)
		if err != nil {
			return err
		}
		if tenants, err = readableTenants(ctx, tx, c, actor, f.TenantID); err != nil {
			return err
		}
		rows, err = q.ListFaceReviewSightings(ctx, db.ListFaceReviewSightingsParams{
			TenantIds: nonNil(tenants), SiteIds: nonNil(f.SiteIDs), ServerIds: nonNil(f.ServerIDs),
		})
		return err
	}); err != nil {
		return ReviewSightingsPage{}, err
	}
	out := ReviewSightingsPage{Items: make([]ReviewSighting, 0, len(rows))}
	counts := make(map[uuid.UUID]int, len(tenants))
	for _, tenantID := range tenants {
		counts[tenantID] = 0
	}
	for _, row := range rows {
		out.Items = append(out.Items, ReviewSighting{
			ID: row.ID, ServerID: row.ServerID, ServerName: row.ServerName, SiteID: row.SiteID,
			SeenAt: row.SourceSeenAt, Label: row.SourceLabel, Score: row.SourceScore,
			FirstSeenAt: row.FirstSeenAt, CropAvailable: row.CropAvailable, IngestError: row.IngestError,
		})
		counts[row.TenantID]++
	}
	err := s.Store.Tx(ctx, store.ScopeFor(actor), func(q *db.Queries) error {
		for _, tenantID := range tenants {
			if err := s.audit(ctx, q, actor, tenantID, ActionSightingRead, "face_sighting", uuid.Nil,
				map[string]any{"scope": "list", "sightings": counts[tenantID]}); err != nil {
				return err
			}
		}
		return nil
	})
	return out, err
}

// ReviewSightingImage serves a retained crop only while its sighting is pending review.
func (s *Service) ReviewSightingImage(ctx context.Context, actor authz.Actor, tenantID, id uuid.UUID) ([]byte, string, error) {
	var row db.FaceSighting
	if err := s.tx(ctx, actor, tenantID, authz.FacesView, func(q *db.Queries) error {
		var err error
		row, err = q.GetFaceSighting(ctx, db.GetFaceSightingParams{TenantID: tenantID, ID: id})
		if err != nil {
			return store.Classify(err)
		}
		if row.ReviewStatus != "pending" || row.CropKey == nil || *row.CropKey == "" {
			return store.ErrNotFound
		}
		return nil
	}); err != nil {
		return nil, "", err
	}
	data, contentType, err := s.Blobs.Get(ctx, *row.CropKey)
	if err != nil {
		return nil, "", store.ErrNotFound
	}
	err = s.tx(ctx, actor, tenantID, authz.FacesView, func(q *db.Queries) error {
		return s.audit(ctx, q, actor, tenantID, ActionSightingRead, "face_sighting", id, map[string]any{"scope": "image"})
	})
	return data, contentType, err
}

// AssignReviewSighting transfers the retained crop into a person's central face library and
// attaches the sighting history to that person in one metadata transaction.
func (s *Service) AssignReviewSighting(ctx context.Context, actor authz.Actor, tenantID, id uuid.UUID, in ReviewAssignInput) (AssignResult, error) {
	var res AssignResult
	if (in.PersonID == nil) == (strings.TrimSpace(in.NewPersonName) == "") {
		return res, invalid("choose exactly one of an existing person or a new person name")
	}
	var source db.FaceSighting
	if err := s.tx(ctx, actor, tenantID, authz.FacesManage, func(q *db.Queries) error {
		var err error
		source, err = q.GetFaceSighting(ctx, db.GetFaceSightingParams{TenantID: tenantID, ID: id})
		if err != nil {
			return store.Classify(err)
		}
		if source.ReviewStatus != "pending" {
			return store.ErrConflict
		}
		if source.CropKey == nil || source.CropContentType == nil || source.CropSha256 == nil || source.CropSizeBytes == nil {
			return store.ErrConflict
		}
		return nil
	}); err != nil {
		return res, err
	}
	data, contentType, err := s.Blobs.Get(ctx, *source.CropKey)
	if err != nil {
		return res, store.ErrNotFound
	}
	meta, err := validateImage(data, contentType)
	if err != nil {
		return res, err
	}

	personID := uuid.Nil
	created := false
	if in.PersonID != nil {
		personID = *in.PersonID
	} else {
		person, err := s.CreatePerson(ctx, actor, tenantID, PersonInput{DisplayName: in.NewPersonName})
		if err != nil {
			return res, err
		}
		personID, created = person.ID, true
	}
	imageID := uuid.New()
	err = s.tx(ctx, actor, tenantID, authz.FacesManage, func(q *db.Queries) error {
		current, err := q.GetFaceSightingForUpdate(ctx, db.GetFaceSightingForUpdateParams{TenantID: tenantID, ID: id})
		if err != nil {
			return store.Classify(err)
		}
		if current.ReviewStatus != "pending" || current.CropKey == nil || *current.CropKey != *source.CropKey {
			return store.ErrConflict
		}
		if err := lockPerson(ctx, q, personID); err != nil {
			return err
		}
		person, err := q.LockFacePersonForUpdate(ctx, db.LockFacePersonForUpdateParams{TenantID: tenantID, ID: personID})
		if err != nil {
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
		imageRow, err := q.InsertFacePersonImage(ctx, db.InsertFacePersonImageParams{
			ID: imageID, TenantID: tenantID, PersonID: personID, StorageKey: *source.CropKey,
			ContentType: meta.ContentType, SizeBytes: int32(meta.Size), //nolint:gosec // bounded by MaxImageBytes
			Sha256: meta.SHA256, Width: &w, Height: &h, CreatedBy: &actor.UserID,
		})
		if err != nil {
			return store.Classify(err)
		}
		changed, err := q.AssignFaceSighting(ctx, db.AssignFaceSightingParams{TenantID: tenantID, ID: id, PersonID: &personID})
		if err != nil {
			return err
		}
		if changed != 1 {
			return store.ErrConflict
		}
		if err := bumpRevision(ctx, q, tenantID, personID); err != nil {
			return err
		}
		count, err := q.CountFacePersonImages(ctx, db.CountFacePersonImagesParams{TenantID: tenantID, PersonID: personID})
		if err != nil {
			return err
		}
		res.Image = toImage(imageRow)
		res.Person = toPerson(person, int(count))
		if err := s.audit(ctx, q, actor, tenantID, ActionImageAdded, "face_person_image", imageID,
			map[string]any{"person_id": personID, "image_id": imageID, "sha256": meta.SHA256, "size_bytes": meta.Size}); err != nil {
			return err
		}
		return s.audit(ctx, q, actor, tenantID, ActionSightingAssigned, "face_sighting", id,
			map[string]any{"sighting_id": id, "person_id": personID, "image_id": imageID})
	})
	if err != nil {
		if created {
			s.discardNewPerson(ctx, actor, tenantID, personID)
		}
		return AssignResult{}, err
	}
	res.AttemptRemoved = s.deleteSightingSource(ctx, source)
	return res, nil
}

// RejectReviewSighting records the manager's decision and queues crop deletion durably.
func (s *Service) RejectReviewSighting(ctx context.Context, actor authz.Actor, tenantID, id uuid.UUID) error {
	return s.tx(ctx, actor, tenantID, authz.FacesManage, func(q *db.Queries) error {
		row, err := q.GetFaceSightingForUpdate(ctx, db.GetFaceSightingForUpdateParams{TenantID: tenantID, ID: id})
		if err != nil {
			return store.Classify(err)
		}
		if row.ReviewStatus != "pending" {
			return store.ErrConflict
		}
		changed, err := q.RejectFaceSighting(ctx, db.RejectFaceSightingParams{TenantID: tenantID, ID: id})
		if err != nil {
			return err
		}
		if changed != 1 {
			return store.ErrConflict
		}
		if row.CropKey != nil && *row.CropKey != "" {
			if err := q.EnqueueFaceBlobPurge(ctx, db.EnqueueFaceBlobPurgeParams{StorageKey: *row.CropKey, TenantID: tenantID}); err != nil {
				return err
			}
		}
		return s.audit(ctx, q, actor, tenantID, ActionSightingRejected, "face_sighting", id, map[string]any{"sighting_id": id})
	})
}

func (s *Service) deleteSightingSource(ctx context.Context, sighting db.FaceSighting) bool {
	if s.Adapters == nil {
		return false
	}
	var server db.FrigateServer
	if err := s.Store.Tx(ctx, store.AllTenants, func(q *db.Queries) error {
		var err error
		server, err = q.GetFaceSyncServer(ctx, sighting.ServerID)
		return err
	}); err != nil {
		return false
	}
	callCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), trainTimeout)
	defer cancel()
	adapter, err := s.Adapters.Get(callCtx, server)
	if err != nil {
		return false
	}
	if err := adapter.DeleteFaceImages(callCtx, "train", []string{sighting.SourceFile}); err != nil {
		if s.Log != nil {
			s.Log.WarnContext(ctx, "assigned face attempt not removed from frigate", "server_id", sighting.ServerID, "error", remoteReason(err))
		}
		return false
	}
	return true
}
