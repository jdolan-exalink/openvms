//go:build integration

package notify_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/jdolan-exalink/openvms/internal/notify"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/testutil/demofix"
	"github.com/jdolan-exalink/openvms/internal/testutil/pgtest"
)

type fixture struct {
	t      *testing.T
	env    *demofix.Env
	worker *notify.Worker
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	env := demofix.Setup(t)
	return &fixture{t: t, env: env, worker: &notify.Worker{
		Store: env.Store, Sealer: env.Sealer, Log: pgtest.Discard(),
		MaxAttempts: 3, BackoffBase: time.Millisecond, BackoffMax: 2 * time.Millisecond,
		Deps: notify.Deps{HTTP: notify.NewHTTPClient(5 * time.Second)},
	}}
}

func (f *fixture) addWebhook(url string, enabled bool) uuid.UUID {
	f.t.Helper()
	cfg, _ := json.Marshal(notify.Config{URL: url})
	id := uuid.New()
	err := f.env.Store.TxRaw(context.Background(), store.AllTenants, func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(),
			`INSERT INTO notification_channels (id, tenant_id, name, type, config, enabled) VALUES ($1, $2, $3, 'webhook', $4, $5)`,
			id, f.env.Demo.TenantID, "wh-"+id.String(), cfg, enabled)
		return err
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return id
}

func (f *fixture) enqueue(channels ...uuid.UUID) {
	f.t.Helper()
	err := f.env.Store.TxRaw(context.Background(), store.AllTenants, func(tx pgx.Tx) error {
		n, err := notify.Enqueue(context.Background(), tx, notify.EnqueueInput{
			TenantID: f.env.Demo.TenantID, ChannelIDs: channels, Title: "Alerta", Body: "Cuerpo", Severity: "warning", OccurredAt: time.Now(),
		})
		if err == nil && n != len(channels) {
			f.t.Fatalf("enqueued %d deliveries, want %d", n, len(channels))
		}
		return err
	})
	if err != nil {
		f.t.Fatal(err)
	}
}

type deliveryRow struct {
	Status   string
	Attempts int
	Err      string
}

func (f *fixture) delivery(channel uuid.UUID) deliveryRow {
	f.t.Helper()
	var r deliveryRow
	err := f.env.Store.TxRaw(context.Background(), store.AllTenants, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(),
			`SELECT status, attempts, coalesce(last_error, '') FROM notification_deliveries WHERE channel_id = $1`, channel).Scan(&r.Status, &r.Attempts, &r.Err)
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return r
}

func TestWorkerDeliversPendingRows(t *testing.T) {
	f := newFixture(t)
	var hits atomic.Int32
	var mu sync.Mutex
	var payload map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		mu.Lock()
		defer mu.Unlock()
		_ = json.NewDecoder(r.Body).Decode(&payload)
	}))
	defer srv.Close()
	ch := f.addWebhook(srv.URL, true)
	f.enqueue(ch)

	n, err := f.worker.RunOnce(context.Background())
	if err != nil || n != 1 {
		t.Fatalf("RunOnce = %d, %v; want 1, nil", n, err)
	}
	if got := f.delivery(ch); got.Status != "sent" || got.Attempts != 1 {
		t.Fatalf("delivery = %+v, want sent after 1 attempt", got)
	}
	mu.Lock()
	defer mu.Unlock()
	if hits.Load() != 1 || payload["title"] != "Alerta" || payload["severity"] != "warning" {
		t.Fatalf("hits=%d payload=%v", hits.Load(), payload)
	}
	// Nothing left to do.
	if n, _ := f.worker.RunOnce(context.Background()); n != 0 {
		t.Fatalf("second RunOnce processed %d rows, want 0", n)
	}
}

func TestWorkerRetriesThenMarksFailed(t *testing.T) {
	f := newFixture(t)
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	ch := f.addWebhook(srv.URL, true)
	f.enqueue(ch)

	for i := 0; i < 10; i++ {
		_, _ = f.worker.RunOnce(context.Background())
		time.Sleep(5 * time.Millisecond)
	}
	got := f.delivery(ch)
	if got.Status != "failed" || got.Attempts != 3 || hits.Load() != 3 {
		t.Fatalf("delivery = %+v hits=%d; want failed after exactly 3 attempts", got, hits.Load())
	}
	if got.Err == "" {
		t.Fatalf("last_error should describe the failure")
	}
}

func TestWorkerDoesNotRetryPermanentFailures(t *testing.T) {
	f := newFixture(t)
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()
	ch := f.addWebhook(srv.URL, true)
	f.enqueue(ch)
	for i := 0; i < 3; i++ {
		_, _ = f.worker.RunOnce(context.Background())
		time.Sleep(5 * time.Millisecond)
	}
	if got := f.delivery(ch); got.Status != "failed" || got.Attempts != 1 || hits.Load() != 1 {
		t.Fatalf("delivery = %+v hits=%d; want failed after 1 attempt", got, hits.Load())
	}
}

func TestWorkerFailsRowsOfDisabledChannels(t *testing.T) {
	f := newFixture(t)
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	ch := f.addWebhook(srv.URL, true)
	f.enqueue(ch)
	if err := f.env.Store.TxRaw(context.Background(), store.AllTenants, func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(), `UPDATE notification_channels SET enabled = false WHERE id = $1`, ch)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	_, _ = f.worker.RunOnce(context.Background())
	if got := f.delivery(ch); got.Status != "failed" || got.Err == "" {
		t.Fatalf("delivery = %+v, want failed with a reason", got)
	}
}

func TestEnqueueSkipsDisabledChannelsAndIsolatesTenantData(t *testing.T) {
	f := newFixture(t)
	on := f.addWebhook("https://example.com/a", true)
	off := f.addWebhook("https://example.com/b", false)
	err := f.env.Store.TxRaw(context.Background(), store.AllTenants, func(tx pgx.Tx) error {
		n, err := notify.Enqueue(context.Background(), tx, notify.EnqueueInput{
			TenantID: f.env.Demo.TenantID, ChannelIDs: []uuid.UUID{on, off, uuid.New()}, Title: "t", Body: "b", Severity: "info",
		})
		if n != 1 {
			t.Fatalf("enqueued %d, want 1 (disabled and unknown channels are skipped)", n)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestPruneKeepsPendingAndYoungRows(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	ch := f.addWebhook("http://127.0.0.1:1/unused", true)
	f.worker.PruneBatch = 2 // force several batches
	old := time.Now().Add(-60 * 24 * time.Hour)
	err := f.env.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		for _, r := range []struct {
			status string
			at     time.Time
		}{{"sent", old}, {"failed", old}, {"sent", old}, {"pending", old}, {"sent", time.Now()}} {
			if _, err := tx.Exec(ctx, `INSERT INTO notification_deliveries (tenant_id, channel_id, channel_name, channel_type, payload, status, created_at)
				VALUES ($1, $2, 'c', 'webhook', '{}', $3, $4)`, f.env.Demo.TenantID, ch, r.status, r.at); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if d, _ := f.worker.Prune(ctx); d != 3 {
		t.Fatalf("pruned %d deliveries, want 3 (old sent/failed only)", d)
	}
	var left int
	err = f.env.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM notification_deliveries WHERE channel_id = $1`, ch).Scan(&left)
	})
	if err != nil || left != 2 {
		t.Fatalf("left %d rows (err %v), want 2 (old pending + recent sent)", left, err)
	}
}
