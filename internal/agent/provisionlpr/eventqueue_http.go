package provisionlpr

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
)

const (
	eventsPath    = "/v1/provision-lpr/events"
	eventsAckPath = "/v1/provision-lpr/events/ack"

	defaultEventsPage = 100
	maxEventsPage     = 500
	maxAckBody        = 1 << 10
)

// handleEvents lists the queued events after a sequence. Like the captures and registry routes it
// does not authenticate itself: the agent mux puts it behind the bearer token.
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
	if v := req.URL.Query().Get("after"); v != "" {
		n, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			http.Error(w, "after must be a sequence number", http.StatusBadRequest)
			return
		}
		after = n
	}
	limit := defaultEventsPage
	if v := req.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			http.Error(w, "limit must be a positive number", http.StatusBadRequest)
			return
		}
		limit = min(n, maxEventsPage)
	}
	st := r.queue.Stats()
	writeJSON(w, map[string]any{
		"events":   r.queue.After(after, limit),
		"last_seq": st.LastSeq,
		"depth":    st.Depth,
		"lost":     st.Lost,
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
