package api

import (
	"context"

	"github.com/google/uuid"
	"github.com/oapi-codegen/runtime/types"

	"github.com/jdolan-exalink/openvms/internal/api/gen"
	"github.com/jdolan-exalink/openvms/internal/provision"
)

func (h *Handlers) StartServerProvision(ctx context.Context, r gen.StartServerProvisionRequestObject) (gen.StartServerProvisionResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if r.Body == nil || r.Body.SshPassword == nil {
		return nil, &provision.ValidationError{Msg: "ssh password is required"}
	}
	allowSystemDisk := r.Body.AllowSystemDisk != nil && *r.Body.AllowSystemDisk
	trustOnFirstUse := r.Body.TrustOnFirstUse != nil && *r.Body.TrustOnFirstUse
	hostKey := ""
	if r.Body.HostKey != nil {
		hostKey = *r.Body.HostKey
	}
	snap, err := h.Provision.Start(ctx, a, provision.Request{SiteID: uuid.UUID(r.Body.SiteId), IP: r.Body.Ip, ServerName: r.Body.ServerName, User: r.Body.SshUser, Password: *r.Body.SshPassword, HostKey: hostKey, TrustOnFirstUse: trustOnFirstUse, AllowSystemDisk: allowSystemDisk})
	if err != nil {
		return nil, err
	}
	return gen.StartServerProvision202JSONResponse(serverProvision(snap)), nil
}

func (h *Handlers) GetServerProvision(ctx context.Context, r gen.GetServerProvisionRequestObject) (gen.GetServerProvisionResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	snap, err := h.Provision.Get(a, uuid.UUID(r.JobId))
	if err != nil {
		return nil, err
	}
	return gen.GetServerProvision200JSONResponse(serverProvision(snap)), nil
}

func (h *Handlers) InstallServerAgent(ctx context.Context, r gen.InstallServerAgentRequestObject) (gen.InstallServerAgentResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if r.Body == nil || r.Body.SshPassword == nil {
		return nil, &provision.ValidationError{Msg: "agent install request is required"}
	}
	if r.Body.SshPort < 1 || r.Body.SshPort > 65535 {
		return nil, &provision.ValidationError{Msg: "ssh_port must be between 1 and 65535"}
	}
	job, err := h.Provision.StartServerAgentInstall(ctx, a, uuid.UUID(r.ServerId), provision.AgentInstallStartRequest{
		Host: r.Body.SshHost, Port: uint16(r.Body.SshPort), Password: *r.Body.SshPassword,
		HostKey: r.Body.SshHostKeyFingerprint,
	})
	if err != nil {
		return nil, err
	}
	return gen.InstallServerAgent202JSONResponse(serverAgentInstallJob(job)), nil
}

func (h *Handlers) GetServerAgentInstallJob(ctx context.Context, r gen.GetServerAgentInstallJobRequestObject) (gen.GetServerAgentInstallJobResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	job, err := h.Provision.GetServerAgentInstallJob(ctx, a, uuid.UUID(r.ServerId), uuid.UUID(r.JobId))
	if err != nil {
		return nil, err
	}
	return gen.GetServerAgentInstallJob200JSONResponse(serverAgentInstallJob(job)), nil
}

func serverAgentInstallJob(in provision.AgentInstallJob) gen.ServerAgentInstallJob {
	out := gen.ServerAgentInstallJob{Id: types.UUID(in.ID), Status: gen.ServerAgentInstallJobStatus(in.Status), Stage: gen.ServerAgentInstallJobStage(in.Stage)}
	if in.Message != "" {
		message := in.Message
		out.Message = &message
	}
	return out
}

func (h *Handlers) GetServerAgent(ctx context.Context, r gen.GetServerAgentRequestObject) (gen.GetServerAgentResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	view, err := h.Provision.AgentStatus(ctx, a, uuid.UUID(r.ServerId))
	if err != nil {
		return nil, err
	}
	return gen.GetServerAgent200JSONResponse(serverAgent(view)), nil
}

func (h *Handlers) UpdateServerAgent(ctx context.Context, r gen.UpdateServerAgentRequestObject) (gen.UpdateServerAgentResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	view, err := h.Provision.UpdateAgent(ctx, a, uuid.UUID(r.ServerId))
	if err != nil {
		return nil, err
	}
	return gen.UpdateServerAgent200JSONResponse(serverAgent(view)), nil
}

func serverProvision(s provision.Snapshot) gen.ServerProvision {
	out := gen.ServerProvision{Id: types.UUID(s.ID), Status: gen.ServerProvisionStatus(s.Status)}
	if s.Error != "" {
		out.Error = &s.Error
	}
	if s.Warning != "" {
		warning := gen.ServerProvisionWarning(s.Warning)
		out.Warning = &warning
	}
	if s.Variant != "" {
		v := gen.ServerProvisionVariant(s.Variant)
		out.Variant = &v
	}
	if s.ServerID != nil {
		id := types.UUID(*s.ServerID)
		out.ServerId = &id
	}
	if s.HostKey != "" {
		out.HostKey = &s.HostKey
	}
	for _, step := range s.Steps {
		item := gen.ServerProvisionStep{Id: gen.ServerProvisionStepId(step.ID), State: gen.ServerProvisionStepState(step.State)}
		if step.Detail != "" {
			item.Detail = &step.Detail
		}
		out.Steps = append(out.Steps, item)
	}
	return out
}

