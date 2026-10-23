package outbox

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jdolan-exalink/openvms/internal/store/db"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

type fakeRepository struct {
	rows     []db.RealtimePublicationOutbox
	sent     []uuid.UUID
	retries  []retryCall
	claimErr error
	markErr  error
	retryErr error
}

type retryCall struct {
	id    uuid.UUID
	delay time.Duration
	err   string
}

func (r *fakeRepository) Claim(_ context.Context, _ int, _ time.Duration, _ int) ([]db.RealtimePublicationOutbox, error) {
	return r.rows, r.claimErr
}
func (r *fakeRepository) MarkSent(_ context.Context, _, id, _ uuid.UUID) error {
	r.sent = append(r.sent, id)
	return r.markErr
}
func (r *fakeRepository) Retry(_ context.Context, _, id, _ uuid.UUID, delay time.Duration, _ int, message string) error {
	r.retries = append(r.retries, retryCall{id: id, delay: delay, err: message})
	return r.retryErr
}

type fakePublisher struct {
	calls  int
	err    error
	cancel context.CancelFunc
}

func (p *fakePublisher) Publish(_ context.Context, _ db.RealtimePublicationOutbox) error {
	p.calls++
	if p.cancel != nil {
		p.cancel()
	}
	return p.err
}

func TestPositiveInt32BoundsConfiguration(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	if got := positiveInt32(maxInt, 8); got != 1<<31-1 {
		t.Fatalf("positiveInt32(maxInt) = %d, want %d", got, int32(1<<31-1))
	}
	if got := positiveInt32(0, 8); got != 8 {
		t.Fatalf("positiveInt32(0) = %d, want 8", got)
	}
	if got := positiveInt32(3, 8); got != 3 {
		t.Fatalf("positiveInt32(3) = %d, want 3", got)
	}
}

func TestDispatcherPublishesThenAcknowledges(t *testing.T) {
	id, token := uuid.New(), uuid.New()
	repo := &fakeRepository{rows: []db.RealtimePublicationOutbox{{ID: id, LeaseToken: &token}}}
	pub := &fakePublisher{}
	d := Dispatcher{Repository: repo, Publisher: pub, Config: Config{MaxAttempts: 3, BatchSize: 10, Lease: time.Minute}}

	if err := d.ProcessBatch(context.Background()); err != nil {
		t.Fatal(err)
	}
	if pub.calls != 1 || len(repo.sent) != 1 || repo.sent[0] != id {
		t.Fatalf("publish=%d sent=%v", pub.calls, repo.sent)
	}
	if len(repo.retries) != 0 {
		t.Fatalf("unexpected retry: %+v", repo.retries)
	}
}

func TestDispatcherRetriesPublishFailureWithBoundedBackoff(t *testing.T) {
	id, token := uuid.New(), uuid.New()
	repo := &fakeRepository{rows: []db.RealtimePublicationOutbox{{ID: id, LeaseToken: &token, Attempts: 2}}}
	pub := &fakePublisher{err: errors.New("nats unavailable")}
	d := Dispatcher{Repository: repo, Publisher: pub, Config: Config{MaxAttempts: 3, BatchSize: 10, Lease: time.Minute, RetryBase: time.Second, RetryMax: 5 * time.Second}}

	if err := d.ProcessBatch(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(repo.sent) != 0 || len(repo.retries) != 1 {
		t.Fatalf("sent=%v retries=%+v", repo.sent, repo.retries)
	}
	if got := repo.retries[0]; got.id != id || got.delay != 2*time.Second || got.err != "nats unavailable" {
		t.Fatalf("retry=%+v", got)
	}
}

func TestDispatcherLeavesLeaseOnCancellation(t *testing.T) {
	id, token := uuid.New(), uuid.New()
	repo := &fakeRepository{rows: []db.RealtimePublicationOutbox{{ID: id, LeaseToken: &token}}}
	ctx, cancel := context.WithCancel(context.Background())
	pub := &fakePublisher{err: context.Canceled, cancel: cancel}
	d := Dispatcher{Repository: repo, Publisher: pub, Config: Config{MaxAttempts: 3, BatchSize: 1, Lease: time.Minute}}

	if err := d.ProcessBatch(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
	if len(repo.sent) != 0 || len(repo.retries) != 0 {
		t.Fatalf("canceled publication must remain leased: sent=%v retries=%v", repo.sent, repo.retries)
	}

	runCtx, stopRun := context.WithCancel(context.Background())
	runRepo := &fakeRepository{rows: []db.RealtimePublicationOutbox{{ID: id, LeaseToken: &token}}}
	runPub := &fakePublisher{err: context.Canceled, cancel: stopRun}
	done := make(chan struct{})
	go func() {
		(&Dispatcher{Repository: runRepo, Publisher: runPub}).Run(runCtx)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("dispatcher run loop did not stop after in-flight cancellation")
	}
	if len(runRepo.sent) != 0 || len(runRepo.retries) != 0 {
		t.Fatalf("stopped dispatcher updated canceled lease: sent=%v retries=%v", runRepo.sent, runRepo.retries)
	}
}

func TestDispatcherRetriesAtMaxAttemptForTerminalFailure(t *testing.T) {
	id, token := uuid.New(), uuid.New()
	repo := &fakeRepository{rows: []db.RealtimePublicationOutbox{{ID: id, LeaseToken: &token, Attempts: 3}}}
	pub := &fakePublisher{err: errors.New("rejected")}
	d := Dispatcher{Repository: repo, Publisher: pub, Config: Config{MaxAttempts: 3, BatchSize: 1, Lease: time.Minute, RetryBase: time.Second, RetryMax: 4 * time.Second}}

	if err := d.ProcessBatch(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(repo.retries) != 1 || repo.retries[0].delay != 0 {
		t.Fatalf("terminal retry=%+v", repo.retries)
	}
}

type fakeJetStream struct{ message *nats.Msg }

func (f *fakeJetStream) PublishMsg(_ context.Context, message *nats.Msg, _ ...jetstream.PublishOpt) (*jetstream.PubAck, error) {
	f.message = message
	return &jetstream.PubAck{}, nil
}

func TestJetStreamPublisherUsesStableOutboxID(t *testing.T) {
	id := uuid.New()
	js := &fakeJetStream{}
	publisher := JetStreamPublisher{JS: js}
	row := db.RealtimePublicationOutbox{ID: id, Subject: "alarm.opened", Payload: []byte(`{"id":"a"}`)}

	if err := publisher.Publish(context.Background(), row); err != nil {
		t.Fatal(err)
	}
	if js.message.Subject != row.Subject || string(js.message.Data) != string(row.Payload) {
		t.Fatalf("message = %s %s", js.message.Subject, js.message.Data)
	}
	if got := js.message.Header.Get(nats.MsgIdHdr); got != id.String() {
		t.Fatalf("message ID = %q, want %q", got, id)
	}
}
