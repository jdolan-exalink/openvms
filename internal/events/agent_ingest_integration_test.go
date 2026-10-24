//go:build integration

package events_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/jdolan-exalink/openvms/internal/events"
	"github.com/jdolan-exalink/openvms/internal/provision"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/testutil/pgtest"
)

// fakeAgent behaves like the real queue: pages hold the events after a sequence, an ack removes what
// was stored, and a lost ack leaves the events there to be read again.
type fakeAgent struct {
	mu        sync.Mutex
	queue     []provision.LPRQueuedEvent
	lastSeq   uint64
	lost      uint64
	pullErr   error
	epoch     string
	failAcks  int
	acks      []uint64
	pageLimit int
}

func (a *fakeAgent) add(camera, plate string, conf *float64, at time.Time) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.lastSeq++
	id := uuid.NewString()[:8]
	a.queue = append(a.queue, provision.LPRQueuedEvent{Seq: a.lastSeq, ID: id, Event: provision.LPRAgentEvent{CameraID: camera, Plate: plate, EventTime: at, ReceivedTime: at, Confidence: conf}})
	return id
}

func (a *fakeAgent) PullProvisionLPREvents(_ context.Context, _ uuid.UUID, after uint64) (provision.LPREventsPage, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.pullErr != nil {
		return provision.LPREventsPage{}, a.pullErr
	}
	page := provision.LPREventsPage{LastSeq: a.lastSeq, Depth: len(a.queue), Lost: a.lost, Epoch: a.epoch}
	for _, e := range a.queue {
		if e.Seq > after && (a.pageLimit == 0 || len(page.Events) < a.pageLimit) {
			page.Events = append(page.Events, e)
		}
	}
	return page, nil
}

func (a *fakeAgent) AckProvisionLPREvents(_ context.Context, _ uuid.UUID, through uint64) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.acks = append(a.acks, through)
	if a.failAcks > 0 {
		a.failAcks--
		return errors.New("agent unreachable")
	}
	kept := a.queue[:0]
	for _, e := range a.queue {
		if e.Seq > through {
			kept = append(kept, e)
		}
	}
	a.queue = kept
	return nil
}

type ingestWorld struct {
	st                         *store.Store
	tenant, site, server, camA uuid.UUID
	camOtherServer             uuid.UUID
	agent                      *fakeAgent
	ing                        *events.AgentIngestor
}

