package camconfig

import "testing"

func TestBuildIntegrationsStatuses(t *testing.T) {
	withCaps := baseInv()
	noONVIF := baseInv()
	noONVIF.ONVIFEndpoint, noONVIF.ONVIFCapabilities = "", nil
	unprobed := baseInv()
	unprobed.ONVIFCapabilities = nil

	cases := map[string]struct {
		cam                  Camera
		in                   Inventory
		frigate, onvif, prov string
	}{
		"all fine": {
			Camera{Sources: []Source{{Name: "frigate", Status: SourceOK}, {Name: "onvif", Status: SourceOK}}}, withCaps,
			IntegrationConnected, IntegrationConnected, IntegrationNotConfigured,
		},
		"frigate down, onvif unreadable": {
			Camera{Sources: []Source{{Name: "frigate", Status: SourceUnavailable}, {Name: "onvif", Status: SourceUnavailable, Detail: "unreachable"}}}, withCaps,
			IntegrationError, IntegrationError, IntegrationNotConfigured,
		},
		"no onvif endpoint":  {Camera{Sources: []Source{{Name: "frigate", Status: SourceOK}}}, noONVIF, IntegrationConnected, IntegrationNotConfigured, IntegrationNotConfigured},
		"onvif never probed": {Camera{Sources: []Source{{Name: "frigate", Status: SourceOK}}}, unprobed, IntegrationConnected, IntegrationUnknown, IntegrationNotConfigured},
		"onvif stored caps":  {Camera{Sources: []Source{{Name: "frigate", Status: SourceOK}}}, withCaps, IntegrationConnected, IntegrationConnected, IntegrationNotConfigured},
		"frigate not serving": {
			Camera{Sources: []Source{{Name: "frigate", Status: SourceUnavailable, Detail: DetailNotServed}}}, noONVIF,
			IntegrationNotConfigured, IntegrationNotConfigured, IntegrationNotConfigured,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := BuildIntegrations(tc.cam, tc.in, nil)
			if len(got) < 7 || got[0].ID != IntegrationFrigate || got[1].ID != IntegrationONVIF || got[2].ID != IntegrationProvision {
				t.Fatalf("entries: %+v", got)
			}
			for i, want := range []string{tc.frigate, tc.onvif, tc.prov} {
				if got[i].Status != want || got[i].Name != got[i].ID || got[i].Label == "" {
					t.Errorf("%s = %+v, want %s", got[i].ID, got[i], want)
				}
			}
			// Verify granular integration controls are present
			hasLPRProv, hasAnalyticsProv, hasAnalyticsFrigate, hasLPRFrigate := false, false, false, false
			for _, item := range got {
				switch item.ID {
				case IntegrationLPRProvision:
					hasLPRProv = true
					if item.Label != "LPR Provision-ISR" {
						t.Errorf("LPR Provision label = %q, want 'LPR Provision-ISR'", item.Label)
					}
				case IntegrationAnalyticsProvision:
					hasAnalyticsProv = true
					if item.Label != "Analíticas Provision" {
						t.Errorf("Analytics Provision label = %q, want 'Analíticas Provision'", item.Label)
					}
				case IntegrationAnalyticsFrigate:
					hasAnalyticsFrigate = true
					if item.Label != "Analíticas Frigate" {
						t.Errorf("Analytics Frigate label = %q, want 'Analíticas Frigate'", item.Label)
					}
				case IntegrationLPRFrigate:
					hasLPRFrigate = true
					if item.Label != "LPR Frigate" {
						t.Errorf("LPR Frigate label = %q, want 'LPR Frigate'", item.Label)
					}
				}
			}
			if !hasLPRProv || !hasAnalyticsProv || !hasAnalyticsFrigate || !hasLPRFrigate {
				t.Fatalf("missing granular controls: %+v", got)
			}
		})
	}
}
