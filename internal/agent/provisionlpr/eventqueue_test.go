package provisionlpr

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func queueEvent(plate string) LPREvent {
	return LPREvent{
		EventType: "lpr", Source: "provision_isr", CameraID: "11111111-1111-1111-1111-111111111111",
		CameraIP: "192.0.2.10", Plate: plate,
		EventTime: time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC), ReceivedTime: time.Date(2026, 10, 24, 12, 0, 1, 0, time.UTC),
	}
}

func openQueue(t *testing.T, dir string, maxEvents int) *EventQueue {
	t.Helper()
	q, err := OpenEventQueue(dir, maxEvents, 1<<20)
	if err != nil {
		t.Fatalf("OpenEventQueue: %v", err)
	}
	return q
}

func TestEventQueueSurvivesARestartWithTheSameIDsAndSequences(t *testing.T) {
	dir := t.TempDir()
	q := openQueue(t, dir, 100)
	a, err := q.Append(queueEvent("AB123CD"))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := q.Append(queueEvent("EF456GH"))
	if a.Seq != 1 || b.Seq != 2 || a.ID == "" || a.ID == b.ID {
		t.Fatalf("sequence/ID not assigned: %+v %+v", a, b)
	}

	again := openQueue(t, dir, 100)
	got := again.After(0, 10)
	if len(got) != 2 || got[0].ID != a.ID || got[1].ID != b.ID || got[1].Event.Plate != "EF456GH" {
		t.Fatalf("queue lost or changed events across a restart: %+v", got)
	}
}

func TestEventQueueAfterPagesBySequence(t *testing.T) {
	q := openQueue(t, t.TempDir(), 100)
	for _, p := range []string{"A1", "B2", "C3", "D4"} {
		if _, err := q.Append(queueEvent(p)); err != nil {
			t.Fatal(err)
		}
	}
	page := q.After(1, 2)
	if len(page) != 2 || page[0].Seq != 2 || page[1].Seq != 3 {
		t.Fatalf("page after 1 limit 2 = %+v", page)
	}
	if rest := q.After(3, 10); len(rest) != 1 || rest[0].Event.Plate != "D4" {
		t.Fatalf("page after 3 = %+v", rest)
	}
	if none := q.After(4, 10); len(none) != 0 {
		t.Fatalf("expected nothing after the last sequence, got %+v", none)
	}
}

func TestEventQueueAckRemovesAndNeverReusesSequences(t *testing.T) {
	dir := t.TempDir()
	q := openQueue(t, dir, 100)
	for _, p := range []string{"A1", "B2", "C3"} {
		_, _ = q.Append(queueEvent(p))
	}
	removed, err := q.Ack(2)
	if err != nil || removed != 2 {
		t.Fatalf("Ack(2) = %d, %v", removed, err)
	}
	if left := q.After(0, 10); len(left) != 1 || left[0].Seq != 3 {
		t.Fatalf("after ack: %+v", left)
	}
	// Acknowledging everything must not let a restarted queue hand out an old sequence again.
	if _, err := q.Ack(3); err != nil {
		t.Fatal(err)
	}
	again := openQueue(t, dir, 100)
	next, _ := again.Append(queueEvent("D4"))
	if next.Seq != 4 {
		t.Fatalf("sequence was reused after a restart: got %d, want 4", next.Seq)
	}
}

func TestEventQueueAckIsIdempotent(t *testing.T) {
	q := openQueue(t, t.TempDir(), 100)
	_, _ = q.Append(queueEvent("A1"))
	if _, err := q.Ack(1); err != nil {
		t.Fatal(err)
	}
	if removed, err := q.Ack(1); err != nil || removed != 0 {
		t.Fatalf("second Ack(1) = %d, %v; want 0, nil", removed, err)
	}
}

func TestEventQueueDropsTheOldestWhenFullAndCountsTheLoss(t *testing.T) {
	dir := t.TempDir()
	q := openQueue(t, dir, 10)
	for i := 0; i < 25; i++ {
		_, _ = q.Append(queueEvent("P" + string(rune('A'+i%26))))
	}
	st := q.Stats()
	if st.Depth > 10 {
		t.Fatalf("queue grew past its bound: %+v", st)
	}
	if st.Lost == 0 || st.Lost+uint64(st.Depth) != 25 { //nolint:gosec // Depth is a small non-negative count
		t.Fatalf("loss not accounted for: %+v", st)
	}
	// The newest event is always kept.
	got := q.After(0, 100)
	if got[len(got)-1].Seq != 25 {
		t.Fatalf("newest event missing: %+v", got[len(got)-1])
	}

	again := openQueue(t, dir, 10)
	if again.Stats().Lost != st.Lost {
		t.Fatalf("loss counter not persisted: %d vs %d", again.Stats().Lost, st.Lost)
	}
}