func newIngestWorld(t *testing.T) *ingestWorld {
	t.Helper()
	pool, _ := pgtest.Migrated(t)
	w := &ingestWorld{st: &store.Store{Pool: pool}, tenant: uuid.New(), site: uuid.New(), server: uuid.New(), camA: uuid.New(), camOtherServer: uuid.New(), agent: &fakeAgent{epoch: "epoch-1"}}
	otherServer := uuid.New()
	ctx := context.Background()
	err := w.st.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		stmts := []struct {
			sql  string
			args []any
		}{
			{`INSERT INTO tenants (id, slug, name) VALUES ($1, 'acme', 'Acme')`, []any{w.tenant}},
			{`INSERT INTO sites (id, tenant_id, name) VALUES ($1, $2, 'Site')`, []any{w.site, w.tenant}},
			{`INSERT INTO frigate_servers (id, tenant_id, site_id, name, base_url) VALUES ($1, $2, $3, 'srv', 'http://example.invalid')`, []any{w.server, w.tenant, w.site}},
			{`INSERT INTO frigate_servers (id, tenant_id, site_id, name, base_url) VALUES ($1, $2, $3, 'other', 'http://example.invalid')`, []any{otherServer, w.tenant, w.site}},
			{`INSERT INTO cameras (id, tenant_id, site_id, server_id, remote_name, display_name, lpr) VALUES ($1, $2, $3, $4, 'cam-a', 'Cam A', true)`, []any{w.camA, w.tenant, w.site, w.server}},
			{`INSERT INTO cameras (id, tenant_id, site_id, server_id, remote_name, display_name, lpr) VALUES ($1, $2, $3, $4, 'cam-x', 'Cam X', true)`, []any{w.camOtherServer, w.tenant, w.site, otherServer}},
			{`INSERT INTO server_agents (server_id, tenant_id, host, port, token_sealed) VALUES ($1, $2, '192.0.2.10', 7419, '\x00')`, []any{w.server, w.tenant}},
		}
		for _, s := range stmts {
			if _, err := tx.Exec(ctx, s.sql, s.args...); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	w.ing = &events.AgentIngestor{Store: w.st, Source: w.agent, Log: pgtest.Discard(), Interval: time.Hour}
	return w
}

type readRow struct {
	remoteID, plate, norm string
	score                 *float32
	camera                uuid.UUID
	seenAt                time.Time
	cropAttempts          int
}

func (w *ingestWorld) reads(t *testing.T) []readRow {
	t.Helper()
	var out []readRow
	err := w.st.TxRaw(context.Background(), store.AllTenants, func(tx pgx.Tx) error {
		rows, err := tx.Query(context.Background(), `SELECT remote_event_id, plate, plate_normalized, score, camera_id, seen_at, crop_attempts FROM lpr_reads WHERE server_id = $1 ORDER BY seen_at, remote_event_id`, w.server)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var r readRow
			if err := rows.Scan(&r.remoteID, &r.plate, &r.norm, &r.score, &r.camera, &r.seenAt, &r.cropAttempts); err != nil {
				return err
			}
			out = append(out, r)
		}
		return rows.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

type cursorRow struct {
	lastSeq                 uint64
	ingested, skipped, lost int64
	depth                   int
	lastError               string
	pulled                  bool
}

func (w *ingestWorld) cursor(t *testing.T) cursorRow {
	t.Helper()
	var c cursorRow
	err := w.st.TxRaw(context.Background(), store.AllTenants, func(tx pgx.Tx) error {
		var pulledAt *time.Time
		err := tx.QueryRow(context.Background(), `SELECT last_seq, ingested_total, skipped_total, agent_lost, agent_depth, last_error, last_pull_at FROM server_agent_event_cursors WHERE server_id = $1`, w.server).
			Scan(&c.lastSeq, &c.ingested, &c.skipped, &c.lost, &c.depth, &c.lastError, &pulledAt)
		c.pulled = pulledAt != nil
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

var t0 = time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)

func TestAgentIngestStoresReadingsOnceAndAcknowledgesThem(t *testing.T) {
	w := newIngestWorld(t)
	conf := 0.9
	id1 := w.agent.add(w.camA.String(), "ab 123-cd", &conf, t0)
	w.agent.add(w.camA.String(), "EF456GH", nil, t0.Add(time.Minute))

	w.ing.IngestAll(context.Background())

	got := w.reads(t)
	if len(got) != 2 {
		t.Fatalf("reads = %+v, want 2", got)
	}
	r := got[0]
	if r.remoteID != "agent:"+id1 || r.plate != "ab 123-cd" || r.norm != "AB123CD" || r.camera != w.camA || !r.seenAt.Equal(t0) || r.score == nil || *r.score != 0.9 {
		t.Fatalf("first read = %+v", r)
	}
	if got[1].score != nil {
		t.Fatalf("a reading without a confidence must have no score: %+v", got[1])
	}
	if r.cropAttempts < 5 {
		t.Fatalf("crop_attempts = %d: the Frigate crop copier would look for a crop that does not exist", r.cropAttempts)
	}
	if c := w.cursor(t); c.lastSeq != 2 || c.ingested != 2 || c.skipped != 0 || c.lastError != "" || !c.pulled {
		t.Fatalf("cursor = %+v", c)
	}
	if len(w.agent.acks) != 1 || w.agent.acks[0] != 2 || len(w.agent.queue) != 0 {
		t.Fatalf("acks = %v, queue left = %d; want one ack of 2 and an empty queue", w.agent.acks, len(w.agent.queue))
	}
}

func TestAgentIngestIsIdempotentWhenTheAckWasLostAndTheCursorIsGone(t *testing.T) {
	w := newIngestWorld(t)
	w.agent.add(w.camA.String(), "AB123CD", nil, t0)
	w.agent.failAcks = 1

	w.ing.IngestAll(context.Background())
	if len(w.agent.queue) != 1 {
		t.Fatal("setup: the failed ack should have left the event in the agent queue")
	}
	// Central loses its cursor (restore from backup) and reads the same event again.
	if err := w.st.TxRaw(context.Background(), store.AllTenants, func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(), `DELETE FROM server_agent_event_cursors WHERE server_id = $1`, w.server)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	w.ing.IngestAll(context.Background())

	if got := w.reads(t); len(got) != 1 {
		t.Fatalf("the same reading was stored %d times", len(got))
	}
	if len(w.agent.queue) != 0 {
		t.Fatal("the second run should have acknowledged the event")
	}
}

func TestAgentIngestReAcknowledgesWhenOnlyTheAckFailed(t *testing.T) {
	w := newIngestWorld(t)
	w.agent.add(w.camA.String(), "AB123CD", nil, t0)
	w.agent.failAcks = 1
	w.ing.IngestAll(context.Background())
	w.ing.IngestAll(context.Background()) // cursor is already 1; the page is empty but the queue is not

	if len(w.agent.queue) != 0 {
		t.Fatalf("the agent still holds %d acknowledged events", len(w.agent.queue))
	}
	if got := w.reads(t); len(got) != 1 {
		t.Fatalf("reads = %d, want 1", len(got))
	}
}

func TestAgentIngestSkipsWhatCentralCannotAttributeWithoutBlockingTheRest(t *testing.T) {
	w := newIngestWorld(t)
	w.agent.add("not-a-uuid", "AAA111", nil, t0)
	w.agent.add(uuid.NewString(), "BBB222", nil, t0)          // unknown camera
	w.agent.add(w.camOtherServer.String(), "CCC333", nil, t0) // a camera of another server
	w.agent.add(w.camA.String(), " - ", nil, t0)              // no plate left after normalising
	w.agent.add(w.camA.String(), "DDD444", nil, t0)

	w.ing.IngestAll(context.Background())

	got := w.reads(t)
	if len(got) != 1 || got[0].norm != "DDD444" {
		t.Fatalf("reads = %+v, want only the valid one", got)
	}
	if c := w.cursor(t); c.lastSeq != 5 || c.ingested != 1 || c.skipped != 4 {
		t.Fatalf("cursor = %+v; the skipped events must not hold the cursor back", c)
	}
	if len(w.agent.queue) != 0 {
		t.Fatal("skipped events must still be acknowledged or they would be read forever")
	}
}

func TestAgentIngestRestartsFromTheStartWhenTheAgentLostItsState(t *testing.T) {
	w := newIngestWorld(t)
	for i := 0; i < 5; i++ {
		w.agent.add(w.camA.String(), "OLD"+string(rune('A'+i)), nil, t0)
	}
	w.ing.IngestAll(context.Background())
	if c := w.cursor(t); c.lastSeq != 5 {
		t.Fatalf("setup: cursor = %+v", c)
	}

	// The agent's state directory is wiped: sequences start again at 1 under a new epoch.
	w.agent.queue, w.agent.lastSeq, w.agent.epoch = nil, 0, "epoch-2"
	w.agent.add(w.camA.String(), "NEWONE", nil, t0.Add(time.Hour))
	w.ing.IngestAll(context.Background())

	found := false
	for _, r := range w.reads(t) {
		found = found || r.norm == "NEWONE"
	}
	if !found {
		t.Fatalf("a reading after an agent reset was never stored: %+v", w.reads(t))
	}
}

func TestAgentIngestRecordsAPullFailureAndKeepsTheCursor(t *testing.T) {
	w := newIngestWorld(t)
	w.agent.add(w.camA.String(), "AB123CD", nil, t0)
	w.ing.IngestAll(context.Background())

	w.agent.pullErr = errors.New("agent unreachable")
	w.agent.add(w.camA.String(), "EF456GH", nil, t0.Add(time.Minute))
	w.ing.IngestAll(context.Background())

	c := w.cursor(t)
	if c.lastSeq != 1 || c.lastError == "" {
		t.Fatalf("cursor = %+v; want the cursor kept and the error recorded", c)
	}
	w.agent.pullErr = nil
	w.ing.IngestAll(context.Background())
	if c := w.cursor(t); c.lastSeq != 2 || c.lastError != "" {
		t.Fatalf("after recovery cursor = %+v; want it to catch up and clear the error", c)
	}
}

func TestAgentIngestReadsSeveralPagesInOnePass(t *testing.T) {
	w := newIngestWorld(t)
	w.agent.pageLimit = 2
	for i := 0; i < 5; i++ {
		w.agent.add(w.camA.String(), "PG"+string(rune('A'+i))+"11", nil, t0.Add(time.Duration(i)*time.Second))
	}
	w.ing.IngestAll(context.Background())
	if got := w.reads(t); len(got) != 5 {
		t.Fatalf("reads = %d, want all 5 over three pages", len(got))
	}
}

// The sequence alone cannot tell a wiped agent from a busy one: once it has produced as many events
// as the old cursor, "after the cursor" would skip the new ones. The epoch does.
func TestAgentIngestDetectsAWipedAgentEvenWhenItsSequenceCaughtUp(t *testing.T) {
	w := newIngestWorld(t)
	for i := 0; i < 5; i++ {
		w.agent.add(w.camA.String(), "OLD"+string(rune('A'+i))+"11", nil, t0)
	}
	w.ing.IngestAll(context.Background())

	w.agent.queue, w.agent.lastSeq, w.agent.epoch = nil, 0, "epoch-2"
	for i := 0; i < 7; i++ { // more events than the old cursor before central looks again
		w.agent.add(w.camA.String(), "NEW"+string(rune('A'+i))+"22", nil, t0.Add(time.Hour))
	}
	w.ing.IngestAll(context.Background())

	if got := w.reads(t); len(got) != 12 {
		t.Fatalf("reads = %d, want 5 old + 7 new: the first five events of the new queue were skipped", len(got))
	}
	if c := w.cursor(t); c.lastSeq != 7 {
		t.Fatalf("cursor = %+v, want 7", c)
	}
}

func TestAgentIngestWithoutAnEpochStillDetectsABackwardsSequence(t *testing.T) {
	w := newIngestWorld(t)
	w.agent.epoch = "" // an agent that predates the epoch
	for i := 0; i < 5; i++ {
		w.agent.add(w.camA.String(), "OLD"+string(rune('A'+i))+"11", nil, t0)
	}
	w.ing.IngestAll(context.Background())

	w.agent.queue, w.agent.lastSeq = nil, 0
	w.agent.add(w.camA.String(), "NEWONE", nil, t0.Add(time.Hour))
	w.ing.IngestAll(context.Background())

	if got := w.reads(t); len(got) != 6 {
		t.Fatalf("reads = %d, want 6", len(got))
	}
}

// An error text that is cut inside a multibyte character must still be recorded.
func TestAgentIngestRecordsALongNonASCIIErrorMessage(t *testing.T) {
	w := newIngestWorld(t)
	w.agent.pullErr = errors.New(strings.Repeat("\u20ac", 300)) // 900 bytes; byte 500 falls inside a character
	w.ing.IngestAll(context.Background())

	if c := w.cursor(t); c.lastError == "" {
		t.Fatal("the failure was not recorded: the truncated message was rejected by the database")
	}
}
