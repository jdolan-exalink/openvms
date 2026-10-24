package provisionlpr

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
)

const (
	eventQueueFile = "provision-events.jsonl"

	// DefaultEventQueueMaxEvents and DefaultEventQueueMaxBytes bound what the agent keeps for central
	// while it is unreachable. When full the oldest events are dropped and counted.
	DefaultEventQueueMaxEvents = 10000
	DefaultEventQueueMaxBytes  = 32 << 20

	// maxQueueLine bounds one stored record so a corrupt file cannot make the agent read without limit.
	maxQueueLine = 1 << 20
)

// QueuedEvent is an LPR event waiting for central to collect it. Seq grows by one per event and is
// never reused, so central can ask for "everything after N". ID is random and survives redelivery, so
// central can recognise an event it already stored even if its own cursor was lost.
type QueuedEvent struct {
	Seq   uint64   `json:"seq"`
	ID    string   `json:"id"`
	Event LPREvent `json:"event"`
}

// QueueStats describes the queue for the status route.
type QueueStats struct {
	Depth     int    `json:"depth"`
	OldestSeq uint64 `json:"oldest_seq,omitempty"`
	LastSeq   uint64 `json:"last_seq"`
	Lost      uint64 `json:"lost"`
}

// queueRecord is one line of the file: either the metadata line written at compaction or an event.
type queueRecord struct {
	Meta *queueMeta `json:"meta,omitempty"`
	QueuedEvent
}

type queueMeta struct {
	NextSeq uint64 `json:"next_seq"`
	Lost    uint64 `json:"lost"`
}

type queuedLine struct {
	QueuedEvent
	size int
}

// EventQueue keeps LPR events on disk until central acknowledges them. The file is append-only
// between compactions, written 0600, and a torn last line (power cut mid-write) is discarded on open.
type EventQueue struct {
	mu        sync.Mutex
	path      string
	maxEvents int
	maxBytes  int64
	items     []queuedLine
	bytes     int64
	nextSeq   uint64
	lost      uint64
}

// OpenEventQueue loads the queue stored in dir, creating it when absent.
func OpenEventQueue(dir string, maxEvents int, maxBytes int64) (*EventQueue, error) {
	if maxEvents <= 0 {
		maxEvents = DefaultEventQueueMaxEvents
	}
	if maxBytes <= 0 {
		maxBytes = DefaultEventQueueMaxBytes
	}
	q := &EventQueue{path: filepath.Join(dir, eventQueueFile), maxEvents: maxEvents, maxBytes: maxBytes, nextSeq: 1}
	dirty, err := q.load()
	if err != nil {
		return nil, err
	}
	if dirty {
		if err := q.compactLocked(); err != nil {
			return nil, err
		}
	}
	return q, nil
}

// load reads the file and reports whether it held anything that has to be rewritten (a torn or
// unreadable line).
func (q *EventQueue) load() (bool, error) {
	f, err := os.Open(q.path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("open event queue: %w", err)
	}
	defer func() { _ = f.Close() }()

	dirty := false
	var maxSeq uint64
	r := bufio.NewReaderSize(f, 64<<10)
	for {
		line, readErr := r.ReadBytes('\n')
		complete := len(line) > 0 && line[len(line)-1] == '\n'
		if len(line) > maxQueueLine {
			dirty = true
			q.lost++
		} else if trimmed := bytes.TrimSpace(line); len(trimmed) > 0 {
			var rec queueRecord
			switch {
			case !complete || json.Unmarshal(trimmed, &rec) != nil:
				dirty = true // torn or corrupt: dropped, and the file is rewritten without it
				if complete {
					q.lost++
				}
			case rec.Meta != nil:
				q.lost = max(q.lost, rec.Meta.Lost)
				q.nextSeq = max(q.nextSeq, rec.Meta.NextSeq)
			case rec.Seq == 0:
				dirty = true
			default:
				q.items = append(q.items, queuedLine{QueuedEvent: rec.QueuedEvent, size: len(line)})
				q.bytes += int64(len(line))
				maxSeq = max(maxSeq, rec.Seq)
			}
		}
		if readErr != nil {
			if !errors.Is(readErr, io.EOF) {
				return false, fmt.Errorf("read event queue: %w", readErr)
			}
			break
		}
	}
	q.nextSeq = max(q.nextSeq, maxSeq+1)
	return dirty, nil
}

