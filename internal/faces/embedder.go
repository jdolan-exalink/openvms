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

const (
	embeddingVectorSize = 512
	// maxEmbedFailures bounds retries of a crop that keeps failing, so it cannot starve fresh ones.
	maxEmbedFailures = 5
)

// Embedder computes the central embedding of retained face crops. Rows whose stored vector was
// produced by another profile or model id are recomputed, so vectors are never compared across them.
// Crops and vectors are biometric data: only IDs and counts are logged.
type Embedder struct {
	Store   *store.Store
	Blobs   Blobs
	Runtime EmbeddingRuntime
	ModelID string
	// Detector, when set, judges every crop before it is embedded. Crops it rejects keep no vector
	// and never become suggestions, but stay in the review inbox. Nil disables the gate.
	Detector FaceDetector
	Log      *slog.Logger
}

// embeddingInputVersion names the preprocessing applied before inference. It is part of the stored
// model id, so changing how crops are fed to the model recomputes every vector instead of mixing
// vectors that are not comparable. in2 is raw 0-255 RGB; in1 (never released as correct) fed [-1, 1].
const embeddingInputVersion = "in2"

func (e *Embedder) storedModel() string { return e.ModelID + "+" + embeddingInputVersion }

// RunOnce embeds up to limit pending sightings and returns how many vectors were stored. A crop that
// cannot be read, judged or embedded is skipped and reported through the returned error after the pass.
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
			Profile: profile, Model: e.storedModel(), MaxFailures: maxEmbedFailures, Detect: e.Detector != nil, RowLimit: int32(limit), //nolint:gosec // bounded above.
		})
		return err
	}); err != nil {
		return 0, err
	}

	done, failed, rejected := 0, 0, 0
	for _, row := range rows {
		if err := ctx.Err(); err != nil {
			return done, err
		}
		if row.CropKey == nil {
			continue
		}
		img, err := e.loadCrop(ctx, *row.CropKey)
		if err != nil {
			failed++
			e.warn(ctx, "embed face sighting", row, err)
			e.recordFailure(ctx, row)
			continue
		}
		var detected *bool
		if e.Detector != nil && row.FaceDetected == nil {
			has, err := e.Detector.HasFace(ctx, img)
			if err != nil {
				failed++
				e.warn(ctx, "detect face in sighting", row, err)
				e.recordFailure(ctx, row)
				continue
			}
			if !has {
				if e.mark(ctx, row, func(q *db.Queries) error { _, err := q.MarkFaceSightingNotFace(ctx, row.ID); return err }) {
					rejected++
				} else {
					failed++
				}
				continue
			}
			detected = &has
			if row.EmbeddedCurrent {
				if !e.mark(ctx, row, func(q *db.Queries) error { _, err := q.MarkFaceSightingFace(ctx, row.ID); return err }) {
					failed++
				}
				continue
			}
		}
		vector, err := e.embed(ctx, img)
		if err != nil {
			failed++
			e.warn(ctx, "embed face sighting", row, err)
			e.recordFailure(ctx, row)
			continue
		}
		var stored int64
		if err := e.Store.Tx(ctx, store.AllTenants, func(q *db.Queries) error {
			var err error
			stored, err = q.SetFaceSightingEmbedding(ctx, db.SetFaceSightingEmbeddingParams{
				ID: row.ID, Embedding: vector, Profile: profile, Model: e.storedModel(), FaceDetected: detected,
			})
			return err
		}); err != nil {
			failed++
			e.warn(ctx, "store face embedding", row, err)
			continue
		}
		done += int(stored)
	}
	if rejected > 0 && e.Log != nil {
		e.Log.InfoContext(ctx, "face crops rejected as non-faces", "count", rejected)
	}
	if failed > 0 {
		return done, fmt.Errorf("%d of %d face sightings could not be processed", failed, len(rows))
	}
	return done, nil
}

func (e *Embedder) loadCrop(ctx context.Context, key string) (image.Image, error) {
	data, _, err := e.Blobs.Get(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("read crop: %w", err)
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode crop: %w", err)
	}
	return img, nil
}

func (e *Embedder) embed(ctx context.Context, img image.Image) ([]float32, error) {
	vector, err := e.Runtime.Embed(ctx, img)
	if err != nil {
		return nil, err
	}
	if len(vector) != embeddingVectorSize {
		return nil, fmt.Errorf("embedding has %d values, want %d", len(vector), embeddingVectorSize)
	}
	return vector, nil
}

// mark stores a detector verdict that needs no new vector and reports whether it was persisted.
func (e *Embedder) mark(ctx context.Context, row db.ListFaceSightingsToEmbedRow, write func(*db.Queries) error) bool {
	if err := e.Store.Tx(ctx, store.AllTenants, func(q *db.Queries) error { return write(q) }); err != nil {
		e.warn(ctx, "store face detection", row, err)
		return false
	}
	return true
}

func (e *Embedder) recordFailure(ctx context.Context, row db.ListFaceSightingsToEmbedRow) {
	if err := e.Store.Tx(ctx, store.AllTenants, func(q *db.Queries) error {
		return q.RecordFaceSightingEmbedFailure(ctx, row.ID)
	}); err != nil {
		e.warn(ctx, "record face embedding failure", row, err)
	}
}

func (e *Embedder) warn(ctx context.Context, message string, row db.ListFaceSightingsToEmbedRow, err error) {
	if e.Log != nil {
		e.Log.WarnContext(ctx, message, "sighting_id", row.ID, "error", err)
	}
}