func serverAgent(s provision.AgentView) gen.ServerAgent {
	out := gen.ServerAgent{Installed: s.Installed, Version: s.Version, CurrentVersion: s.Current, Outdated: s.Outdated, CpuPercent: float32ptr(float32(s.CPUPercent)), MemoryTotalBytes: int64ptr(s.MemoryTotal), MemoryAvailableBytes: int64ptr(s.MemoryAvailable), CctvTotalBytes: int64ptr(s.CCTVTotal), CctvFreeBytes: int64ptr(s.CCTVFree), DatabaseTotalBytes: int64ptr(s.DatabaseTotal), DatabaseFreeBytes: int64ptr(s.DatabaseFree), Coral: boolptr(s.Coral), GpuPresent: boolptr(s.GPUPresent)}
	if s.Variant != "" {
		out.Variant = &s.Variant
	}
	if s.NTP != "" {
		out.Ntp = &s.NTP
	}
	if s.GPUVendor != "" {
		out.GpuVendor = &s.GPUVendor
	}
	if s.GPUName != "" {
		out.GpuName = &s.GPUName
	}
	if s.Error != "" {
		out.Error = &s.Error
	}
	return out
}
func int64ptr(v int64) *int64       { return &v }
func float32ptr(v float32) *float32 { return &v }
func boolptr(v bool) *bool          { return &v }

func (h *Handlers) DiscoverServerOnvif(ctx context.Context, r gen.DiscoverServerOnvifRequestObject) (gen.DiscoverServerOnvifResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if r.Body == nil || len(r.Body.InterfaceName) == 0 || len(r.Body.InterfaceName) > 64 {
		return nil, &provision.ValidationError{Msg: "interface_name is required and must be at most 64 characters"}
	}
	result, err := h.Provision.Discover(ctx, a, uuid.UUID(r.ServerId), r.Body.InterfaceName)
	if err != nil {
		return nil, err
	}
	out := gen.OnvifDiscoveryResult{Devices: make([]gen.OnvifDiscoveryDevice, 0, len(result.Devices))}
	for _, device := range result.Devices {
		out.Devices = append(out.Devices, gen.OnvifDiscoveryDevice{Xaddrs: device.XAddrs})
	}
	return gen.DiscoverServerOnvif200JSONResponse(out), nil
}

func (h *Handlers) ProbeServerOnvif(ctx context.Context, r gen.ProbeServerOnvifRequestObject) (gen.ProbeServerOnvifResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if r.Body == nil {
		return nil, &provision.ValidationError{Msg: "ONVIF probe request is required"}
	}
	input := provision.OnvifProbeRequest{Endpoint: r.Body.Endpoint}
	if r.Body.Username != nil {
		input.Username = *r.Body.Username
	}
	if r.Body.Password != nil {
		input.Password = *r.Body.Password
	}
	result, err := h.Provision.ProbeOnvif(ctx, a, uuid.UUID(r.ServerId), input)
	if err != nil {
		return nil, err
	}
	return gen.ProbeServerOnvif200JSONResponse(onvifProbeResult(result)), nil
}

func onvifProbeResult(in provision.OnvifProbeResult) gen.OnvifProbeResult {
	out := gen.OnvifProbeResult{
		DeviceInformation: gen.OnvifProbeDeviceInformation{
			Manufacturer: in.Information.Manufacturer, Model: in.Information.Model,
			FirmwareVersion: in.Information.FirmwareVersion, SerialNumber: in.Information.SerialNumber, HardwareId: in.Information.HardwareID,
		},
		Services: make([]gen.OnvifProbeService, 0, len(in.Services)),
		SystemTime: gen.OnvifProbeClock{DateTimeType: in.Clock.DateTimeType,
			Utc:   gen.OnvifProbeDateTime{Time: gen.OnvifProbeTime{Hour: in.Clock.UTC.Time.Hour, Minute: in.Clock.UTC.Time.Minute, Second: in.Clock.UTC.Time.Second}, Date: gen.OnvifProbeDate{Year: in.Clock.UTC.Date.Year, Month: in.Clock.UTC.Date.Month, Day: in.Clock.UTC.Date.Day}},
			Local: gen.OnvifProbeDateTime{Time: gen.OnvifProbeTime{Hour: in.Clock.Local.Time.Hour, Minute: in.Clock.Local.Time.Minute, Second: in.Clock.Local.Time.Second}, Date: gen.OnvifProbeDate{Year: in.Clock.Local.Date.Year, Month: in.Clock.Local.Date.Month, Day: in.Clock.Local.Date.Day}}},
	}
	for _, service := range in.Services {
		out.Services = append(out.Services, gen.OnvifProbeService{Namespace: service.Namespace, Xaddrs: service.XAddrs, Version: gen.OnvifProbeVersion{Major: service.Version.Major, Minor: service.Version.Minor}})
	}
	return out
}