// Append stores the event and returns it with its sequence and ID. Images are not queued: central
// does not take them in this version and a base64 image would make the queue unbounded in practice.
func (q *EventQueue) Append(ev LPREvent) (QueuedEvent, error) {
	ev.PanoramicImage, ev.PlateCrop = nil, nil
	id, err := newQueueID()
	if err != nil {
		return QueuedEvent{}, err
	}

	q.mu.Lock()
	defer q.mu.Unlock()

	qe := QueuedEvent{Seq: q.nextSeq, ID: id, Event: ev}
	data, err := json.Marshal(qe)
	if err != nil {
		return QueuedEvent{}, fmt.Errorf("encode event: %w", err)
	}
	data = append(data, '\n')
	if len(data) > maxQueueLine {
		return QueuedEvent{}, fmt.Errorf("event too large to queue (%d bytes)", len(data))
	}

	if len(q.items) >= q.maxEvents || q.bytes+int64(len(data)) > q.maxBytes {
		// Drop a tenth at once so a full queue does not rewrite its file for every new event.
		drop := max(1, q.maxEvents/10)
		for q.bytes+int64(len(data)) > q.maxBytes && drop < len(q.items) {
			drop++
		}
		drop = min(drop, len(q.items))
		for _, it := range q.items[:drop] {
			q.bytes -= int64(it.size)
		}
		q.items = append([]queuedLine(nil), q.items[drop:]...)
		q.lost += uint64(drop)
		if err := q.compactLocked(); err != nil {
			return QueuedEvent{}, err
		}
	}

	f, err := os.OpenFile(q.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600) //nolint:gosec // path is the agent's own state file
	if err != nil {
		return QueuedEvent{}, fmt.Errorf("open event queue: %w", err)
	}
	_, werr := f.Write(data)
	if werr == nil {
		werr = f.Sync()
	}
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		return QueuedEvent{}, fmt.Errorf("write event queue: %w", werr)
	}

	q.items = append(q.items, queuedLine{QueuedEvent: qe, size: len(data)})
	q.bytes += int64(len(data))
	q.nextSeq++
	return qe, nil
}

// After returns up to limit events whose sequence is greater than seq, oldest first.
func (q *EventQueue) After(seq uint64, limit int) []QueuedEvent {
	if limit <= 0 {
		limit = 100
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	out := make([]QueuedEvent, 0, min(limit, len(q.items)))
	for _, it := range q.items {
		if it.Seq <= seq {
			continue
		}
		out = append(out, it.QueuedEvent)
		if len(out) == limit {
			break
		}
	}
	return out
}

// Ack removes every event whose sequence is up to and including through. It is idempotent: asking
// again, or asking for less than what is already gone, removes nothing.
func (q *EventQueue) Ack(through uint64) (int, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	n := 0
	for n < len(q.items) && q.items[n].Seq <= through {
		q.bytes -= int64(q.items[n].size)
		n++
	}
	if n == 0 {
		return 0, nil
	}
	q.items = append([]queuedLine(nil), q.items[n:]...)
	if err := q.compactLocked(); err != nil {
		return 0, err
	}
	return n, nil
}

// Stats reports the queue depth and what was lost to the bound.
func (q *EventQueue) Stats() QueueStats {
	q.mu.Lock()
	defer q.mu.Unlock()
	st := QueueStats{Depth: len(q.items), LastSeq: q.nextSeq - 1, Lost: q.lost}
	if len(q.items) > 0 {
		st.OldestSeq = q.items[0].Seq
	}
	return st
}

// compactLocked rewrites the file as the metadata line plus the events still queued. The metadata
// keeps the next sequence and the loss counter when the queue is empty. The replacement is atomic.
func (q *EventQueue) compactLocked() error {
	var buf bytes.Buffer
	meta, _ := json.Marshal(struct {
		Meta queueMeta `json:"meta"`
	}{queueMeta{NextSeq: q.nextSeq, Lost: q.lost}})
	buf.Write(meta)
	buf.WriteByte('\n')
	for _, it := range q.items {
		line, err := json.Marshal(it.QueuedEvent)
		if err != nil {
			return fmt.Errorf("encode event: %w", err)
		}
		buf.Write(line)
		buf.WriteByte('\n')
	}

	tmp := q.path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600) //nolint:gosec // path is the agent's own state file
	if err != nil {
		return fmt.Errorf("compact event queue: %w", err)
	}
	_, werr := f.Write(buf.Bytes())
	if werr == nil {
		werr = f.Sync()
	}
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("compact event queue: %w", werr)
	}
	if err := os.Rename(tmp, q.path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("compact event queue: %w", err)
	}
	return nil
}

func newQueueID() (string, error) {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("event id: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}
