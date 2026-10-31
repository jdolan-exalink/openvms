package camconfig

// Integration ids and statuses. The Integraciones tab is status and association only: every
// value comes from evidence OpenVMS already has, and nothing here talks to a device.
const (
	IntegrationFrigate             = "frigate"
	IntegrationONVIF               = "onvif"
	IntegrationProvision           = "provision_isr"
	IntegrationLPRProvision        = "lpr_provision"
	IntegrationAnalyticsProvision  = "analytics_provision"
	IntegrationAnalyticsFrigate    = "analytics_frigate"
	IntegrationLPRFrigate          = "lpr_frigate"

	IntegrationConnected     = "connected"
	IntegrationNotConfigured = "not_configured"
	IntegrationError         = "error"
	IntegrationUnknown       = "unknown"
)

// DetailNotServed is the Source.Detail of a Frigate source that answered but does not serve the
// camera, as opposed to being unreachable.
const DetailNotServed = "camera is not served by frigate"

// BuildIntegrations lists the status of each backend and service control a camera can have:
// Frigate, ONVIF, Provision ISR, and the 4 granular controls (LPR Provision-ISR, Analíticas Provision,
// Analíticas Frigate, LPR Frigate). cam must already carry its Sources; link is the Provision agent
// evidence (nil when it could not be read or was not asked for).
func BuildIntegrations(cam Camera, in Inventory, link *ProvisionLink) []Integration {
	mk := func(id, label, status, detail string) Integration {
		return Integration{Name: id, Enabled: status == IntegrationConnected, ID: id, Label: label, Status: status, Detail: detail}
	}
	return []Integration{
		frigateIntegration(cam, mk),
		onvifIntegration(cam, in, mk),
		provisionIntegration(in, link, mk),
		lprProvisionIntegration(in, link, mk),
		analyticsProvisionIntegration(cam, in, link, mk),
		analyticsFrigateIntegration(cam, in, mk),
		lprFrigateIntegration(cam, in, mk),
	}
}

type integrationFn func(id, label, status, detail string) Integration

func sourceOf(cam Camera, name string) (Source, bool) {
	for _, s := range cam.Sources {
		if s.Name == name {
			return s, true
		}
	}
	return Source{}, false
}

func frigateIntegration(cam Camera, mk integrationFn) Integration {
	s, ok := sourceOf(cam, "frigate")
	switch {
	case !ok:
		return mk(IntegrationFrigate, "Frigate", IntegrationNotConfigured, "")
	case s.Status == SourceOK:
		return mk(IntegrationFrigate, "Frigate", IntegrationConnected, "")
	case s.Detail == DetailNotServed:
		return mk(IntegrationFrigate, "Frigate", IntegrationNotConfigured, "La cámara no está en Frigate")
	default:
		return mk(IntegrationFrigate, "Frigate", IntegrationError, "No se pudo leer Frigate")
	}
}

func onvifIntegration(cam Camera, in Inventory, mk integrationFn) Integration {
	if in.ONVIFEndpoint == "" {
		return mk(IntegrationONVIF, "ONVIF", IntegrationNotConfigured, "")
	}
	if s, ok := sourceOf(cam, sourceONVIF); ok {
		if s.Status == SourceOK {
			return mk(IntegrationONVIF, "ONVIF", IntegrationConnected, "")
		}
		return mk(IntegrationONVIF, "ONVIF", IntegrationError, "No se pudo leer la cámara por ONVIF")
	}
	if _, ok := readONVIFCaps(in.ONVIFCapabilities); ok {
		return mk(IntegrationONVIF, "ONVIF", IntegrationConnected, "Capacidades leídas")
	}
	return mk(IntegrationONVIF, "ONVIF", IntegrationUnknown, "Sin verificar")
}

