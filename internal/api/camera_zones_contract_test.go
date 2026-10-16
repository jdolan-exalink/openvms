package api

import (
	"testing"

	"github.com/jdolan-exalink/openvms/internal/api/gen"
)

func TestCameraZoneUpdateContractDeclaresConflict(t *testing.T) {
	spec, err := gen.GetSpec()
	if err != nil {
		t.Fatal(err)
	}
	zone := spec.Paths.Find("/api/v1/cameras/{cameraId}/zones/{zoneId}")
	if zone == nil || zone.Put == nil || zone.Put.Responses.Status(409) == nil {
		t.Fatal("updateCameraZone must declare its 409 duplicate-name conflict")
	}
	branding := spec.Paths.Find("/api/v1/tenants/{tenantId}/branding")
	if branding == nil || branding.Put == nil {
		t.Fatal("updateTenantBranding operation not found")
	}
	if branding.Put.Responses.Status(409) != nil {
		t.Fatal("updateTenantBranding must not declare a 409 it never returns")
	}
}
