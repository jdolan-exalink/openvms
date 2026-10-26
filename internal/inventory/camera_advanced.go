package inventory

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/access"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/camconfig"
	"github.com/jdolan-exalink/openvms/internal/frigate"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

// Advanced panels (PRD §33): technical information per backend, read only. It comes from what
// OpenVMS already stores (the engine's camera document, the ONVIF probe, the inventory); no
// live call to the camera is made. Stream URIs are reduced to scheme, host and path.

// CameraAdvanced is the technical information of one camera, by backend.
type CameraAdvanced struct {
	CameraID  uuid.UUID
	Frigate   AdvancedFrigate
	ONVIF     AdvancedONVIF
	Provision AdvancedProvision
	// Inventory is what OpenVMS itself keeps for the camera, needed to complete the Provision
	// panel with the agent's status. It holds the raw ONVIF endpoint, so it is never serialized.
	Inventory camconfig.Inventory `json:"-"`
}

// AdvancedFrigate summarizes the configuration generated for the camera in Frigate.
type AdvancedFrigate struct {
	Available bool
	Reason    string
	Version   string
	// Sections are the camera sections present in the engine's document.
	Sections []string
	// ManagedSections are the sections that hold parameters OpenVMS manages.
	ManagedSections []string
	DesiredRevision int64
	AppliedRevision int64
	SyncState       string
}

// AdvancedONVIFProfile is one media profile from the stored probe.
type AdvancedONVIFProfile struct {
	Token     string
	Name      string
	Encoding  string
	Width     int
	Height    int
	FPS       float64
	Bitrate   int
	StreamURI string
}

// AdvancedONVIF is what the stored ONVIF probe reported.
type AdvancedONVIF struct {
	Configured   bool
	Endpoint     string
	Version      string
	Profile      string
	Manufacturer string
	Model        string
	Firmware     string
	// Profiles are the media profiles; their stream URIs carry no credentials or query.
	Profiles []AdvancedONVIFProfile
	// Capabilities are the has_* flags of the probe; Services the service namespaces;
	// Reported the other scalar and list parameters it reported.
	Capabilities map[string]bool
	Services     []string
	Reported     map[string]any
}

// AdvancedProvision is the technical data of the Provision ISR link.
type AdvancedProvision struct {
	Evidence bool
	Tags     []string
	// Manufacturer and Model are the ONVIF identity that points to a Provision camera.
	Manufacturer string
	Model        string
}

// requireAdvanced is the permission to read the advanced panels: servers.config on the
// camera's server.
func requireAdvanced(c *access.Checker, _ db.GetCameraRow, srv db.GetServerRow) error {
	return c.Require(authz.ServersConfig, access.Server(srv.TenantID, srv.SiteID, srv.ID))
}

// GetCameraAdvanced returns the per-backend technical information of a camera.
func (s *Service) GetCameraAdvanced(ctx context.Context, actor authz.Actor, id uuid.UUID) (CameraAdvanced, error) {
	var cam db.GetCameraRow
	var srv db.GetServerRow
	var t yamlTarget
	var state *db.CameraConfigState
	err := s.tx(ctx, actor, func(q *db.Queries, c *access.Checker) error {
		var err error
		if cam, err = q.GetCamera(ctx, id); err != nil {
			return notFoundOr(err)
		}
		if err = c.Require(authz.CamerasView, cameraResource(cam)); err != nil {
			return err
		}
		if srv, err = q.GetServer(ctx, cam.ServerID); err != nil {
			return notFoundOr(err)
		}
		if err = requireAdvanced(c, cam, srv); err != nil {
			return err
		}
		t.cam = cam
		if err = loadDesired(ctx, q, &t); err != nil {
			return err
		}
		st, err := q.GetCameraConfigState(ctx, id)
		switch {
		case err == nil:
			state = &st
		case !errors.Is(store.Classify(err), store.ErrNotFound):
			return err
		}
		return nil
	})
	if err != nil {
		return CameraAdvanced{}, err
	}
	inv := CameraConfigInventory(cam, srv.Name)
	out := CameraAdvanced{CameraID: cam.ID, Inventory: inv, ONVIF: advancedONVIF(inv), Provision: advancedProvision(inv)}
	out.Frigate = s.advancedFrigate(ctx, srv, cam, t, state)
	return out, nil
}

