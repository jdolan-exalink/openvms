//go:build integration

package store_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
	"github.com/jdolan-exalink/openvms/internal/testutil/demofix"
)

func TestRealtimePublicationOutboxLifecycle(t *testing.T) {
	ctx := context.Background()
	env := demofix.Setup(t)
	tenantID := env.Demo.TenantID
	actor := authz.Actor{TenantID: &tenantID, UserID: env.Admin.UserID}
	messageID := uuid.New()
	payload := json.RawMessage(`{"event_id":"` + uuid.NewString() + `"}`)

	err := env.Store.TxRaw(ctx, store.ScopeFor(actor), func(tx pgx.Tx) error {
		q := db.New(tx)
		// Insertion must supply only the durable message identity and payload; lifecycle
		// defaults are owned by the database.
		_, err := tx.Exec(ctx, `INSERT INTO realtime_publication_outbox
			(id, tenant_id, subject, payload) VALUES ($1, $2, $3, $4)`,
			messageID, tenantID, "openvms.events.created", payload)
		if err != nil {
			return err
		}
		var status string
		var attempts int
		if err := tx.QueryRow(ctx, `SELECT status, attempts FROM realtime_publication_outbox WHERE id = $1`, messageID).Scan(&status, &attempts); err != nil {
			return err
		}
		if status != "pending" || attempts != 0 {
			t.Fatalf("initial status/attempts = %q/%d, want pending/0", status, attempts)
		}
		rows, err := q.ClaimRealtimePublications(ctx, db.ClaimRealtimePublicationsParams{LeaseSeconds: 30, MaxRows: 1, MaxAttempts: 3})
		if err != nil || len(rows) != 1 {
			t.Fatalf("claim = %d rows, %v; want one", len(rows), err)
		}
		claim := rows[0]
		if claim.ID != messageID || claim.LeaseToken == nil || claim.Attempts != 1 {
			t.Fatalf("claim = %+v; want matching ID, lease token and first attempt", claim)
		}
		_, err = tx.Exec(ctx, `UPDATE realtime_publication_outbox SET lease_until = now() - interval '1 second' WHERE id = $1`, messageID)
		if err != nil {
			return err
		}
		rows, err = q.ClaimRealtimePublications(ctx, db.ClaimRealtimePublicationsParams{LeaseSeconds: 30, MaxRows: 1, MaxAttempts: 3})
		if err != nil || len(rows) != 1 || rows[0].Attempts != 2 || rows[0].LeaseToken == nil || *rows[0].LeaseToken == *claim.LeaseToken {
			t.Fatalf("reclaimed lease = %+v, %v; want attempt 2 with a new token", rows, err)
		}
		staleClaim := claim
		claim = rows[0]
		updated, err := q.MarkRealtimePublicationSent(ctx, db.MarkRealtimePublicationSentParams{ID: messageID, LeaseToken: staleClaim.LeaseToken})
		if err != nil || updated != 0 {
			t.Fatalf("stale ack rows = %d, %v; want 0", updated, err)
		}
		updated, err = q.RetryRealtimePublication(ctx, db.RetryRealtimePublicationParams{ID: messageID, LeaseToken: staleClaim.LeaseToken, MaxAttempts: 3, RetrySeconds: 0, LastError: outboxStringPtr("stale retry")})
		if err != nil || updated != 0 {
			t.Fatalf("stale retry rows = %d, %v; want 0", updated, err)
		}
		updated, err = q.RetryRealtimePublication(ctx, db.RetryRealtimePublicationParams{ID: messageID, LeaseToken: claim.LeaseToken, MaxAttempts: 3, RetrySeconds: 0, LastError: outboxStringPtr("temporary publish failure")})
		if err != nil || updated != 1 {
			t.Fatalf("retry rows = %d, %v; want 1", updated, err)
		}
		rows, err = q.ClaimRealtimePublications(ctx, db.ClaimRealtimePublicationsParams{LeaseSeconds: 30, MaxRows: 1, MaxAttempts: 3})
		if err != nil || len(rows) != 1 || rows[0].Attempts != 3 {
			t.Fatalf("third claim = %+v, %v; want attempt 3", rows, err)
		}
		updated, err = q.RetryRealtimePublication(ctx, db.RetryRealtimePublicationParams{ID: messageID, LeaseToken: rows[0].LeaseToken, MaxAttempts: 3, RetrySeconds: 0, LastError: outboxStringPtr("permanent publish failure")})
		if err != nil || updated != 1 {
			t.Fatalf("terminal retry rows = %d, %v; want 1", updated, err)
		}
		var terminal string
		if err := tx.QueryRow(ctx, `SELECT status FROM realtime_publication_outbox WHERE id = $1`, messageID).Scan(&terminal); err != nil {
			return err
		}
		if terminal != "failed" {
			t.Fatalf("terminal status = %q, want failed", terminal)
		}
		_, err = tx.Exec(ctx, `UPDATE realtime_publication_outbox SET status='processing', lease_until=now() - interval '1 second', lease_token=gen_random_uuid() WHERE id=$1`, messageID)
		if err != nil {
			return err
		}
		rows, err = q.ClaimRealtimePublications(ctx, db.ClaimRealtimePublicationsParams{LeaseSeconds: 30, MaxRows: 1, MaxAttempts: 3})
		if err != nil || len(rows) != 0 {
			t.Fatalf("claim after exhausted lease = %+v, %v; want no rows", rows, err)
		}
		if err := tx.QueryRow(ctx, `SELECT status FROM realtime_publication_outbox WHERE id=$1`, messageID).Scan(&terminal); err != nil {
			return err
		}
		if terminal != "failed" {
			t.Fatalf("status after exhausted lease = %q, want failed", terminal)
		}

		// Requeue for an independent successful-send path and verify stale lease fencing.
		_, err = tx.Exec(ctx, `UPDATE realtime_publication_outbox SET status='pending', attempts=0, available_at=now() WHERE id=$1`, messageID)
		if err != nil {
			return err
		}
		rows, err = q.ClaimRealtimePublications(ctx, db.ClaimRealtimePublicationsParams{LeaseSeconds: 30, MaxRows: 1, MaxAttempts: 3})
		if err != nil || len(rows) != 1 {
			return err
		}
		updated, err = q.MarkRealtimePublicationSent(ctx, db.MarkRealtimePublicationSentParams{ID: messageID, LeaseToken: rows[0].LeaseToken})
		if err != nil || updated != 1 {
			t.Fatalf("sent ack rows = %d, %v; want 1", updated, err)
		}
		var sentAt *time.Time
		if err := tx.QueryRow(ctx, `SELECT sent_at FROM realtime_publication_outbox WHERE id=$1`, messageID).Scan(&sentAt); err != nil {
			return err
		}
		if sentAt == nil {
			t.Fatal("sent_at is nil after successful acknowledgement")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	otherTenantID := uuid.New()
	err = env.Store.TxRaw(ctx, store.TenantScope{TenantID: otherTenantID}, func(tx pgx.Tx) error {
		var count int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM realtime_publication_outbox WHERE id = $1`, messageID).Scan(&count); err != nil {
			return err
		}
		if count != 0 {
			t.Fatalf("other tenant can see %d outbox rows, want 0", count)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	err = env.Store.TxRaw(ctx, store.TenantScope{TenantID: otherTenantID}, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO realtime_publication_outbox (tenant_id, subject, payload) VALUES ($1, $2, $3)`,
			tenantID, "openvms.events.forbidden", json.RawMessage(`{}`))
		return err
	})
	var policyErr *pgconn.PgError
	if !errors.As(err, &policyErr) || policyErr.Code != "42501" {
		t.Fatalf("cross-tenant insert error = %v, want RLS policy violation", err)
	}
}

func outboxStringPtr(v string) *string { return &v }

func TestRealtimePublicationAggregateClaimOrdering(t *testing.T) {
	ctx := context.Background()
	env := demofix.Setup(t)
	tenantID := env.Demo.TenantID
	aggregateKey := "alarm/" + uuid.NewString()
	var firstID, secondID, unrelatedID, independentID uuid.UUID

	err := env.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		q := db.New(tx)
		rows := []struct {
			id  uuid.UUID
			key *string
		}{
			{firstID, &aggregateKey},
			{secondID, &aggregateKey},
			{unrelatedID, outboxStringPtr("alarm/" + uuid.NewString())},
			{independentID, nil},
		}
		for i, row := range rows {
			created, err := q.EnqueueRealtimePublication(ctx, db.EnqueueRealtimePublicationParams{
				TenantID: tenantID, Subject: "alarm.opened." + tenantID.String(), Payload: json.RawMessage(`{}`), AggregateKey: row.key,
			})
			if err != nil {
				return err
			}
			if i == 0 {
				firstID = created.ID
			}
			if i == 1 {
				secondID = created.ID
			}
			if i == 2 {
				unrelatedID = created.ID
			}
			if i == 3 {
				independentID = created.ID
			}
			if i == 0 && (created.AggregateSequence == nil || *created.AggregateSequence != 1) {
				t.Fatalf("first aggregate sequence = %v; want 1", created.AggregateSequence)
			}
			if i == 1 && (created.AggregateSequence == nil || *created.AggregateSequence != 2) {
				t.Fatalf("second aggregate sequence = %v; want 2", created.AggregateSequence)
			}
			if i == 3 && (created.AggregateKey != nil || created.AggregateSequence != nil) {
				t.Fatalf("NULL aggregate metadata = %v/%v; want nil/nil", created.AggregateKey, created.AggregateSequence)
			}
			if _, err := tx.Exec(ctx, `UPDATE realtime_publication_outbox SET available_at=now() - ((10 - $2) * interval '1 second') WHERE id=$1`, created.ID, i); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// Hold the first aggregate row locked in replica one while replica two claims.
	firstReplica, err := env.Store.Pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer firstReplica.Rollback(ctx)
	if _, err := firstReplica.Exec(ctx, `SELECT set_config('app.all_tenants', 'on', true)`); err != nil {
		t.Fatal(err)
	}
	firstRows, err := db.New(firstReplica).ClaimRealtimePublications(ctx, db.ClaimRealtimePublicationsParams{LeaseSeconds: 30, MaxRows: 1, MaxAttempts: 3})
	if err != nil || len(firstRows) != 1 || firstRows[0].ID != firstID {
		t.Fatalf("replica one claim = %+v, %v; first=%s second=%s unrelated=%s independent=%s", firstRows, err, firstID, secondID, unrelatedID, independentID)
	}

	secondReplica, err := env.Store.Pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := secondReplica.Exec(ctx, `SELECT set_config('app.all_tenants', 'on', true)`); err != nil {
		t.Fatal(err)
	}
	secondRows, err := db.New(secondReplica).ClaimRealtimePublications(ctx, db.ClaimRealtimePublicationsParams{LeaseSeconds: 30, MaxRows: 10, MaxAttempts: 3})
	if err != nil {
		t.Fatal(err)
	}
	claimed := make(map[uuid.UUID]bool, len(secondRows))
	for _, row := range secondRows {
		claimed[row.ID] = true
	}
	if claimed[secondID] || !claimed[unrelatedID] || !claimed[independentID] {
		t.Fatalf("replica two claims = %v; want no later aggregate row and unrelated/NULL rows", claimed)
	}
	if err := secondReplica.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if err := firstReplica.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	// A terminally failed predecessor is terminal and must release its successor.
	if err := env.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE realtime_publication_outbox SET status='failed', lease_until=NULL, lease_token=NULL WHERE id=$1`, firstID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := env.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		rows, err := db.New(tx).ClaimRealtimePublications(ctx, db.ClaimRealtimePublicationsParams{LeaseSeconds: 30, MaxRows: 1, MaxAttempts: 3})
		if err != nil {
			return err
		}
		if len(rows) != 1 || rows[0].ID != secondID {
			t.Fatalf("claim after terminal predecessor = %+v; want successor %s", rows, secondID)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestRealtimePublicationConcurrentEnqueueSequencesFollowLockOrder(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	env := demofix.Setup(t)
	tenantID := env.Demo.TenantID
	key := "alarm/" + uuid.NewString()
	params := db.EnqueueRealtimePublicationParams{
		TenantID: tenantID, Subject: "alarm.updated." + tenantID.String(),
		Payload: json.RawMessage(`{}`), AggregateKey: &key,
	}

	firstTx, err := env.Store.Pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer firstTx.Rollback(ctx)
	if _, err := firstTx.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, tenantID.String()); err != nil {
		t.Fatal(err)
	}
	first, err := db.New(firstTx).EnqueueRealtimePublication(ctx, params)
	if err != nil {
		t.Fatal(err)
	}
	if first.AggregateSequence == nil || *first.AggregateSequence != 1 {
		t.Fatalf("first sequence = %v; want 1", first.AggregateSequence)
	}

	secondTx, err := env.Store.Pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer secondTx.Rollback(ctx)
	if _, err := secondTx.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, tenantID.String()); err != nil {
		t.Fatal(err)
	}
	var secondPID int
	if err := secondTx.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&secondPID); err != nil {
		t.Fatal(err)
	}
	result := make(chan struct {
		row db.RealtimePublicationOutbox
		err error
	}, 1)
	go func() {
		row, err := db.New(secondTx).EnqueueRealtimePublication(ctx, params)
		result <- struct {
			row db.RealtimePublicationOutbox
			err error
		}{row: row, err: err}
	}()

	deadline := time.Now().Add(5 * time.Second)
	locked := false
	for time.Now().Before(deadline) {
		var hasUngrantedLock bool
		if err := firstTx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_locks WHERE pid=$1 AND NOT granted)`, secondPID).Scan(&hasUngrantedLock); err != nil {
			t.Fatal(err)
		}
		if hasUngrantedLock {
			locked = true
			break
		}
		select {
		case outcome := <-result:
			t.Fatalf("second enqueue completed before aggregate lock wait: row=%+v err=%v", outcome.row, outcome.err)
		case <-time.After(10 * time.Millisecond):
		}
	}
	if !locked {
		t.Fatal("second transaction never waited on the first transaction's aggregate lock")
	}

	if err := firstTx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	outcome := <-result
	if outcome.err != nil {
		t.Fatal(outcome.err)
	}
	if outcome.row.AggregateSequence == nil || *outcome.row.AggregateSequence != 2 {
		t.Fatalf("second sequence = %v; want 2 after first transaction acquired the lock", outcome.row.AggregateSequence)
	}
	if err := secondTx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}
