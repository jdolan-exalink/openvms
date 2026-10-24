package provisionlpr

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func queuedPlates(q *EventQueue) []string {
	var plates []string
	for _, qe := range q.After(0, 100) {
		plates = append(plates, qe.Event.Plate)
	}
	return plates
}

func TestReceiverQueuesOnlyEventsFromRegisteredCamerasAndOnlyOnce(t *testing.T) {
	f := newRegistryFixture(t)
	q, err := OpenEventQueue(t.TempDir(), 100, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	f.r.SetEventQueue(q)
	_ = f.reg.Upsert(RegisteredCamera{CameraID: "11111111-2222-3333-4444-555555555555", IP: "10.0.15.107"})

	// A registered camera, twice inside the dedup window: one queued event.
	f.send(http.MethodPost, "/v1/provision-lpr/event", "10.0.15.107:5000", `{"plate":"AF123BC"}`, nil)
	f.send(http.MethodPost, "/v1/provision-lpr/event", "10.0.15.107:5000", `{"plate":"AF123BC"}`, nil)
	// An address that is not registered (the registry does not enforce yet): accepted, never queued.
	f.send(http.MethodPost, "/v1/provision-lpr/event", "10.0.15.99:5000", `{"plate":"ZZ999ZZ"}`, nil)

	f.emitted(t) // waits until everything sent before has been handled

	got := queuedPlates(q)
	want := []string{"AF123BC"}
	if len(got) < 1 || got[0] != want[0] {
		t.Fatalf("queued = %v, want %v first", got, want)
	}
	for _, p := range got {
		if p == "ZZ999ZZ" {
			t.Fatalf("an unidentified source was queued for central: %v", got)
		}
	}
	count := 0
	for _, p := range got {
		if p == "AF123BC" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("the duplicate reading was queued %d times: %v", count, got)
	}
	if qe := q.After(0, 1)[0]; qe.Event.CameraID != "11111111-2222-3333-4444-555555555555" {
		t.Fatalf("queued event is not filed under the registered camera: %+v", qe.Event)
	}
}

func TestReceiverWithoutAQueueStillWorks(t *testing.T) {
	f := newRegistryFixture(t)
	_ = f.reg.Upsert(RegisteredCamera{CameraID: "cam", IP: "10.0.15.107"})
	if rec := f.send(http.MethodPost, "/v1/provision-lpr/event", "10.0.15.107:5000", `{"plate":"AF123BC"}`, nil); rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
}

func queueRoutesFixture(t *testing.T) (*Receiver, *EventQueue) {
	t.Helper()
	q, err := OpenEventQueue(t.TempDir(), 100, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"A1", "B2", "C3"} {
		if _, err := q.Append(queueEvent(p)); err != nil {
			t.Fatal(err)
		}
	}
	r := NewReceiver(DefaultConfig(), nil)
	r.SetEventQueue(q)
	return r, q
}

func call(r *Receiver, method, target, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestEventsRouteReturnsTheQueuedEventsAfterASequence(t *testing.T) {
	r, q := queueRoutesFixture(t)
	r.eventsPage = 1
	rec := call(r, http.MethodGet, "/v1/provision-lpr/events/after/1", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	var out struct {
		Events  []QueuedEvent `json:"events"`
		LastSeq uint64        `json:"last_seq"`
		Depth   int           `json:"depth"`
		Lost    uint64        `json:"lost"`
		Epoch   string        `json:"epoch"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Events) != 1 || out.Events[0].Seq != 2 || out.Events[0].Event.Plate != "B2" || out.Events[0].ID == "" {
		t.Fatalf("events = %+v", out.Events)
	}
	if out.Epoch == "" || out.Epoch != q.Stats().Epoch {
		t.Fatalf("epoch = %q, want the queue's %q", out.Epoch, q.Stats().Epoch)
	}
	if out.LastSeq != 3 || out.Depth != 3 || out.Lost != 0 {
		t.Fatalf("stats = %+v", out)
	}
}

func TestAckRouteRemovesTheEventsAndIsIdempotent(t *testing.T) {
	r, q := queueRoutesFixture(t)
	for i := 0; i < 2; i++ {
		rec := call(r, http.MethodPost, "/v1/provision-lpr/events/ack", `{"through":2}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("ack #%d status = %d: %s", i+1, rec.Code, rec.Body)
		}
	}
	if left := q.After(0, 10); len(left) != 1 || left[0].Seq != 3 {
		t.Fatalf("queue after ack = %+v", left)
	}
}

func TestQueueRoutesRejectBadInputAndMethods(t *testing.T) {
	r, q := queueRoutesFixture(t)
	cases := []struct {
		method, target, body string
		want                 int
	}{
		{http.MethodGet, "/v1/provision-lpr/events/after/abc", "", http.StatusBadRequest},
		{http.MethodGet, "/v1/provision-lpr/events/after/", "", http.StatusBadRequest},
		{http.MethodGet, "/v1/provision-lpr/events/after/-1", "", http.StatusBadRequest},
		{http.MethodGet, "/v1/provision-lpr/events/other", "", http.StatusNotFound},
		{http.MethodPost, "/v1/provision-lpr/events", "", http.StatusMethodNotAllowed},
		{http.MethodGet, "/v1/provision-lpr/events/ack", "", http.StatusMethodNotAllowed},
		{http.MethodPost, "/v1/provision-lpr/events/ack", `not json`, http.StatusBadRequest},
		{http.MethodPost, "/v1/provision-lpr/events/ack", `{}`, http.StatusBadRequest},
	}
	for _, c := range cases {
		if got := call(r, c.method, c.target, c.body).Code; got != c.want {
			t.Errorf("%s %s %q = %d, want %d", c.method, c.target, c.body, got, c.want)
		}
	}
	if len(q.After(0, 10)) != 3 {
		t.Fatal("a rejected request changed the queue")
	}
}

func TestQueueRoutesAreNotFoundWithoutAQueueAndStatusReportsTheQueue(t *testing.T) {
	bare := NewReceiver(DefaultConfig(), nil)
	if got := call(bare, http.MethodGet, "/v1/provision-lpr/events", "").Code; got != http.StatusNotFound {
		t.Fatalf("events without a queue = %d, want 404", got)
	}
	if got := call(bare, http.MethodPost, "/v1/provision-lpr/events/ack", `{"through":1}`).Code; got != http.StatusNotFound {
		t.Fatalf("ack without a queue = %d, want 404", got)
	}

	r, _ := queueRoutesFixture(t)
	var status struct {
		Events *QueueStats `json:"events"`
	}
	if err := json.Unmarshal(call(r, http.MethodGet, "/v1/provision-lpr/status", "").Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if status.Events == nil || status.Events.Depth != 3 || status.Events.LastSeq != 3 {
		t.Fatalf("status events = %+v", status.Events)
	}
}