func (s *Service) advancedFrigate(ctx context.Context, srv db.GetServerRow, cam db.GetCameraRow, t yamlTarget, st *db.CameraConfigState) AdvancedFrigate {
	out := AdvancedFrigate{Sections: []string{}, ManagedSections: []string{}, DesiredRevision: t.revision, SyncState: camconfig.SyncSynced}
	if st != nil {
		out.AppliedRevision, out.SyncState = st.AppliedRevision, st.SyncState
	}
	a, err := s.frigateAdapter(ctx, srv)
	if err != nil {
		out.Reason = "No se pudo conectar con Frigate."
		return out
	}
	out.Version = a.Version()
	doc, err := a.CameraConfig(ctx, cam.RemoteName, false)
	if err != nil {
		out.Reason = "Frigate no devolvió la configuración de la cámara."
		if errors.Is(err, frigate.ErrNotFound) {
			out.Reason = "La cámara no está en Frigate."
		}
		return out
	}
	out.Available = true
	for k := range doc {
		out.Sections = append(out.Sections, k)
	}
	sort.Strings(out.Sections)
	seen := map[string]bool{}
	for _, m := range append(camconfig.ManagedPaths(t.desired), camconfig.ManagedZonePaths(doc["zones"])...) {
		if sec, _, ok := strings.Cut(m.EnginePath, "."); ok && !seen[sec] {
			seen[sec] = true
			out.ManagedSections = append(out.ManagedSections, sec)
		}
	}
	sort.Strings(out.ManagedSections)
	return out
}

func advancedProvision(in camconfig.Inventory) AdvancedProvision {
	out := AdvancedProvision{Evidence: camconfig.ProvisionEvidence(in), Tags: []string{}}
	for _, tag := range in.Tags {
		switch strings.ToLower(tag) {
		case "lpr_provision", "provision", "provision_isr":
			out.Tags = append(out.Tags, tag)
		}
	}
	if out.Evidence {
		var caps struct {
			Manufacturer string `json:"manufacturer"`
			Model        string `json:"model"`
		}
		if json.Unmarshal(in.ONVIFCapabilities, &caps) == nil {
			out.Manufacturer, out.Model = caps.Manufacturer, caps.Model
		}
	}
	return out
}

// advancedONVIF reads the stored probe document. Unknown shapes are skipped, never guessed.
func advancedONVIF(in camconfig.Inventory) AdvancedONVIF {
	out := AdvancedONVIF{
		Configured: in.ONVIFEndpoint != "", Endpoint: camconfig.RedactURL(in.ONVIFEndpoint),
		Version: in.ONVIFVersion, Profile: in.ONVIFProfile,
		Profiles: []AdvancedONVIFProfile{}, Capabilities: map[string]bool{}, Services: []string{}, Reported: map[string]any{},
	}
	var doc map[string]any
	if len(in.ONVIFCapabilities) == 0 || json.Unmarshal(in.ONVIFCapabilities, &doc) != nil {
		return out
	}
	out.Manufacturer, _ = doc["manufacturer"].(string)
	out.Model, _ = doc["model"].(string)
	out.Firmware, _ = doc["firmware_version"].(string)
	for k, v := range doc {
		switch k {
		case "manufacturer", "model", "firmware_version", "serial_number", "hardware_id", "main_stream_url", "sub_stream_url":
			// identity is shown above; the serial and stream URLs are not repeated here.
		case "media_profiles":
			out.Profiles = advancedProfiles(v)
		case "services":
			out.Services = serviceNamespaces(v)
		default:
			if b, ok := v.(bool); ok && strings.HasPrefix(k, "has_") {
				out.Capabilities[k] = b
				continue
			}
			if r, ok := reportedValue(v); ok {
				out.Reported[k] = r
			}
		}
	}
	return out
}

func advancedProfiles(v any) []AdvancedONVIFProfile {
	list, _ := v.([]any)
	out := make([]AdvancedONVIFProfile, 0, len(list))
	for _, e := range list {
		m, ok := e.(map[string]any)
		if !ok {
			continue
		}
		p := AdvancedONVIFProfile{}
		p.Token, _ = m["token"].(string)
		p.Name, _ = m["name"].(string)
		p.Encoding, _ = m["encoding"].(string)
		if f, ok := m["width"].(float64); ok {
			p.Width = int(f)
		}
		if f, ok := m["height"].(float64); ok {
			p.Height = int(f)
		}
		if f, ok := m["frame_rate"].(float64); ok && f > 0 {
			p.FPS = f
		} else if f, ok := m["fps"].(float64); ok {
			p.FPS = f
		}
		if f, ok := m["bitrate"].(float64); ok {
			p.Bitrate = int(f)
		}
		if u, ok := m["stream_uri"].(string); ok {
			p.StreamURI = camconfig.RedactURL(u)
		}
		out = append(out, p)
	}
	return out
}

func serviceNamespaces(v any) []string {
	list, _ := v.([]any)
	out := make([]string, 0, len(list))
	for _, e := range list {
		if m, ok := e.(map[string]any); ok {
			if ns, ok := m["namespace"].(string); ok {
				out = append(out, ns)
			}
		}
	}
	sort.Strings(out)
	return out
}

// reportedValue keeps scalars and lists of scalars, the parameters worth listing; nested
// documents (presets, topics with endpoints) are not echoed.
func reportedValue(v any) (any, bool) {
	switch t := v.(type) {
	case string, float64, bool:
		return t, true
	case []any:
		for _, e := range t {
			switch e.(type) {
			case string, float64, bool:
			default:
				return nil, false
			}
		}
		return t, true
	}
	return nil, false
}