func TestEventQueueIgnoresATornLastLineAndKeepsWorking(t *testing.T) {
	dir := t.TempDir()
	q := openQueue(t, dir, 100)
	_, _ = q.Append(queueEvent("A1"))
	_, _ = q.Append(queueEvent("B2"))

	path := filepath.Join(dir, eventQueueFile)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600) //nolint:gosec // path is inside t.TempDir()
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString(`{"seq":3,"id":"abc","event":{"plate":"TOR`) // power cut mid-write
	_ = f.Close()

	again := openQueue(t, dir, 100)
	if got := again.After(0, 10); len(got) != 2 {
		t.Fatalf("expected the two complete events, got %+v", got)
	}
	next, err := again.Append(queueEvent("C3"))
	if err != nil || next.Seq != 3 {
		t.Fatalf("Append after a torn line = %+v, %v", next, err)
	}
	third := openQueue(t, dir, 100)
	if got := third.After(0, 10); len(got) != 3 || got[2].Event.Plate != "C3" {
		t.Fatalf("the torn line corrupted the next write: %+v", got)
	}
}

func TestEventQueueFileIsPrivateAndPreservesBoundedImages(t *testing.T) {
	dir := t.TempDir()
	q := openQueue(t, dir, 100)
	img := "BASE64IMAGEDATA"
	ev := queueEvent("A1")
	ev.PanoramicImage, ev.PlateCrop = &img, &img
	qe, err := q.Append(ev)
	if err != nil {
		t.Fatal(err)
	}
	if qe.Event.PanoramicImage == nil || *qe.Event.PanoramicImage != img {
		t.Fatalf("panoramic image must be preserved: %+v", qe.Event)
	}
	if qe.Event.PlateCrop == nil || *qe.Event.PlateCrop != img {
		t.Fatalf("plate crop must be preserved: %+v", qe.Event)
	}
	info, err := os.Stat(filepath.Join(dir, eventQueueFile))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("queue file mode = %v, want 0600", info.Mode().Perm())
	}

	huge := strings.Repeat("A", 3600<<10)
	ev2 := queueEvent("A2")
	ev2.PanoramicImage = &huge
	qe2, err := q.Append(ev2)
	if err != nil {
		t.Fatal(err)
	}
	if qe2.Event.PanoramicImage != nil {
		t.Fatalf("oversized panoramic image must be dropped: %+v", qe2.Event)
	}
}

// A queue that is full by bytes must drop what it needs to, not everything it holds.
func TestEventQueueByteBoundDropsOnlyWhatIsNeeded(t *testing.T) {
	probe := openQueue(t, t.TempDir(), 100)
	_, _ = probe.Append(queueEvent("A1"))
	line := probe.bytes

	q, err := OpenEventQueue(t.TempDir(), 100, line*3+line/2)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"A1", "B2", "C3", "D4"} {
		if _, err := q.Append(queueEvent(p)); err != nil {
			t.Fatal(err)
		}
	}
	st := q.Stats()
	if st.Depth != 3 || st.Lost != 1 {
		t.Fatalf("depth=%d lost=%d; want 3 kept and only 1 dropped", st.Depth, st.Lost)
	}
	if got := q.After(0, 10); got[0].Event.Plate != "B2" || got[2].Event.Plate != "D4" {
		t.Fatalf("kept the wrong events: %+v", got)
	}
}

// A write that fails half way must not leave bytes that glue themselves to the next record.
func TestEventQueueRecoversFromAPartialWrite(t *testing.T) {
	dir := t.TempDir()
	q := openQueue(t, dir, 100)
	if _, err := q.Append(queueEvent("A1")); err != nil {
		t.Fatal(err)
	}

	realWrite := q.writeLine
	q.writeLine = func(f *os.File, data []byte) error {
		_, _ = f.Write(data[:len(data)/2]) // the disk fills up mid-record
		return errors.New("no space left on device")
	}
	if _, err := q.Append(queueEvent("LOST")); err == nil {
		t.Fatal("expected the failed write to be reported")
	}
	q.writeLine = realWrite
	if _, err := q.Append(queueEvent("C3")); err != nil {
		t.Fatal(err)
	}

	again := openQueue(t, dir, 100)
	got := again.After(0, 10)
	if len(got) != 2 || got[0].Event.Plate != "A1" || got[1].Event.Plate != "C3" {
		t.Fatalf("a partial write corrupted the next record: %+v", got)
	}
	if got[1].Seq != 2 {
		t.Fatalf("the failed event must not consume a sequence: %+v", got[1])
	}
}

// The epoch tells central that this is the same queue it read before: it survives restarts and
// compaction, and a queue whose file was lost has a new one, even if its sequences have caught up.
func TestEventQueueEpochSurvivesRestartsAndChangesWhenTheFileIsLost(t *testing.T) {
	dir := t.TempDir()
	q := openQueue(t, dir, 100)
	epoch := q.Stats().Epoch
	if epoch == "" {
		t.Fatal("a new queue must have an epoch")
	}
	_, _ = q.Append(queueEvent("A1"))
	if _, err := q.Ack(1); err != nil { // compaction rewrites the file
		t.Fatal(err)
	}
	if got := openQueue(t, dir, 100).Stats().Epoch; got != epoch {
		t.Fatalf("epoch changed across a restart: %q -> %q", epoch, got)
	}

	if err := os.Remove(filepath.Join(dir, eventQueueFile)); err != nil {
		t.Fatal(err)
	}
	if got := openQueue(t, dir, 100).Stats().Epoch; got == "" || got == epoch {
		t.Fatalf("a queue whose file was lost must get a new epoch, got %q (old %q)", got, epoch)
	}
}
