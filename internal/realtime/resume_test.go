package realtime_test

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/realtime"
)

func makeMsg(stream string, seq uint64) realtime.Message {
	return realtime.Message{
		Stream: stream,
		Seq:    seq,
		Envelope: realtime.Envelope{
			V:        2,
			Type:     realtime.TypeCameraStatusChanged,
			TenantID: uuid.New(),
		},
	}
}

func TestRingBufferAddAndReplay(t *testing.T) {
	ring := realtime.NewRingBuffer()

	// 1. Replaying non-existent stream returns nil, true (nothing to catch up)
	msgs, ok := ring.Replay("STREAM1", 100)
	if !ok || len(msgs) != 0 {
		t.Fatalf("expected nil, true; got msgs=%v, ok=%v", msgs, ok)
	}

	// 2. Add seq 1..5
	for i := uint64(1); i <= 5; i++ {
		ring.Add(makeMsg("STREAM1", i))
	}

	// Replay from seq 3 -> should get 4, 5
	msgs, ok = ring.Replay("STREAM1", 3)
	if !ok || len(msgs) != 2 {
		t.Fatalf("expected 2 msgs, ok=true; got %d, %v", len(msgs), ok)
	}
	if msgs[0].Seq != 4 || msgs[1].Seq != 5 {
		t.Fatalf("unexpected seqs: %d, %d", msgs[0].Seq, msgs[1].Seq)
	}

	// Replay from seq 5 (up to date) -> nil, true
	msgs, ok = ring.Replay("STREAM1", 5)
	if !ok || len(msgs) != 0 {
		t.Fatalf("expected 0 msgs, ok=true; got %d, %v", len(msgs), ok)
	}

	// Replay from seq 0 (covers seq 1..5)
	msgs, ok = ring.Replay("STREAM1", 0)
	if !ok || len(msgs) != 5 {
		t.Fatalf("expected 5 msgs, ok=true; got %d, %v", len(msgs), ok)
	}
}

func TestRingBufferGapFellOut(t *testing.T) {
	ring := realtime.NewRingBuffer()
	clock := time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC)
	ring.SetClock(func() time.Time { return clock })

	// Add seq 1 at t=0
	ring.Add(makeMsg("S1", 1))

	// Fast-forward 6 minutes and add seq 2
	clock = clock.Add(6 * time.Minute)
	ring.Add(makeMsg("S1", 2))

	// Replay asking for seq 0 (wants seq 1 which expired) -> must return false (resync needed)
	msgs, ok := ring.Replay("S1", 0)
	if ok {
		t.Fatalf("expected ok=false when gap fell out of buffer, got ok=true, msgs=%v", msgs)
	}

	// Replay asking for seq 1 -> gets seq 2
	msgs, ok = ring.Replay("S1", 1)
	if !ok || len(msgs) != 1 || msgs[0].Seq != 2 {
		t.Fatalf("expected seq 2, got msgs=%v, ok=%v", msgs, ok)
	}
}
