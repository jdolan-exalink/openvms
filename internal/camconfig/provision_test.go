package camconfig

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/jdolan-exalink/openvms/internal/frigate"
)

type fakeLink struct {
	link *ProvisionLink
	err  error
}

func (f fakeLink) ProvisionLink(context.Context, Inventory) (*ProvisionLink, error) {
	return f.link, f.err
}

func provisionInv() Inventory {
	in := baseInv()
	in.Tags = []string{"lpr_provision"}
	in.ONVIFCapabilities = []byte(`{"manufacturer":"Provision-ISR","model":"I6-380LPR"}`)
	return in
}

func integration(t *testing.T, cam Camera, id string) Integration {
	t.Helper()
	for _, i := range cam.Integrations {
		if i.ID == id {
			return i
		}
	}
	t.Fatalf("no %s integration in %+v", id, cam.Integrations)
	return Integration{}
}

func TestProvisionReadsOnlyWhatItReports(t *testing.T) {
	cam, err := ProvisionAdapter{}.Read(context.Background(), provisionInv())
	if err != nil {
		t.Fatal(err)
	}
	if cam.Origin.Engine != EngineProvision || cam.Device.Manufacturer != "Provision-ISR" {
		t.Fatalf("origin/device: %+v %+v", cam.Origin, cam.Device)
	}
	// Vehicle without subtype: Vehículos is selected through the generic object, nothing finer.
	if !slices.Equal(cam.Detection.Selection, []string{"vehicle.generic", "other.license_plate"}) || !slices.Contains(cam.Detection.Categories, "vehicle") {
		t.Fatalf("detection: %+v", cam.Detection)
	}
	for _, invented := range []string{"vehicle.car", "vehicle.truck", "vehicle.bus", "vehicle.motorcycle", "person"} {
		if slices.Contains(cam.Detection.Selection, invented) {
			t.Fatalf("invented %s: %+v", invented, cam.Detection)
		}
	}
	if len(cam.Detection.Raw) != 0 || !slices.Equal(cam.Detection.Notes, []string{"Provision no distingue el tipo de vehículo"}) {
		t.Fatalf("notes/raw: %+v", cam.Detection)
	}
	if cam.Detection.Enabled == nil || !*cam.Detection.Enabled {
		t.Fatal("detection should be on: the camera reports vehicles")
	}
	// Positive evidence only: no PTZ, no audio, no recording, no events without evidence.
	if got := strings.Join(cam.Capabilities, ","); got != "onvif,lpr,object_detection" {
		t.Fatalf("capabilities = %s", got)
	}
	if cam.Recording.Enabled != nil {
		t.Fatal("recording is unknown for Provision")
	}
	if m, ok := cam.Analytics.Module(ModuleLPR); !ok || !m.Available || m.Writable || m.Engine != ModuleEngineProvision || m.Reason != ReasonConfiguredOnCam {
		t.Fatalf("lpr module: %+v", m)
	}
	if cam.Streams.Primary == nil || cam.Streams.Primary.Name != "gate" {
		t.Fatalf("streams come from inventory: %+v", cam.Streams)
	}
}

func TestProvisionWithoutEvidenceReportsNothing(t *testing.T) {
	in := baseInv()
	in.Tags = nil
	in.ONVIFCapabilities = nil
	cam, err := ProvisionAdapter{}.Read(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if cam.Detection.Enabled != nil || len(cam.Detection.Objects) != 0 || slices.Contains(cam.Capabilities, CapLPR) {
		t.Fatalf("invented evidence: %+v %v", cam.Detection, cam.Capabilities)
	}
	if i := integration(t, cam, IntegrationProvision); i.Status != IntegrationNotConfigured {
		t.Fatalf("integration: %+v", i)
	}
}

func TestProvisionEventFeedIsACapabilityOnlyWithEvents(t *testing.T) {
	cases := map[string]struct {
		link *ProvisionLink
		want bool
	}{
		"events seen": {&ProvisionLink{Reachable: true, Registered: true, EventCount: 3}, true},
		"no events":   {&ProvisionLink{Reachable: true, Registered: true, Online: true}, false},
		"no link":     {nil, false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			cam, _ := ProvisionAdapter{Status: fakeLink{link: tc.link}}.Read(context.Background(), provisionInv())
			if got := slices.Contains(cam.Capabilities, CapEvents); got != tc.want {
				t.Fatalf("events capability = %v: %v", got, cam.Capabilities)
			}
		})
	}
}

