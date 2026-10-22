package faces

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/jdolan-exalink/openvms/internal/frigate"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

const (
	sightingSystemActor = "system:faces-inbox"
)

// ReviewIngestor copies Frigate's unclassified attempts into the tenant's durable face inbox.
// It is run by the worker, independently of operator requests and face-library synchronization.
type ReviewIngestor struct {
	Store    *store.Store
	Blobs    Blobs
	Adapters AdapterSource
	Log      *slog.Logger
}

// RunOnce imports up to limit crops across every face-capable server. Repeated passes are
// idempotent: the source (server, file) pair keeps its original ID and retained crop.
func (r *ReviewIngestor) RunOnce(ctx context.Context, limit int) error {
	if r.Store == nil || r.Blobs == nil || r.Adapters == nil {
		return errors.New("face review ingestion is not configured")
	}
	if limit <= 0 {
		return nil
	}
	if limit > 5000 {
		limit = 5000
	}
	var servers []db.FrigateServer
	if err := r.Store.Tx(ctx, store.AllTenants, func(q *db.Queries) error {
		var err error
		servers, err = q.ListFaceCapableServersAny(ctx)
		return err
	}); err != nil {
		return err
	}

	remaining := limit
	var firstErr error
	for _, srv := range servers {
		if remaining == 0 {
			break
		}
		callCtx, cancel := context.WithTimeout(ctx, trainTimeout)
		adapter, err := r.Adapters.Get(callCtx, srv)
		var train []string
		if err == nil {
			var lib frigate.FaceLibrary
			lib, err = adapter.ListFaces(callCtx)
			if err == nil {
				train = lib.Train
			}
		}
		cancel()
		if err != nil {
			r.warn(ctx, "list unclassified face attempts", srv.ID, err)
			if firstErr == nil {
				firstErr = fmt.Errorf("list face attempts for server %s: %s", srv.ID, remoteReason(err))
			}
			continue
		}

		for _, file := range train {
			if remaining == 0 {
				break
			}
			if !frigate.ValidFaceFile(file) {
				continue
			}
			row, inserted, err := r.ensureSighting(ctx, srv, file)
			if err != nil {
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
			if row.ReviewStatus != "pending" || (row.CropKey != nil && *row.CropKey != "") {
				continue
			}
			remaining--
			if inserted {
				if r.Log != nil {
					r.Log.InfoContext(ctx, "face review sighting recorded", "sighting_id", row.ID, "server_id", srv.ID)
				}
			}
			if err := r.capture(ctx, adapter, srv, file, row); err != nil {
				if firstErr == nil {
					firstErr = err
				}
			}
		}
	}
	return firstErr
}

func (r *ReviewIngestor) ensureSighting(ctx context.Context, srv db.FrigateServer, file string) (db.FaceSighting, bool, error) {
	seen, label, score := parseTrainFile(file)
	var row db.FaceSighting
	inserted := false
	err := r.Store.Tx(ctx, store.AllTenants, func(q *db.Queries) error {
		created, err := q.InsertFaceSighting(ctx, db.InsertFaceSightingParams{
			TenantID: srv.TenantID, ServerID: srv.ID, SourceFile: file, SourceSeenAt: seen,
			SourceLabel: label, SourceScore: score,
		})
		if err == nil {
			row, inserted = created, true
			return insertSightingAudit(ctx, q, srv, created.ID)
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		row, err = q.GetFaceSightingBySource(ctx, db.GetFaceSightingBySourceParams{
			TenantID: srv.TenantID, ServerID: srv.ID, SourceFile: file,
		})
		return err
	})
	return row, inserted, err
}

func (r *ReviewIngestor) capture(ctx context.Context, adapter frigate.Adapter, srv db.FrigateServer, file string, row db.FaceSighting) error {
	data, contentType, err := fetchAttempt(ctx, adapter, file)
	if err != nil {
		_ = r.setSightingRetry(ctx, row.ID, "source_unavailable")
		return fmt.Errorf("capture face sighting %s: source unavailable", row.ID)
	}
	contentType = strings.TrimSpace(strings.SplitN(contentType, ";", 2)[0])
	meta, validationErr := validateImage(data, contentType)
	if validationErr != nil {
		contentType = http.DetectContentType(data)
		meta, validationErr = validateImage(data, contentType)
		if validationErr != nil {
			_ = r.setSightingRetry(ctx, row.ID, "invalid_image")
			return fmt.Errorf("capture face sighting %s: invalid image", row.ID)
		}
	}
	key := fmt.Sprintf("tenant/%s/faces/review/%s", srv.TenantID, row.ID)
	if err := r.Blobs.Put(ctx, key, data, contentType); err != nil {
		_ = r.setSightingRetry(ctx, row.ID, "storage_unavailable")
		return fmt.Errorf("store face sighting %s crop: storage unavailable", row.ID)
	}
	hash := meta.SHA256
	size := int32(meta.Size) //nolint:gosec // validateImage bounds this to MaxImageBytes.
	if err := r.Store.Tx(ctx, store.AllTenants, func(q *db.Queries) error {
		return q.SetFaceSightingCrop(ctx, db.SetFaceSightingCropParams{
			ID: row.ID, CropKey: &key, CropContentType: &contentType, CropSizeBytes: &size, CropSha256: &hash,
		})
	}); err != nil {
		// A later pass reuses the same key, so a transient database error cannot orphan repeated copies.
		return fmt.Errorf("record face sighting %s crop: %w", row.ID, err)
	}
	return nil
}

func (r *ReviewIngestor) setSightingRetry(ctx context.Context, id uuid.UUID, reason string) error {
	return r.Store.Tx(ctx, store.AllTenants, func(q *db.Queries) error {
		return q.SetFaceSightingRetry(ctx, db.SetFaceSightingRetryParams{ID: id, IngestError: reason})
	})
}

func insertSightingAudit(ctx context.Context, q *db.Queries, srv db.FrigateServer, id uuid.UUID) error {
	return q.InsertAudit(ctx, db.InsertAuditParams{
		TenantID: &srv.TenantID, ActorName: sightingSystemActor, Action: ActionSightingIngested,
		TargetType: "face_sighting", TargetID: &id, Details: []byte(`{"source":"frigate"}`),
	})
}

func (r *ReviewIngestor) warn(ctx context.Context, message string, serverID uuid.UUID, err error) {
	if r.Log != nil {
		r.Log.WarnContext(ctx, message, "server_id", serverID, "error", remoteReason(err))
	}
}
