package camconfig

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jdolan-exalink/openvms/internal/camconfig/catalog"
	"github.com/jdolan-exalink/openvms/internal/frigate"
)

// EngineProvision is Origin.Engine of a camera whose only engine is the analytics built into a
// Provision ISR camera.
const EngineProvision = "provision"

const (
	sourceProvision = "provision"
	// NoteProvisionVehicle explains why "Vehículos" has no finer objects on Provision.
	NoteProvisionVehicle = "Provision no distingue el tipo de vehículo"
)

// Provision labels, as the camera's events report them (see internal/agent/provisionlpr).
const (
	provisionVehicle = "vehicle"
	provisionPlate   = "license_plate"
)

// ProvisionEvidence reports whether the inventory shows the camera is a Provision ISR: its
// tags or its ONVIF identity. It says nothing about whether the link works.
func ProvisionEvidence(in Inventory) bool {
	if hasAnyTag(in.Tags, "lpr_provision", "provision", "provision_isr") {
		return true
	}
	caps, ok := readONVIFCaps(in.ONVIFCapabilities)
	return ok && (strings.Contains(strings.ToLower(caps.Manufacturer), "provision") ||
		strings.Contains(strings.ToLower(caps.Model), "provision"))
}

// ProvisionLink is what the edge agent that receives the camera's REST events says about it.
type ProvisionLink struct {
	// Reachable is false when the agent's status could not be read.
	Reachable bool
	// Registered says the agent knows the camera.
	Registered bool
	// Online says the camera sent a keepalive recently.
	Online      bool
	EventCount  int64
	LastEventAt *time.Time
}

// ProvisionStatusSource reads the agent's view of a camera. It must only read. A nil link
// with a nil error means there is no evidence; an error means the agent could not be asked.
type ProvisionStatusSource interface {
	ProvisionLink(ctx context.Context, in Inventory) (*ProvisionLink, error)
}

// ProvisionAdapter builds the model of a Provision ISR camera. Provision documents no remote
// configuration, so the adapter reads and reports; it never writes to the camera.
type ProvisionAdapter struct {
	// Status is optional; without it the integration status is unknown.
	Status ProvisionStatusSource
}

var _ Adapter = ProvisionAdapter{}

// Read implements Adapter. Every capability needs positive evidence: the LPR analytics (tags
// or Provision identity), and what inventory and ONVIF already know. Detection holds the
// objects the camera really reports, "vehicle" without subtype and the plate.
func (a ProvisionAdapter) Read(ctx context.Context, in Inventory) (Camera, error) {
	if in.CameraID == "" {
		return Camera{}, fmt.Errorf("camconfig: camera id is required")
	}
	cam := baseModel(in)
	cam.Origin.Engine = EngineProvision
	cam.Streams = inventoryStreams(in)
	cam.Sources = append(cam.Sources, Source{Name: sourceProvision, Status: SourceOK})

	var link *ProvisionLink
	if a.Status != nil && ProvisionEvidence(in) {
		var err error
		if link, err = a.Status.ProvisionLink(ctx, in); err != nil {
			link = nil
			cam.Sources[0] = unavailable(sourceProvision)
		}
	}

	caps, haveCaps := readONVIFCaps(in.ONVIFCapabilities)
	if haveCaps {
		cam.Device.Manufacturer, cam.Device.Model = caps.Manufacturer, caps.Model
		cam.Device.Firmware, cam.Device.SerialNumber = caps.Firmware, caps.Serial
	}
	cam.PTZ.Supported = haveCaps && caps.HasPTZ
	cam.Fisheye.Supported = hasAnyTag(in.Tags, "fisheye", "ojo_de_pez")

	deriveModules(&cam, in, nil)
	lpr := false
	if m, ok := cam.Analytics.Module(ModuleLPR); ok {
		lpr = m.Available && m.Engine == ModuleEngineProvision
	}
	if lpr {
		on := true
		cam.Analytics.LPR = &on
		cam.Detection.Enabled = &on
		cam.Detection.Objects = []string{provisionVehicle, provisionPlate}
		cam.Detection.Notes = []string{NoteProvisionVehicle}
	}
	collapseProvision(&cam.Detection)

	events := (link != nil && (link.EventCount > 0 || link.LastEventAt != nil)) || (haveCaps && caps.HasEvents)
	var c []string
	add := func(ok bool, name string) {
		if ok {
			c = append(c, name)
		}
	}
	add(in.ONVIFEndpoint != "", CapONVIF)
	add(cam.PTZ.Supported, CapPTZ)
	add(lpr, CapLPR)
	add(cam.Fisheye.Supported, CapFisheye)
	add(lpr, CapObjectDetection)
	add(haveCaps && caps.HasAnalytics, CapAnalytics)
	add(events, CapEvents)
	if c == nil {
		c = []string{}
	}
	cam.Capabilities = c
	cam.Integrations = append(cam.Integrations, Integration{Name: "lpr", Enabled: lpr})
	cam.Integrations = append(cam.Integrations, BuildIntegrations(cam, in, link)...)
	return cam, nil
}