func TestProvisionIntegrationStatusesFromEvidence(t *testing.T) {
	cases := map[string]struct {
		src  ProvisionStatusSource
		want string
	}{
		"no source":      {nil, IntegrationUnknown},
		"agent down":     {fakeLink{err: errors.New("dial tcp 10.0.0.1: refused")}, IntegrationUnknown},
		"unreachable":    {fakeLink{link: &ProvisionLink{}}, IntegrationUnknown},
		"not registered": {fakeLink{link: &ProvisionLink{Reachable: true}}, IntegrationError},
		"stale":          {fakeLink{link: &ProvisionLink{Reachable: true, Registered: true}}, IntegrationError},
		"online":         {fakeLink{link: &ProvisionLink{Reachable: true, Registered: true, Online: true}}, IntegrationConnected},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			cam, _ := ProvisionAdapter{Status: tc.src}.Read(context.Background(), provisionInv())
			i := integration(t, cam, IntegrationProvision)
			if i.Status != tc.want || i.Enabled != (tc.want == IntegrationConnected) || i.Label != "Provision ISR" {
				t.Fatalf("integration = %+v, want %s", i, tc.want)
			}
			if strings.Contains(i.Detail, "10.0.0.1") {
				t.Fatalf("detail leaks the cause: %q", i.Detail)
			}
		})
	}
}

func TestProvisionRefusesEveryWriteWithTheReason(t *testing.T) {
	on := true
	obj := []string{"car"}
	patches := map[string]Patch{
		"detection":  {Detection: &DetectionPatch{Enabled: &on}},
		"selection":  {Detection: &DetectionPatch{Selection: &[]string{"vehicle"}}},
		"objects":    {Detection: &DetectionPatch{Objects: &obj}},
		"recording":  {Recording: &RecordingPatch{Enabled: &on}},
		"lpr":        {Analytics: &AnalyticsPatch{LPR: &on}},
		"module":     {Analytics: &AnalyticsPatch{Modules: map[string]*ModulePatch{ModuleLPR: {Enabled: &on}}}},
		"stream":     {Streams: &StreamsPatch{Primary: &VideoPatch{Codec: strPtr("h264")}}},
		"stream det": {Streams: &StreamsPatch{Detection: &VideoPatch{Codec: strPtr("h264")}}},
	}
	for name, p := range patches {
		t.Run(name, func(t *testing.T) {
			rev, err := ProvisionAdapter{}.Plan(context.Background(), Camera{}, p)
			if err != nil {
				t.Fatal(err)
			}
			if len(rev.Changes) != 0 || len(rev.Unsupported) == 0 {
				t.Fatalf("review: %+v", rev)
			}
			for _, u := range rev.Unsupported {
				if !strings.HasSuffix(u, "Se configura en la cámara") {
					t.Fatalf("reason: %q", u)
				}
			}
			var ve *ValidationError
			if err := (ProvisionAdapter{}).Apply(context.Background(), p); !errors.As(err, &ve) || !strings.Contains(ve.Msg, "Se configura en la cámara") {
				t.Fatalf("apply: %v", err)
			}
		})
	}
}

func strPtr(s string) *string { return &s }

func TestComposedWithoutProvisionIsFrigateAsBefore(t *testing.T) {
	in := baseInv()
	f := FrigateAdapter{Engine: fakeEngine{doc: parse(t, twoInputDoc)}}
	want, _ := f.Read(context.Background(), in)
	got, err := ComposedAdapter{Frigate: f}.Read(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got.Capabilities, want.Capabilities) || got.Origin != want.Origin || len(got.Sources) != len(want.Sources) {
		t.Fatalf("model changed: %+v vs %+v", got, want)
	}
	if fr := integration(t, got, IntegrationFrigate); fr.Status != IntegrationConnected {
		t.Fatalf("frigate: %+v", fr)
	}
	if on := integration(t, got, IntegrationONVIF); on.Status != IntegrationConnected {
		t.Fatalf("onvif: %+v", on)
	}
	if pr := integration(t, got, IntegrationProvision); pr.Status != IntegrationNotConfigured {
		t.Fatalf("provision: %+v", pr)
	}
}

