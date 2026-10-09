package api

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/runtime/types"

	"github.com/jdolan-exalink/openvms/internal/agent"
	"github.com/jdolan-exalink/openvms/internal/api/gen"
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
	outdated := true
	view := provision.AgentView{
		Installed: true, Version: "1.2.3", Current: "1.2.4", Outdated: true, CPUPercent: 72.5, CPUAvailable: true,
		BinaryStatus: provision.AgentBinaryUpdateAvailable, BinaryAvailable: &agent.BinaryIdentity{SHA256: strings.Repeat("a", 64), Architecture: "amd64", Commit: "desired"},
		BinaryObserved: &agent.BinaryIdentity{SHA256: strings.Repeat("b", 64), Architecture: "amd64", Version: "v1.2.3"}, BinaryOutdated: &outdated, BinaryUpgradeAvailable: true,
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
	if got.BinaryStatus != gen.ServerAgentBinaryStatusUpdateAvailable || got.BinaryOutdated == nil || !*got.BinaryOutdated || !got.BinaryUpgradeAvailable {
		t.Fatalf("binary status not projected independently: %#v", got)
	}
	if got.BinaryAvailable == nil || got.BinaryAvailable.Sha256 != strings.Repeat("a", 64) || got.BinaryAvailable.Commit == nil || *got.BinaryAvailable.Commit != "desired" {
		t.Fatalf("available binary identity not projected: %#v", got.BinaryAvailable)
	}
	if got.BinaryObserved == nil || got.BinaryObserved.Sha256 != strings.Repeat("b", 64) || got.BinaryObserved.Version == nil || *got.BinaryObserved.Version != "v1.2.3" {
		t.Fatalf("observed binary identity not projected: %#v", got.BinaryObserved)
	}
}

func TestServerAgentProjectsOptionalFreshNetworkAndUnavailableCPU(t *testing.T) {
	sampledAt := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	got := serverAgent(provision.AgentView{
		Installed: true, Version: "0.1.0", Current: "0.1.0", BinaryStatus: provision.AgentBinaryUnknown,
		NetworkInterfaces: []agent.NetworkInterface{{Name: "eth0", RXBytesPerSecond: 125.5, TXBytesPerSecond: 64}}, NetworkSampledAt: &sampledAt,
	})
	if got.CpuPercent != nil {
		t.Fatalf("unavailable CPU was projected as a number: %#v", got.CpuPercent)
	}
	if got.NetworkInterfaces == nil || len(*got.NetworkInterfaces) != 1 || (*got.NetworkInterfaces)[0].Name != "eth0" || (*got.NetworkInterfaces)[0].RxBytesPerSecond != 125.5 || got.NetworkSampledAt == nil || !got.NetworkSampledAt.Equal(sampledAt) {
		t.Fatalf("network sample was not projected: %#v", got)
	}
	legacy := serverAgent(provision.AgentView{Installed: true})
	if legacy.NetworkInterfaces != nil || legacy.NetworkSampledAt != nil {
		t.Fatalf("missing legacy measurements must remain absent: %#v", legacy)
	}
}

func TestServerAgentKeepsProtocolAndBinaryOutdatedSignalsSeparate(t *testing.T) {
	view := provision.AgentView{
		Installed: true, Version: "0.1.0", Current: "0.1.0", Outdated: false,
		BinaryStatus: provision.AgentBinaryUpdateAvailable, BinaryOutdated: boolptr(true), BinaryUpgradeAvailable: true,
	}
	got := serverAgent(view)
	if got.Version != "0.1.0" || got.CurrentVersion != "0.1.0" || got.Outdated {
		t.Fatalf("legacy protocol fields changed: %#v", got)
	}
	if got.BinaryStatus != gen.ServerAgentBinaryStatusUpdateAvailable || got.BinaryOutdated == nil || !*got.BinaryOutdated {
		t.Fatalf("binary mismatch was conflated with protocol status: %#v", got)
	}
}

const genServerProvisionRunning = "running"

func TestOnvifDiscoveryProjectionHasNoCredentialFields(t *testing.T) {
	got := gen.OnvifDiscoveryResult{Devices: []gen.OnvifDiscoveryDevice{{Xaddrs: []string{"http://192.0.2.4/onvif/device_service"}}}}
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "token") || strings.Contains(string(encoded), "password") || !strings.Contains(string(encoded), `"xaddrs"`) {
		t.Fatalf("unexpected discovery projection: %s", encoded)
	}
}
