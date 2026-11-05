package provision

import (
	"strings"
	"time"
)

// ReceptionState says whether the analytics a Provision camera or recorder reports are reaching central.
type ReceptionState string

const (
	// ReceptionReceiving: an analytic arrived inside ReceptionWindow.
	ReceptionReceiving ReceptionState = "receiving"
	// ReceptionIdle: the link works as far as central knows, but nothing arrived inside the window.
	ReceptionIdle ReceptionState = "idle"
	// ReceptionDown: central's last attempt to pull the analytics from the agent failed.
	ReceptionDown ReceptionState = "down"
)

// ReceptionWindow is how recent the last analytic must be to count as received. Analytics are sparse by
// nature (a camera at a quiet gate can go hours without one), so the window is a day, not minutes.
const ReceptionWindow = 24 * time.Hour

// Reception derives the state from the time of the last analytic received and the error of central's last
// pull from the agent. A failed pull wins over a recent analytic: the data may be flowing in the past but
// is not flowing now.
func Reception(last *time.Time, ingestError string, now time.Time) ReceptionState {
	switch {
	case strings.TrimSpace(ingestError) != "":
		return ReceptionDown
	case last != nil && now.Sub(*last) <= ReceptionWindow:
		return ReceptionReceiving
	default:
		return ReceptionIdle
	}
}