func provisionIntegration(in Inventory, link *ProvisionLink, mk integrationFn) Integration {
	const id, label = IntegrationProvision, "Provision ISR"
	switch {
	case !ProvisionEvidence(in):
		return mk(id, label, IntegrationNotConfigured, "")
	case link == nil || !link.Reachable:
		return mk(id, label, IntegrationUnknown, "Sin datos del agente")
	case !link.Registered:
		return mk(id, label, IntegrationError, "La cámara no está registrada en el agente")
	case link.Online:
		return mk(id, label, IntegrationConnected, "")
	default:
		return mk(id, label, IntegrationError, "Sin keepalive reciente")
	}
}

func lprProvisionIntegration(in Inventory, link *ProvisionLink, mk integrationFn) Integration {
	const id, label = IntegrationLPRProvision, "LPR Provision-ISR"
	isProvLPR := in.LPR || hasAnyTag(in.Tags, "lpr", "lpr_provision")
	if !isProvLPR && !ProvisionEvidence(in) {
		return mk(id, label, IntegrationNotConfigured, "No configurado")
	}
	switch {
	case link == nil || !link.Reachable:
		if isProvLPR {
			return mk(id, label, IntegrationUnknown, "Sin datos del agente")
		}
		return mk(id, label, IntegrationNotConfigured, "Cámara sin LPR Provision")
	case !link.Registered:
		return mk(id, label, IntegrationError, "No registrada en el agente Provision")
	case link.Online:
		return mk(id, label, IntegrationConnected, "En línea con el agente")
	default:
		return mk(id, label, IntegrationError, "Sin keepalive reciente")
	}
}

func analyticsProvisionIntegration(cam Camera, in Inventory, link *ProvisionLink, mk integrationFn) Integration {
	const id, label = IntegrationAnalyticsProvision, "Analíticas Provision"
	caps, haveCaps := readONVIFCaps(in.ONVIFCapabilities)
	hasProvEvidence := ProvisionEvidence(in)
	hasAnalytics := (haveCaps && caps.HasAnalytics) || (haveCaps && caps.HasEvents) || (link != nil && link.EventCount > 0)
	switch {
	case !hasProvEvidence && !hasAnalytics:
		return mk(id, label, IntegrationNotConfigured, "No configurado")
	case haveCaps && caps.HasAnalytics:
		return mk(id, label, IntegrationConnected, "Analítica inteligente Profile M soportada")
	case link != nil && link.Online:
		return mk(id, label, IntegrationConnected, "Receptor de analíticas del agente conectado")
	case hasProvEvidence:
		return mk(id, label, IntegrationUnknown, "Sin verificar")
	default:
		return mk(id, label, IntegrationNotConfigured, "")
	}
}

func analyticsFrigateIntegration(cam Camera, in Inventory, mk integrationFn) Integration {
	const id, label = IntegrationAnalyticsFrigate, "Analíticas Frigate"
	s, ok := sourceOf(cam, "frigate")
	switch {
	case !ok || s.Detail == DetailNotServed:
		return mk(id, label, IntegrationNotConfigured, "La cámara no está en Frigate")
	case s.Status != SourceOK:
		return mk(id, label, IntegrationError, "No se pudo leer Frigate")
	case isTrue(cam.Detection.Enabled):
		return mk(id, label, IntegrationConnected, "Detección activa en Frigate")
	default:
		return mk(id, label, IntegrationConnected, "Conectado a Frigate")
	}
}

func lprFrigateIntegration(cam Camera, in Inventory, mk integrationFn) Integration {
	const id, label = IntegrationLPRFrigate, "LPR Frigate"
	s, ok := sourceOf(cam, "frigate")
	isFrigateLPR := (in.LPR || hasAnyTag(in.Tags, "lpr", "lpr_frigate")) && !hasAnyTag(in.Tags, "lpr_provision")
	switch {
	case !ok || s.Detail == DetailNotServed:
		return mk(id, label, IntegrationNotConfigured, "La cámara no está en Frigate")
	case s.Status != SourceOK:
		return mk(id, label, IntegrationError, "Frigate inaccesible")
	case isFrigateLPR || isTrue(cam.Analytics.LPR):
		return mk(id, label, IntegrationConnected, "LPR activo en Frigate")
	default:
		return mk(id, label, IntegrationNotConfigured, "LPR no habilitado en Frigate")
	}
}
