package api

import (
	"errors"
	"net/http"

	"github.com/jdolan-exalink/openvms/internal/access"
	"github.com/jdolan-exalink/openvms/internal/alarms"
	"github.com/jdolan-exalink/openvms/internal/branding"
	"github.com/jdolan-exalink/openvms/internal/clipwatermark"
	"github.com/jdolan-exalink/openvms/internal/events"
	"github.com/jdolan-exalink/openvms/internal/frigate"
	"github.com/jdolan-exalink/openvms/internal/inventory"
	"github.com/jdolan-exalink/openvms/internal/notify"
	"github.com/jdolan-exalink/openvms/internal/rules"
	"github.com/jdolan-exalink/openvms/internal/search"
	"github.com/jdolan-exalink/openvms/internal/store"
)

// errUnauthenticated is returned by handlers that somehow run without an actor.
var errUnauthenticated = errors.New("unauthenticated")

// statusFor maps service errors to the responses declared in the contract.
func statusFor(err error) (int, string, string) {
	var ve *inventory.ValidationError
	var be *branding.ValidationError
	var ave *alarms.ValidationError
	var rve *rules.ValidationError
	var nve *notify.ValidationError
	var we *notify.WahaError
	var fe *inventory.FrigateError
	switch {
	case errors.As(err, &ve):
		return http.StatusBadRequest, "invalid", ve.Msg
	case errors.As(err, &be):
		return http.StatusBadRequest, "invalid", be.Msg
	case errors.As(err, &ave):
		return http.StatusBadRequest, "invalid", ave.Msg
	case errors.As(err, &rve):
		return http.StatusBadRequest, "invalid", rve.Msg
	case errors.As(err, &nve):
		return http.StatusBadRequest, "invalid", nve.Msg
	case errors.Is(err, notify.ErrQRUnavailable):
		return http.StatusConflict, "qr_unavailable", err.Error()
	case errors.As(err, &we):
		return http.StatusBadGateway, "waha_unreachable", "could not reach the WhatsApp service (WAHA)"
	case errors.Is(err, search.ErrQueryTooShort):
		return http.StatusBadRequest, "invalid", err.Error()
	case errors.Is(err, alarms.ErrInvalidTransition):
		return http.StatusConflict, "invalid_transition", err.Error()
	case errors.Is(err, events.ErrInvalidCursor):
		return http.StatusBadRequest, "invalid_cursor", "the pagination cursor is not valid"
	case errors.Is(err, errUnauthenticated):
		return http.StatusUnauthorized, "unauthorized", "authentication required"
	case errors.Is(err, access.ErrForbidden):
		return http.StatusForbidden, "forbidden", "you do not have permission for this action"
	case errors.Is(err, store.ErrNotFound):
		return http.StatusNotFound, "not_found", "not found"
	case errors.Is(err, store.ErrConflict):
		return http.StatusConflict, "conflict", "a resource with the same name already exists"
	case errors.Is(err, inventory.ErrSiteNotEmpty):
		return http.StatusConflict, "site_not_empty", err.Error()
	case errors.Is(err, clipwatermark.ErrNotReady):
		return http.StatusConflict, "not_ready", "the clip watermark job is not ready yet"
	case errors.Is(err, frigate.ErrConfigEditUnsupported):
		return http.StatusConflict, "config_edit_unsupported", err.Error()
	case errors.As(err, &fe):
		msg := "could not reach Frigate"
		if errors.Is(err, frigate.ErrUnauthorized) {
			msg = "Frigate rejected the credentials"
		}
		return http.StatusBadGateway, "frigate_unreachable", msg + ": " + fe.Err.Error()
	}
	return http.StatusInternalServerError, "internal", "internal server error"
}