// Frigate serves the video and the detection; Provision adds the LPR evidence.
func TestComposedMergesFrigateAndProvision(t *testing.T) {
	in := provisionInv()
	in.ONVIFCapabilities = []byte(`{"manufacturer":"Provision-ISR","model":"I6","has_events":true}`)
	link := &ProvisionLink{Reachable: true, Registered: true, Online: true, EventCount: 7}
	ad := ComposedAdapter{
		Frigate:   FrigateAdapter{Engine: fakeEngine{doc: parse(t, twoInputDoc)}},
		Provision: ProvisionAdapter{Status: fakeLink{link: link}},
	}
	cam, err := ad.Read(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if cam.Origin.Engine != "frigate" {
		t.Fatalf("engine = %s", cam.Origin.Engine)
	}
	// Frigate's detection wins: person and car, no Provision objects mixed in.
	if !slices.Equal(cam.Detection.Objects, []string{"person", "car"}) || slices.Contains(cam.Detection.Selection, "vehicle.generic") {
		t.Fatalf("detection: %+v", cam.Detection)
	}
	if cam.Streams.Detection == nil || cam.Streams.Detection.Width != 640 {
		t.Fatalf("streams: %+v", cam.Streams)
	}
	if m, _ := cam.Analytics.Module(ModuleLPR); m.Engine != ModuleEngineProvision || m.Writable {
		t.Fatalf("lpr module: %+v", m)
	}
	if !slices.Contains(cam.Capabilities, CapLPR) || !slices.Contains(cam.Capabilities, CapEvents) {
		t.Fatalf("capabilities: %v", cam.Capabilities)
	}
	names := map[string]string{}
	for _, s := range cam.Sources {
		names[s.Name] = s.Status
	}
	if names["frigate"] != SourceOK || names["provision"] != SourceOK {
		t.Fatalf("sources: %+v", cam.Sources)
	}
	if pr := integration(t, cam, IntegrationProvision); pr.Status != IntegrationConnected {
		t.Fatalf("provision: %+v", pr)
	}
	if fr := integration(t, cam, IntegrationFrigate); fr.Status != IntegrationConnected {
		t.Fatalf("frigate: %+v", fr)
	}
}

func TestComposedProvisionOnlyWhenFrigateDoesNotServeTheCamera(t *testing.T) {
	ad := ComposedAdapter{
		Frigate: FrigateAdapter{Engine: fakeEngine{docErr: frigate.ErrNotFound}},
	}
	cam, err := ad.Read(context.Background(), provisionInv())
	if err != nil {
		t.Fatal(err)
	}
	if cam.Origin.Engine != EngineProvision {
		t.Fatalf("engine = %s", cam.Origin.Engine)
	}
	if len(cam.Sources) < 2 || cam.Sources[0].Name != "frigate" || cam.Sources[0].Status != SourceUnavailable || cam.Sources[1].Name != "provision" {
		t.Fatalf("sources: %+v", cam.Sources)
	}
	if fr := integration(t, cam, IntegrationFrigate); fr.Status != IntegrationNotConfigured {
		t.Fatalf("frigate: %+v", fr)
	}
	if !slices.Contains(cam.Detection.Categories, "vehicle") {
		t.Fatalf("detection: %+v", cam.Detection)
	}
	rev, _ := ad.Plan(context.Background(), cam, Patch{Detection: &DetectionPatch{Enabled: boolp(false)}})
	if len(rev.Changes) != 0 || len(rev.Unsupported) != 1 {
		t.Fatalf("review: %+v", rev)
	}
}

// An unreachable Frigate says nothing about the camera: it stays a Frigate camera, degraded.
func TestComposedUnreachableFrigateStaysFrigate(t *testing.T) {
	for name, eng := range map[string]FrigateEngine{
		"error":   fakeEngine{docErr: errors.New("connection refused")},
		"no link": nil,
	} {
		t.Run(name, func(t *testing.T) {
			cam, err := ComposedAdapter{Frigate: FrigateAdapter{Engine: eng}}.Read(context.Background(), provisionInv())
			if err != nil {
				t.Fatal(err)
			}
			if cam.Origin.Engine != "frigate" {
				t.Fatalf("engine = %s", cam.Origin.Engine)
			}
			if fr := integration(t, cam, IntegrationFrigate); fr.Status != IntegrationError {
				t.Fatalf("frigate: %+v", fr)
			}
		})
	}
}

func boolp(b bool) *bool { return &b }
