package api

import (
	"testing"

	"github.com/google/uuid"
	"github.com/oapi-codegen/runtime/types"

	"github.com/jdolan-exalink/openvms/internal/provision"
)

func TestServerProvisionProjectsProgressWithoutCredentialFields(t *testing.T) {
	jobID, serverID := uuid.New(), uuid.New()
	snapshot := provision.Snapshot{
		ID: jobID, Status: "running", Warning: "cpu", Variant: "cpu", ServerID: &serverID,
		HostKey: "SHA256:verified", Steps: []provision.StepState{{ID: "connecting", State: "done"}},
	}
	got := serverProvision(snapshot)
	if got.Id != types.UUID(jobID) || got.Status != genServerProvisionRunning || got.Warning == nil || *got.Warning != "cpu" {
		t.Fatalf("provision header fields not projected: %#v", got)
	}
	if got.ServerId == nil || *got.ServerId != types.UUID(serverID) || got.HostKey == nil || *got.HostKey != snapshot.HostKey {
		t.Fatalf("provision identity fields not projected: %#v", got)
	}
	if len(got.Steps) != 1 || string(got.Steps[0].Id) != "connecting" || string(got.Steps[0].State) != "done" {
		t.Fatalf("provision progress not projected: %#v", got.Steps)
	}
}

func TestServerAgentProjectsMetricsAndVersion(t *testing.T) {
	view := provision.AgentView{
		Installed: true, Version: "1.2.3", Current: "1.2.4", Outdated: true, CPUPercent: 72.5,
		MemoryTotal: 100, MemoryAvailable: 25, Coral: true, GPUPresent: true, GPUVendor: "NVIDIA", GPUName: "Test GPU",
		CCTVTotal: 200, CCTVFree: 80, DatabaseTotal: 300, DatabaseFree: 120, Variant: "tensorrt", NTP: "synced",
	}
	got := serverAgent(view)
	if !got.Installed || !got.Outdated || got.Version != view.Version || got.CurrentVersion != view.Current {
		t.Fatalf("agent version state not projected: %#v", got)
	}
	if got.CpuPercent == nil || float64(*got.CpuPercent) != 72.5 || got.MemoryAvailableBytes == nil || *got.MemoryAvailableBytes != 25 {
		t.Fatalf("agent metrics not projected: %#v", got)
	}
	if got.Coral == nil || !*got.Coral || got.GpuName == nil || *got.GpuName != view.GPUName || got.Variant == nil || *got.Variant != view.Variant {
		t.Fatalf("agent hardware state not projected: %#v", got)
	}
}

const genServerProvisionRunning = "running"
