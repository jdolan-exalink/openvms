package rules

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/jdolan-exalink/openvms/internal/store"
)

type beginCountingTx struct {
	pgx.Tx
	beginCount int
}

func (tx *beginCountingTx) Begin(context.Context) (pgx.Tx, error) {
	tx.beginCount++
	return nil, errors.New("unexpected savepoint")
}

func TestMatchesFacesUsesWarmCacheWithoutSavepoint(t *testing.T) {
	now := time.Now()
	tenantID, personID := uuid.New(), uuid.New()
	resolver := &StoreFaceResolver{
		Store: &store.Store{},
		Now:   func() time.Time { return now },
		cache: map[uuid.UUID]faceIndex{
			tenantID: {
				loaded: now,
				names:  map[uuid.UUID]string{personID: "ana"},
			},
		},
	}
	svc := &Service{
		Faces: resolver,
		Log:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	tx := &beginCountingTx{}
	rule := Rule{Conditions: Conditions{FacePersonIDs: []uuid.UUID{personID}}}
	event := EventContext{TenantID: tenantID, SubLabels: []string{"ana"}}

	if !svc.matchesFaces(context.Background(), tx, event, rule) {
		t.Fatal("warm cached person did not match")
	}
	if tx.beginCount != 0 {
		t.Fatalf("warm cache opened %d savepoints, want none", tx.beginCount)
	}
}
