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