// collapseProvision fills the catalog view of Detection from the labels Provision reports.
func collapseProvision(d *Detection) {
	sel := catalog.Default().CollapseFromEngine(catalog.EngineProvision, d.Objects)
	d.Selection, d.Categories, d.Raw = sel.Objects, sel.Categories, sel.Raw
}

// ProvisionUnsupported lists the parts of a patch that cannot be applied to a camera whose
// only engine is Provision: everything is configured on the camera itself.
func ProvisionUnsupported(p Patch) []string {
	var out []string
	add := func(path string) { out = append(out, path+": "+ReasonConfiguredOnCam) }
	if d := p.Detection; d != nil && (d.Enabled != nil || d.Objects != nil || d.Selection != nil) {
		add("detection")
	}
	if p.Recording != nil && p.Recording.Enabled != nil {
		add("recording")
	}
	if a := p.Analytics; a != nil {
		if a.LPR != nil {
			add("analytics.lpr")
		}
		for _, id := range sortedModuleIDs(a.Modules) {
			add("analytics.modules." + id)
		}
	}
	if s := p.Streams; s != nil {
		if s.Primary != nil {
			add("streams.primary")
		}
		if s.Detection != nil {
			add("streams.detection")
		}
	}
	return out
}

// Plan implements Adapter. Nothing can change through Provision, so the review lists no
// changes and puts every requested field under Unsupported with the reason.
func (ProvisionAdapter) Plan(_ context.Context, _ Camera, patch Patch) (Review, error) {
	if err := patch.Validate(); err != nil {
		return Review{}, err
	}
	return Review{Changes: []Change{}, Impact: []ImpactItem{}, Unsupported: ProvisionUnsupported(patch.Normalized())}, nil
}

// Apply implements Adapter: Provision cameras are configured on the camera, never from here.
func (ProvisionAdapter) Apply(_ context.Context, patch Patch) error {
	if err := patch.Validate(); err != nil {
		return err
	}
	if u := ProvisionUnsupported(patch.Normalized()); len(u) > 0 {
		return invalid("%s", strings.Join(u, "; "))
	}
	return nil
}

// ComposedAdapter reads a camera through every engine that has evidence for it.
//
// Precedence when both Frigate and Provision serve a camera: Frigate owns identity, origin,
// streams, recording, detection, zones and PTZ; Provision adds the LPR evidence (analytics
// module, capability, integration status, event feed). Device fields Frigate/ONVIF left empty
// are filled from Provision. A camera Frigate does not serve is read through Provision alone.
// Cameras without Provision evidence are read exactly as before, plus integration statuses.
type ComposedAdapter struct {
	Frigate   FrigateAdapter
	Provision ProvisionAdapter
}

var _ Adapter = ComposedAdapter{}

