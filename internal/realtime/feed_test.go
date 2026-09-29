package realtime

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sort"
	"sync"
	"testing"
	"time"
)

type consumeCall struct {
	stream   string
	subjects []string
}

// fakeSource records Consume calls and lets a test drive deliveries and failures.
type fakeSource struct {
	mu      sync.Mutex
	calls   []consumeCall
	handle  map[string]func(subject string, data []byte)
	results map[string][]error // per stream: errors returned by successive Consume calls
}

func newFakeSource() *fakeSource {
	return &fakeSource{handle: map[string]func(string, []byte){}, results: map[string][]error{}}
}

func (f *fakeSource) Consume(ctx context.Context, stream string, subjects []string, h func(string, []byte)) error {
	f.mu.Lock()
	f.calls = append(f.calls, consumeCall{stream, subjects})
	f.handle[stream] = h
	var err error
	if q := f.results[stream]; len(q) > 0 {
		err, f.results[stream] = q[0], q[1:]
	}
	f.mu.Unlock()
	if err != nil {
		return err
	}
	<-ctx.Done()
	return ctx.Err()
}

func (f *fakeSource) callsSnapshot() []consumeCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]consumeCall(nil), f.calls...)
}

func (f *fakeSource) deliver(stream, subject string, data []byte) {
	f.mu.Lock()
	h := f.handle[stream]
	f.mu.Unlock()
	h(subject, data)
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition not met in time")
}

func quietLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestFeedStartsOneConsumerPerStreamWithItsSubjects(t *testing.T) {
	src := newFakeSource()
	routes := []Route{
		{Stream: "FRIGATE", Subject: "frigate.event.new.*"},
		{Stream: "PLATFORM", Subject: "server.*"},
		{Stream: "PLATFORM", Subject: "camera.*"},
	}
	feed := &Feed{Source: src, Hub: NewHub(HubConfig{Routes: routes}), Routes: routes, Log: quietLog()}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { feed.Run(ctx); close(done) }()
	waitFor(t, func() bool { return len(src.callsSnapshot()) == 2 })
	cancel()
	<-done

	calls := src.callsSnapshot()
	sort.Slice(calls, func(i, j int) bool { return calls[i].stream < calls[j].stream })
	if calls[0].stream != "FRIGATE" || len(calls[0].subjects) != 1 || calls[0].subjects[0] != "frigate.event.new.*" {
		t.Errorf("FRIGATE consumer: %+v", calls[0])
	}
	if calls[1].stream != "PLATFORM" || len(calls[1].subjects) != 2 {
		t.Errorf("PLATFORM consumer must carry both subjects: %+v", calls[1])
	}
}

func TestFeedHandsMessagesToTheHub(t *testing.T) {
	src := newFakeSource()
	var mu sync.Mutex
	var got []string
	routes := []Route{{Stream: "S", Subject: "a.*", Decode: func(subject string, _ []byte) (Message, error) {
		mu.Lock()
		got = append(got, subject)
		mu.Unlock()
		return Message{}, errNoTenant // dropped by the hub after decoding; the call is the evidence
	}}}
	feed := &Feed{Source: src, Hub: NewHub(HubConfig{Routes: routes, Log: quietLog()}), Routes: routes, Log: quietLog()}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go feed.Run(ctx)
	waitFor(t, func() bool { return len(src.callsSnapshot()) == 1 })
	src.deliver("S", "a.x", []byte(`{}`))
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 || got[0] != "a.x" {
		t.Fatalf("hub did not decode the delivered message: %v", got)
	}
}

func TestFeedRestartsAFailedConsumerUntilCancelled(t *testing.T) {
	src := newFakeSource()
	src.results["S"] = []error{errors.New("boom"), errors.New("boom")}
	routes := []Route{{Stream: "S", Subject: "a.*"}}
	feed := &Feed{Source: src, Hub: NewHub(HubConfig{Routes: routes}), Routes: routes, Log: quietLog(), RetryDelay: time.Millisecond}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { feed.Run(ctx); close(done) }()
	waitFor(t, func() bool { return len(src.callsSnapshot()) >= 3 })
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after cancellation")
	}
}

func TestNextRetryDelayBacksOffAndResetsAfterHealthyRun(t *testing.T) {
	const initial, maxDelay, healthy = 2 * time.Second, 30 * time.Second, time.Minute
	got := []time.Duration{}
	d := initial
	for i := 0; i < 6; i++ {
		got = append(got, d)
		d = nextRetryDelay(d, initial, maxDelay, time.Second, healthy)
	}
	want := []time.Duration{2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second, 30 * time.Second, 30 * time.Second}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("delays = %v, want %v", got, want)
		}
	}
	if d := nextRetryDelay(30*time.Second, initial, maxDelay, 2*time.Minute, healthy); d != initial {
		t.Fatalf("after a healthy run delay = %v, want reset to %v", d, initial)
	}
}

func TestFeedBacksOffBetweenFailedConsumers(t *testing.T) {
	src := newFakeSource()
	src.results["S"] = []error{errors.New("1"), errors.New("2"), errors.New("3")}
	routes := []Route{{Stream: "S", Subject: "a.*"}}
	feed := &Feed{Source: src, Hub: NewHub(HubConfig{Routes: routes}), Routes: routes, Log: quietLog(),
		RetryDelay: 20 * time.Millisecond, MaxRetryDelay: 60 * time.Millisecond, HealthyAfter: time.Hour}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	start := time.Now()
	go feed.Run(ctx)
	waitFor(t, func() bool { return len(src.callsSnapshot()) >= 4 })
	// delays before calls 2..4 are 20ms + 40ms + 60ms (capped): at least 120ms in total.
	if el := time.Since(start); el < 120*time.Millisecond {
		t.Fatalf("4 consumers started in %v, want exponential backoff of at least 120ms", el)
	}
}

type fakeConsume struct{ closed chan struct{} }

func (f fakeConsume) Closed() <-chan struct{} { return f.closed }

func TestAwaitConsumeReportsAnyStopOfTheConsumeContext(t *testing.T) {
	fc := fakeConsume{closed: make(chan struct{})}
	res := make(chan error, 1)
	go func() { res <- awaitConsume(context.Background(), fc, make(chan error, 1)) }()
	close(fc.closed)
	select {
	case err := <-res:
		if err == nil {
			t.Fatal("a stopped consume context must be reported as an error")
		}
	case <-time.After(time.Second):
		t.Fatal("awaitConsume did not notice the consume context stopping")
	}
}

func TestAwaitConsumeReturnsContextErrorOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := awaitConsume(ctx, fakeConsume{closed: make(chan struct{})}, make(chan error, 1))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestAwaitConsumeReturnsTerminalError(t *testing.T) {
	failed := make(chan error, 1)
	want := errors.New("consumer deleted")
	failed <- want
	if err := awaitConsume(context.Background(), fakeConsume{closed: make(chan struct{})}, failed); !errors.Is(err, want) {
		t.Fatalf("err = %v, want %v", err, want)
	}
}
