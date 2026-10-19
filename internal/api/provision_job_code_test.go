package api

import (
	"testing"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/provision"
)

func TestServerAgentInstallJobExposesFailureCode(t *testing.T) {
	out := serverAgentInstallJob(provision.AgentInstallJob{ID: uuid.New(), Status: "failed", Stage: "failed", Code: provision.AgentInstallCodeNotRoot})
	if out.Code == nil || string(*out.Code) != "not_root" {
		t.Fatalf("code = %v", out.Code)
	}
	if plain := serverAgentInstallJob(provision.AgentInstallJob{ID: uuid.New(), Status: "queued", Stage: "validating"}); plain.Code != nil {
		t.Fatalf("code = %v on a job without one", plain.Code)
	}
}

func TestServerAgentInstallJobExposesVersionDetails(t *testing.T) {
	out := serverAgentInstallJob(provision.AgentInstallJob{ID: uuid.New(), Status: "failed", Stage: "failed", Code: provision.AgentInstallCodeAgentInstalled, InstalledVersion: "0.2.1", BundledVersion: "0.3.0", VersionState: provision.AgentVersionOutdated, RegisteredHere: true})
	if out.InstalledVersion == nil || *out.InstalledVersion != "0.2.1" || out.BundledVersion == nil || *out.BundledVersion != "0.3.0" || out.VersionState == nil || string(*out.VersionState) != "outdated" || out.RegisteredHere == nil || !*out.RegisteredHere {
		t.Fatalf("job = %#v", out)
	}
}
