package camconfig

// Integration ids and statuses. The Integraciones tab is status and association only: every
// value comes from evidence OpenVMS already has, and nothing here talks to a device.
const (
	IntegrationFrigate   = "frigate"
	IntegrationONVIF     = "onvif"
	IntegrationProvision = "provision_isr"

	IntegrationConnected     = "connected"
	IntegrationNotConfigured = "not_configured"
	IntegrationError         = "error"
	IntegrationUnknown       = "unknown"
)

// DetailNotServed is the Source.Detail of a Frigate source that answered but does not serve the
// camera, as opposed to being unreachable.
const DetailNotServed = "camera is not served by frigate"

// BuildIntegrations lists the status of each backend a camera can have: Frigate, ONVIF and
// Provision ISR. cam must already carry its Sources; link is the Provision agent evidence
// (nil when it could not be read or was not asked for).
func BuildIntegrations(cam Camera, in Inventory, link *ProvisionLink) []Integration {
	mk := func(id, label, status, detail string) Integration {
		return Integration{Name: id, Enabled: status == IntegrationConnected, ID: id, Label: label, Status: status, Detail: detail}
	}
	return []Integration{
		frigateIntegration(cam, mk),
		onvifIntegration(cam, in, mk),
		provisionIntegration(in, link, mk),
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