// Read implements Adapter.
func (a ComposedAdapter) Read(ctx context.Context, in Inventory) (Camera, error) {
	fm, err := a.Frigate.Read(ctx, in)
	if err != nil {
		return Camera{}, err
	}
	if !ProvisionEvidence(in) {
		fm.Integrations = append(fm.Integrations, BuildIntegrations(fm, in, nil)...)
		return fm, nil
	}
	pm, err := a.Provision.Read(ctx, in)
	if err != nil {
		return Camera{}, err
	}
	if a.frigateDoesNotServe(ctx, fm, in) {
		src := Source{Name: "frigate", Status: SourceUnavailable, Detail: DetailNotServed}
		pm.Sources = append([]Source{src}, pm.Sources...)
		refreshBackendIntegrations(&pm, in, pm)
		return pm, nil
	}
	return mergeProvision(fm, pm, in), nil
}

// frigateDoesNotServe is true only when Frigate answered that it has no such camera; an
// unreachable Frigate says nothing about it.
func (a ComposedAdapter) frigateDoesNotServe(ctx context.Context, fm Camera, in Inventory) bool {
	if a.Frigate.Engine == nil || EngineReadable(fm) {
		return false
	}
	_, err := a.Frigate.Engine.CameraConfig(ctx, in.RemoteName, false)
	return errors.Is(err, frigate.ErrNotFound)
}

// Plan implements Adapter: Provision-only cameras get the Provision review, the rest Frigate's.
func (a ComposedAdapter) Plan(ctx context.Context, current Camera, patch Patch) (Review, error) {
	if current.Origin.Engine == EngineProvision {
		return a.Provision.Plan(ctx, current, patch)
	}
	return a.Frigate.Plan(ctx, current, patch)
}

// Apply implements Adapter through Frigate. Callers refuse Provision-only cameras first (see
// ProvisionUnsupported); the Provision adapter itself refuses every write.
func (a ComposedAdapter) Apply(ctx context.Context, patch Patch) error {
	return a.Frigate.Apply(ctx, patch)
}

// refreshBackendIntegrations recomputes the Frigate and ONVIF entries from the final sources
// and keeps the Provision entry the Provision adapter built from the agent's evidence.
func refreshBackendIntegrations(cam *Camera, in Inventory, provision Camera) {
	fresh := BuildIntegrations(*cam, in, nil)
	for i, f := range fresh {
		if f.ID != IntegrationProvision {
			continue
		}
		for _, p := range provision.Integrations {
			if p.ID == IntegrationProvision {
				fresh[i] = p
			}
		}
	}
	kept := []Integration{}
	for _, i := range cam.Integrations {
		if i.ID == "" {
			kept = append(kept, i)
		}
	}
	cam.Integrations = append(kept, fresh...)
}

var capabilityOrder = []string{CapONVIF, CapAudio, CapPTZ, CapLPR, CapFisheye, CapObjectDetection, CapRecording, CapAnalytics, CapEvents}

// mergeProvision overlays what Provision adds onto the Frigate model.
func mergeProvision(fm, pm Camera, in Inventory) Camera {
	for _, c := range pm.Capabilities {
		if !slices.Contains(fm.Capabilities, c) {
			fm.Capabilities = append(fm.Capabilities, c)
		}
	}
	slices.SortStableFunc(fm.Capabilities, func(x, y string) int {
		return slices.Index(capabilityOrder, x) - slices.Index(capabilityOrder, y)
	})
	if fm.Device.Manufacturer == "" {
		fm.Device.Manufacturer = pm.Device.Manufacturer
	}
	if fm.Device.Model == "" {
		fm.Device.Model = pm.Device.Model
	}
	if fm.Analytics.LPR == nil {
		fm.Analytics.LPR = pm.Analytics.LPR
	}
	fm.Sources = append(fm.Sources, pm.Sources...)
	refreshBackendIntegrations(&fm, in, pm)
	return fm
}
