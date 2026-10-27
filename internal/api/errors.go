package api

import (
	"errors"
	"net/http"

	"github.com/jdolan-exalink/openvms/internal/access"
	"github.com/jdolan-exalink/openvms/internal/agentca"
	"github.com/jdolan-exalink/openvms/internal/agentenroll"
	"github.com/jdolan-exalink/openvms/internal/alarms"
	"github.com/jdolan-exalink/openvms/internal/branding"
	"github.com/jdolan-exalink/openvms/internal/clipwatermark"
	"github.com/jdolan-exalink/openvms/internal/events"
	"github.com/jdolan-exalink/openvms/internal/faces"
	"github.com/jdolan-exalink/openvms/internal/frigate"
	"github.com/jdolan-exalink/openvms/internal/inventory"
	"github.com/jdolan-exalink/openvms/internal/maps"
	"github.com/jdolan-exalink/openvms/internal/notify"
	"github.com/jdolan-exalink/openvms/internal/provision"
	"github.com/jdolan-exalink/openvms/internal/rules"
	"github.com/jdolan-exalink/openvms/internal/search"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/visionai"
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
	var mve *maps.ValidationError
	var pve *provision.ValidationError
	var vae *visionai.ValidationError
	var we *notify.WahaError
	var fe *inventory.FrigateError
	var fve *faces.ValidationError
	var fre *faces.RemoteError
	switch {
	case errors.As(err, &ve):
		return http.StatusBadRequest, "invalid", ve.Msg
	case errors.As(err, &be):
		return http.StatusBadRequest, "invalid", be.Msg
	case errors.As(err, &fve):
		return http.StatusBadRequest, "invalid", fve.Msg
	case errors.As(err, &fre):
		return http.StatusBadGateway, "frigate_unreachable", "could not reach Frigate: " + fre.Reason
	case errors.As(err, &ave):
		return http.StatusBadRequest, "invalid", ave.Msg
	case errors.As(err, &rve):
		return http.StatusBadRequest, "invalid", rve.Msg
	case errors.As(err, &nve):
		return http.StatusBadRequest, "invalid", nve.Msg
	case errors.As(err, &mve):
		return http.StatusBadRequest, "invalid", mve.Msg
	case errors.As(err, &pve):
		return http.StatusBadRequest, "invalid", pve.Msg
	case errors.As(err, &vae):
		return http.StatusBadRequest, "invalid", vae.Msg
	case errors.Is(err, errVisionAIUnavailable):
		return http.StatusServiceUnavailable, "unavailable", "vision ai is not available"
	case errors.Is(err, provision.ErrAgentTLSNotConfigured):
		return http.StatusNotFound, "agent_tls_not_configured", "agent TLS is not configured"
	case errors.Is(err, provision.ErrAgentInstallJobNotFound):
		return http.StatusNotFound, "not_found", "agent install job not found"
	case errors.Is(err, provision.ErrAgentInstallBusy), errors.Is(err, provision.ErrAgentAlreadyProvisioned):
		return http.StatusConflict, "conflict", "agent is already registered or an install is active"
	case errors.Is(err, provision.ErrAgentUpdateTLSAlreadyConfigured):
		return http.StatusConflict, "conflict", "agent TLS configuration changed; refresh server state before retrying"
	case errors.Is(err, provision.ErrAgentUnprovisioned):
		return http.StatusNotFound, "agent_unprovisioned", "server has no registered agent"
	case errors.Is(err, provision.ErrAgentConnectUnreachable):
		return http.StatusBadGateway, "agent_unreachable", "OpenVMS could not reach the agent at that address"
	case errors.Is(err, provision.ErrAgentConnectUnauthorized):
		return http.StatusUnprocessableEntity, "agent_token_rejected", "the agent refused the token"
	case errors.Is(err, provision.ErrAgentConnectForbidden):
		return http.StatusUnprocessableEntity, "agent_token_scope", "the token is valid but does not have the read scope"
	case errors.Is(err, provision.ErrAgentConnectNotAnAgent):
		return http.StatusUnprocessableEntity, "not_an_agent", "that address answered but it is not an OpenVMS agent"
	case errors.Is(err, provision.ErrAgentDiscoveryUnsupported):
		return http.StatusConflict, "discovery_unsupported", "agent discovery is unsupported or the interface is not allowed"
	case errors.Is(err, provision.ErrAgentDiscoveryUnknown):
		return http.StatusBadGateway, "discovery_failed", "agent discovery failed"
	case errors.Is(err, provision.ErrAgentBackupUnsupported):
		return http.StatusConflict, "agent_backup_unsupported", "update the agent to enable backups"
	case errors.Is(err, provision.ErrAgentProbeUnavailable), errors.Is(err, provision.ErrAgentProbeFailed):
		return http.StatusBadGateway, "onvif_probe_failed", "could not complete the ONVIF probe"
	case errors.Is(err, provision.ErrAgentUnavailable):
		return http.StatusBadGateway, "agent_unavailable", "registered agent is unavailable for discovery"
	case errors.Is(err, provision.ErrBusy):
		return http.StatusConflict, "conflict", err.Error()
	case errors.Is(err, provision.ErrNotFound):
		return http.StatusNotFound, "not_found", "not found"
	case errors.Is(err, inventory.ErrCameraGroupVersionConflict):
		return http.StatusConflict, "conflict", err.Error()
	case errors.Is(err, rules.ErrVersionConflict):
		return http.StatusConflict, "conflict", err.Error()
	case errors.Is(err, maps.ErrOptimisticLockConflict):
		return http.StatusConflict, "conflict", err.Error()
	case errors.Is(err, notify.ErrQRUnavailable):
		return http.StatusConflict, "qr_unavailable", err.Error()
	case errors.As(err, &we):
		return http.StatusBadGateway, "waha_unreachable", "could not reach the WhatsApp service (WAHA)"
	case errors.Is(err, search.ErrQueryTooShort):
		return http.StatusBadRequest, "invalid", err.Error()
	case errors.Is(err, alarms.ErrInvalidTransition):
		return http.StatusConflict, "invalid_transition", err.Error()
	case errors.Is(err, events.ErrInvalidCursor), errors.Is(err, alarms.ErrInvalidCursor), errors.Is(err, faces.ErrInvalidCursor):
		return http.StatusBadRequest, "invalid_cursor", "the pagination cursor is not valid"
	case errors.Is(err, agentenroll.ErrInvalidToken):
		// One answer for unknown, used and expired tokens, so tokens cannot be probed.
		return http.StatusUnauthorized, "unauthorized", "invalid enrollment token"
	case errors.Is(err, agentca.ErrInvalidCSR):
		return http.StatusBadRequest, "invalid_csr", "the certificate request is not valid"
	case errors.Is(err, errUnauthenticated):
		return http.StatusUnauthorized, "unauthorized", "authentication required"
	case errors.Is(err, access.ErrForbidden):
		return http.StatusForbidden, "forbidden", "you do not have permission for this action"
	case errors.Is(err, store.ErrNotFound):
		return http.StatusNotFound, "not_found", "not found"
	case errors.Is(err, store.ErrConflict):
		return http.StatusConflict, "conflict", "a resource with the same name already exists"
	case errors.Is(err, inventory.ErrRevisionConflict):
		return http.StatusConflict, "revision_conflict", err.Error()
	case errors.Is(err, inventory.ErrDuplicateServer):
		return http.StatusConflict, "server_duplicate", err.Error()
	case errors.Is(err, inventory.ErrSiteNotEmpty):
		return http.StatusConflict, "site_not_empty", err.Error()
	case errors.Is(err, inventory.ErrLastSite):
		return http.StatusConflict, "last_site", err.Error()
	case errors.Is(err, clipwatermark.ErrNotReady):
		return http.StatusConflict, "not_ready", "the clip watermark job is not ready yet"
	case errors.As(err, &fe):
		msg := "could not reach Frigate"
		if errors.Is(err, frigate.ErrUnauthorized) {
			msg = "Frigate rejected the credentials"
		}
		return http.StatusBadGateway, "frigate_unreachable", msg + ": " + fe.Err.Error()
	}
	return http.StatusInternalServerError, "internal", "internal server error"
}
