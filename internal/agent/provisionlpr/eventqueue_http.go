package provisionlpr

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
)

const (
	eventsPath    = "/v1/provision-lpr/events"
	eventsAckPath = "/v1/provision-lpr/events/ack"

	defaultEventsPage = 25
	maxAckBody        = 1 << 10
)

// handleEvents lists the queued events after a sequence: GET /v1/provision-lpr/events (everything
// from the start) or GET /v1/provision-lpr/events/after/{seq}. A page holds at most eventsPage events;
// the caller asks again with the last sequence it got. The sequence is in the path because central's
// agent client refuses query strings. Like the captures and registry routes it does not authenticate
// itself: the agent mux puts it behind the bearer token.
func (r *Receiver) handleEvents(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if r.queue == nil {
		http.Error(w, "the event queue is not enabled", http.StatusNotFound)
		return
	}
	var after uint64
	if rest := strings.TrimPrefix(req.URL.Path, eventsPath); rest != "" {
		seq, ok := strings.CutPrefix(rest, "/after/")
		n, err := strconv.ParseUint(seq, 10, 64)
		if !ok {
			http.NotFound(w, req)
			return
		}
		if err != nil {
			http.Error(w, "the sequence must be a number", http.StatusBadRequest)
			return
		}
		after = n
	}
	st := r.queue.Stats()
	writeJSON(w, map[string]any{
		"events":   r.queue.After(after, r.eventsPage),
		"last_seq": st.LastSeq,
		"depth":    st.Depth,
		"lost":     st.Lost,
		"epoch":    st.Epoch,
	})
}

// handleEventsAck removes the events central has stored. It is idempotent: central may repeat it.
func (r *Receiver) handleEventsAck(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if r.queue == nil {
		http.Error(w, "the event queue is not enabled", http.StatusNotFound)
		return
	}
	var body struct {
		Through *uint64 `json:"through"`
	}
	data, err := io.ReadAll(io.LimitReader(req.Body, maxAckBody))
	if err != nil || json.Unmarshal(data, &body) != nil || body.Through == nil {
		http.Error(w, `body must be {"through": <sequence>}`, http.StatusBadRequest)
		return
	}
	removed, err := r.queue.Ack(*body.Through)
	if err != nil {
		r.log.Error("provision-isr event queue ack failed", "error", err)
		http.Error(w, "event queue unavailable", http.StatusInternalServerError)
		return
	}
	st := r.queue.Stats()
	writeJSON(w, map[string]any{"removed": removed, "depth": st.Depth, "last_seq": st.LastSeq})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
