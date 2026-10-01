package maps

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

// MaxUserPrefsBytes bounds one preference blob. Preferences are a UI convenience, so a
// client must never be able to park an arbitrary payload in map_user_prefs through this
// endpoint.
const MaxUserPrefsBytes = 16 << 10

// The contract declares these enums, but oapi-codegen does not reject unknown members
// while decoding, so the server re-checks them before storing anything.
var (
	validFocusModes = map[string]bool{"none": true, "current-site": true}
	validPriorities = map[string]bool{"alert": true, "detection": true}
	validStatuses   = map[string]bool{
		"ONLINE": true, "DEGRADED": true, "OFFLINE": true, "NO_SIGNAL": true,
		"RECORDING_ERROR": true, "UNREACHABLE": true, "ALARM": true,
	}
	validCameraTypes = map[string]bool{
		"fixed": true, "dome": true, "ptz": true, "fisheye": true, "lpr": true,
	}
)

// prefsEnvelope decodes only the fields the server owns. Layers and filters are kept as
// raw JSON: their shape is enforced by the generated request type, and duplicating those
// structs here would create a second source of truth to keep in sync with the contract.
type prefsEnvelope struct {
	FocusMode *string         `json:"focus_mode,omitempty"`
	Filters   json.RawMessage `json:"filters,omitempty"`
}

type prefsFilterEnums struct {
	Status      []string `json:"status,omitempty"`
	Priority    []string `json:"priority,omitempty"`
	CameraTypes []string `json:"camera_types,omitempty"`
}

// GetUserPrefs returns the caller's stored map preferences. A user who never saved
// anything gets nil, which the handler reports as an empty object.
func (s *Service) GetUserPrefs(ctx context.Context, actor authz.Actor) (json.RawMessage, error) {
	var raw json.RawMessage
	err := s.Store.Tx(ctx, store.ScopeFor(actor), func(q *db.Queries) error {
		row, err := q.GetMapUserPrefs(ctx, actor.UserID)
		if err != nil {
			return store.Classify(err)
		}
		raw = row.Prefs
		return nil
	})
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil
	}
	return raw, err
}

// SaveUserPrefs replaces the caller's stored map preferences. It never merges: the blob
// the client sends is exactly what the next read returns.
func (s *Service) SaveUserPrefs(ctx context.Context, actor authz.Actor, raw json.RawMessage) error {
	if err := validatePrefs(raw); err != nil {
		return err
	}
	return s.Store.Tx(ctx, store.ScopeFor(actor), func(q *db.Queries) error {
		_, err := q.UpsertMapUserPrefs(ctx, db.UpsertMapUserPrefsParams{
			UserID:   actor.UserID,
			TenantID: actor.TenantID,
			Prefs:    raw,
		})
		return store.Classify(err)
	})
}

func validatePrefs(raw json.RawMessage) error {
	if len(raw) > MaxUserPrefsBytes {
		return &ValidationError{Msg: fmt.Sprintf("map preferences exceed %d bytes", MaxUserPrefsBytes)}
	}
	var env prefsEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return &ValidationError{Msg: "invalid map preferences: " + err.Error()}
	}
	if env.FocusMode != nil && !validFocusModes[*env.FocusMode] {
		return &ValidationError{Msg: fmt.Sprintf("unknown focus_mode %q", *env.FocusMode)}
	}
	if len(env.Filters) == 0 {
		return nil
	}
	var f prefsFilterEnums
	if err := json.Unmarshal(env.Filters, &f); err != nil {
		return &ValidationError{Msg: "invalid filters: " + err.Error()}
	}
	for _, v := range f.Status {
		if !validStatuses[v] {
			return &ValidationError{Msg: fmt.Sprintf("unknown status filter %q", v)}
		}
	}
	for _, v := range f.Priority {
		if !validPriorities[v] {
			return &ValidationError{Msg: fmt.Sprintf("unknown priority filter %q", v)}
		}
	}
	for _, v := range f.CameraTypes {
		if !validCameraTypes[v] {
			return &ValidationError{Msg: fmt.Sprintf("unknown camera_types filter %q", v)}
		}
	}
	return nil
}
