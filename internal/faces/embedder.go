package faces

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"log/slog"

	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

const embeddingVectorSize = 512

// Embedder computes the central embedding of retained face crops. Rows whose stored vector was
// produced by another profile or model id are recomputed, so vectors are never compared across them.
// Crops and vectors are biometric data: only IDs and counts are logged.
type Embedder struct {
	Store   *store.Store
	Blobs   Blobs
	Runtime EmbeddingRuntime
	ModelID string
	Log     *slog.Logger
}

// RunOnce embeds up to limit pending sightings and returns how many were stored. A crop that
// cannot be read or embedded is skipped and reported through the returned error after the pass.
func (e *Embedder) RunOnce(ctx context.Context, limit int) (int, error) {
	if e.Store == nil || e.Blobs == nil || e.Runtime == nil || e.ModelID == "" {
		return 0, errors.New("face embedding is not configured")
	}
	if limit <= 0 {
		return 0, nil
	}
	if limit > 1000 {
		limit = 1000
	}
	profile := string(e.Runtime.Profile())
	var rows []db.ListFaceSightingsToEmbedRow
	if err := e.Store.Tx(ctx, store.AllTenants, func(q *db.Queries) error {
		var err error
		rows, err = q.ListFaceSightingsToEmbed(ctx, db.ListFaceSightingsToEmbedParams{
			Profile: profile, Model: e.ModelID, RowLimit: int32(limit), //nolint:gosec // bounded above.
		})
		return err
	}); err != nil {
		return 0, err
	}

	done := 0
	failed := 0
	for _, row := range rows {
		if err := ctx.Err(); err != nil {
			return done, err
		}
		if row.CropKey == nil {
			continue
		}
		vector, err := e.embedCrop(ctx, *row.CropKey)
		if err != nil {
			failed++
			e.warn(ctx, "embed face sighting", row, err)
			continue
		}
		var stored int64
		if err := e.Store.Tx(ctx, store.AllTenants, func(q *db.Queries) error {
			var err error
			stored, err = q.SetFaceSightingEmbedding(ctx, db.SetFaceSightingEmbeddingParams{
				ID: row.ID, Embedding: vector, Profile: profile, Model: e.ModelID,
			})
			return err
		}); err != nil {
			failed++
			e.warn(ctx, "store face embedding", row, err)
			continue
		}
		done += int(stored)
	}
	if failed > 0 {
		return done, fmt.Errorf("%d of %d face sightings could not be embedded", failed, len(rows))
	}
	return done, nil
}

func (e *Embedder) embedCrop(ctx context.Context, key string) ([]float32, error) {
	data, _, err := e.Blobs.Get(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("read crop: %w", err)
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode crop: %w", err)
	}
	vector, err := e.Runtime.Embed(ctx, img)
	if err != nil {
		return nil, err
	}
	if len(vector) != embeddingVectorSize {
		return nil, fmt.Errorf("embedding has %d values, want %d", len(vector), embeddingVectorSize)
	}
	return vector, nil
}

func (e *Embedder) warn(ctx context.Context, message string, row db.ListFaceSightingsToEmbedRow, err error) {
	if e.Log != nil {
		e.Log.WarnContext(ctx, message, "sighting_id", row.ID, "error", err)
	}
}
