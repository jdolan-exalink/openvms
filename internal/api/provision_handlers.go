package api

import (
	"context"
	"errors"
	"time"

	"github.com/jdolan-exalink/openvms/internal/agent"
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
	snap, err := h.Provision.Start(ctx, a, provision.Request{SiteID: r.Body.SiteId, IP: r.Body.Ip, ServerName: r.Body.ServerName, User: r.Body.SshUser, Password: *r.Body.SshPassword, HostKey: hostKey, TrustOnFirstUse: trustOnFirstUse, AllowSystemDisk: allowSystemDisk})
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
	snap, err := h.Provision.Get(a, r.JobId)
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
	job, err := h.Provision.StartServerAgentInstall(ctx, a, r.ServerId, provision.AgentInstallStartRequest{
		Host: r.Body.SshHost, Port: uint16(r.Body.SshPort), Password: *r.Body.SshPassword, ReplaceExisting: r.Body.ReplaceExisting,
	})
	if err != nil {
		return nil, err
	}
	return gen.InstallServerAgent202JSONResponse(serverAgentInstallJob(job)), nil
}

func (h *Handlers) InspectServerAgentOverSSH(ctx context.Context, r gen.InspectServerAgentOverSSHRequestObject) (gen.InspectServerAgentOverSSHResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if r.Body == nil || r.Body.SshPassword == nil {
		return nil, &provision.ValidationError{Msg: "agent discovery request is required"}
	}
	if r.Body.SshPort < 1 || r.Body.SshPort > 65535 {
		return nil, &provision.ValidationError{Msg: "ssh_port must be between 1 and 65535"}
	}
	status, err := h.Provision.InspectServerAgent(ctx, a, r.ServerId, provision.AgentInstallStartRequest{
		Host: r.Body.SshHost, Port: uint16(r.Body.SshPort), Password: *r.Body.SshPassword,
	})
	if errors.Is(err, provision.ErrAgentDiscoveryConnection) {
		failure := gen.ServerAgentDiscoveryResultFailure("connection_failed")
		result := serverAgentDiscovery(status)
		result.Failure = &failure
		return gen.InspectServerAgentOverSSH200JSONResponse(result), nil
	}
	if err != nil {
		return nil, err
	}
	return gen.InspectServerAgentOverSSH200JSONResponse(serverAgentDiscovery(status)), nil
}

func serverAgentDiscovery(in provision.RemoteAgentStatus) gen.ServerAgentDiscoveryResult {
	state := gen.ServerAgentDiscoveryResultStateUnknown
	ownership := gen.ServerAgentDiscoveryResultOwnershipUnknown
	switch in.State {
	case provision.RemoteAgentAbsent:
		state = gen.ServerAgentDiscoveryResultStateAbsent
	case provision.RemoteAgentPresentUnregistered, provision.RemoteAgentRegisteredHere:
		state = gen.ServerAgentDiscoveryResultStatePresent
		if in.State == provision.RemoteAgentRegisteredHere {
			ownership = gen.ServerAgentDiscoveryResultOwnershipRegisteredHere
		}
	}
	result := gen.ServerAgentDiscoveryResult{State: state, Ownership: ownership}
	if in.Version != "" {
		result.Version = &in.Version
	}
	return result
}

func (h *Handlers) GetServerAgentInstallJob(ctx context.Context, r gen.GetServerAgentInstallJobRequestObject) (gen.GetServerAgentInstallJobResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	job, err := h.Provision.GetServerAgentInstallJob(ctx, a, r.ServerId, r.JobId)
	if err != nil {
		return nil, err
	}
	return gen.GetServerAgentInstallJob200JSONResponse(serverAgentInstallJob(job)), nil
}

func (h *Handlers) UpdateServerAgentOverSSH(ctx context.Context, r gen.UpdateServerAgentOverSSHRequestObject) (gen.UpdateServerAgentOverSSHResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if r.Body == nil || r.Body.SshPassword == nil {
		return nil, &provision.ValidationError{Msg: "agent update request is required"}
	}
	if r.Body.SshPort < 1 || r.Body.SshPort > 65535 {
		return nil, &provision.ValidationError{Msg: "ssh_port must be between 1 and 65535"}
	}
	job, err := h.Provision.StartServerAgentUpdate(ctx, a, r.ServerId, provision.AgentUpdateStartRequest{
		SSHPort: uint16(r.Body.SshPort), Password: *r.Body.SshPassword,
	})
	if err != nil {
		return nil, err
	}
	return gen.UpdateServerAgentOverSSH202JSONResponse(serverAgentInstallJob(job)), nil
}

func (h *Handlers) GetServerAgentUpdateJob(ctx context.Context, r gen.GetServerAgentUpdateJobRequestObject) (gen.GetServerAgentUpdateJobResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	job, err := h.Provision.GetServerAgentInstallJob(ctx, a, r.ServerId, r.JobId)
	if err != nil {
		return nil, err
	}
	return gen.GetServerAgentUpdateJob200JSONResponse(serverAgentInstallJob(job)), nil
}

func (h *Handlers) UninstallServerAgentOverSSH(ctx context.Context, r gen.UninstallServerAgentOverSSHRequestObject) (gen.UninstallServerAgentOverSSHResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if r.Body == nil || r.Body.SshPassword == nil {
		return nil, &provision.ValidationError{Msg: "agent uninstall request is required"}
	}
	if r.Body.SshPort < 1 || r.Body.SshPort > 65535 {
		return nil, &provision.ValidationError{Msg: "ssh_port must be between 1 and 65535"}
	}
	job, err := h.Provision.StartServerAgentUninstall(ctx, a, r.ServerId, agentUninstallStartRequest(r.Body))
	if err != nil {
		return nil, err
	}
	return gen.UninstallServerAgentOverSSH202JSONResponse(serverAgentInstallJob(job)), nil
}

// agentUninstallStartRequest maps the body; an omitted config_action means keep and an omitted
// discard_backup means false. An unknown config_action is rejected later by the provision service.
func agentUninstallStartRequest(body *gen.ServerAgentUninstallRequest) provision.AgentUninstallStartRequest {
	out := provision.AgentUninstallStartRequest{SSHPort: uint16(body.SshPort), Password: *body.SshPassword, ConfigAction: provision.ConfigActionKeep}
	if body.ConfigAction != nil {
		out.ConfigAction = string(*body.ConfigAction)
	}
	if body.DiscardBackup != nil {
		out.DiscardBackup = *body.DiscardBackup
	}
	return out
}

func (h *Handlers) GetServerAgentUninstallJob(ctx context.Context, r gen.GetServerAgentUninstallJobRequestObject) (gen.GetServerAgentUninstallJobResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	job, err := h.Provision.GetServerAgentInstallJob(ctx, a, r.ServerId, r.JobId)
	if err != nil {
		return nil, err
	}
	return gen.GetServerAgentUninstallJob200JSONResponse(serverAgentInstallJob(job)), nil
}

func serverAgentInstallJob(in provision.AgentInstallJob) gen.ServerAgentInstallJob {
	out := gen.ServerAgentInstallJob{Id: in.ID, Status: gen.ServerAgentInstallJobStatus(in.Status), Stage: gen.ServerAgentInstallJobStage(in.Stage)}
	if in.Message != "" {
		message := in.Message
		out.Message = &message
	}
	if in.Code != "" {
		code := gen.ServerAgentInstallJobCode(in.Code)
		out.Code = &code
	}
	if in.InstalledVersion != "" {
		version := in.InstalledVersion
		out.InstalledVersion = &version
	}
	if in.BundledVersion != "" {
		version := in.BundledVersion
		out.BundledVersion = &version
	}
	if in.RegisteredHere {
		registered := true
		out.RegisteredHere = &registered
	}
	if in.VersionState != "" {
		state := gen.ServerAgentInstallJobVersionState(in.VersionState)
		out.VersionState = &state
	}
	return out
}

func (h *Handlers) GetServerAgent(ctx context.Context, r gen.GetServerAgentRequestObject) (gen.GetServerAgentResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	view, err := h.Provision.AgentStatus(ctx, a, r.ServerId)
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
	view, err := h.Provision.UpdateAgent(ctx, a, r.ServerId)
	if err != nil {
		return nil, err
	}
	// #nosec G118 -- asynchronous sweep outlives HTTP request
	go func() {
		sweepCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Minute)
		defer cancel()
		_, _ = h.SweepServerCamerasOnvif(sweepCtx, a, r.ServerId, nil)
	}()
	return gen.UpdateServerAgent200JSONResponse(serverAgent(view)), nil
}

func (h *Handlers) GetServerAgentBackup(ctx context.Context, r gen.GetServerAgentBackupRequestObject) (gen.GetServerAgentBackupResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	backup, err := h.Provision.GetAgentBackup(ctx, a, r.ServerId)
	if err != nil {
		return nil, err
	}
	return gen.GetServerAgentBackup200JSONResponse(serverAgentBackup(backup)), nil
}

func (h *Handlers) EnsureServerAgentBackup(ctx context.Context, r gen.EnsureServerAgentBackupRequestObject) (gen.EnsureServerAgentBackupResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	backup, err := h.Provision.EnsureOriginalConfigBackup(ctx, a, r.ServerId)
	if err != nil {
		return nil, err
	}
	return gen.EnsureServerAgentBackup200JSONResponse(serverAgentBackup(backup)), nil
}

// serverAgentBackup is the metadata of the backup; the configuration itself never leaves the agent.
func serverAgentBackup(in provision.AgentBackup) gen.ServerAgentBackup {
	out := gen.ServerAgentBackup{
		Exists: in.Exists, FrigateVersion: in.FrigateVersion, RegisteredBy: in.RegisteredBy, Sha256: in.SHA256, Size: in.Size,
	}
	if !in.SavedAt.IsZero() {
		saved := in.SavedAt
		out.SavedAt = &saved
	}
	return out
}

func serverProvision(s provision.Snapshot) gen.ServerProvision {
	out := gen.ServerProvision{Id: s.ID, Status: gen.ServerProvisionStatus(s.Status)}
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
		id := *s.ServerID
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
	out := gen.ServerAgent{
		Installed: s.Installed, Version: s.Version, CurrentVersion: s.Current, Outdated: s.Outdated,
		BinaryStatus: gen.ServerAgentBinaryStatus(s.BinaryStatus), BinaryUpgradeAvailable: s.BinaryUpgradeAvailable,
		MemoryTotalBytes: int64ptr(s.MemoryTotal), MemoryAvailableBytes: int64ptr(s.MemoryAvailable),
		CctvTotalBytes: int64ptr(s.CCTVTotal), CctvFreeBytes: int64ptr(s.CCTVFree),
		DatabaseTotalBytes: int64ptr(s.DatabaseTotal), DatabaseFreeBytes: int64ptr(s.DatabaseFree),
		Coral: boolptr(s.Coral), GpuPresent: boolptr(s.GPUPresent),
		CoralUsb: boolptr(s.CoralUSB), CoralPcie: boolptr(s.CoralPCIe),
		Openvino: boolptr(s.OpenVINO), Hailo: boolptr(s.Hailo), Nvidia: boolptr(s.Nvidia),
		StorageBufferPercent: intptr(s.StorageBufferPercent),
		StorageWatchdog:      boolptr(s.StorageWatchdog),
		StorageOver85Percent: boolptr(s.StorageOver85Percent),
		DatabaseCritical:     boolptr(s.DatabaseCritical),
		PrunedSegmentsCount:  int64ptr(s.PrunedSegmentsCount),
	}
	if s.CPUAvailable {
		out.CpuPercent = float32ptr(float32(s.CPUPercent))
	}
	if len(s.NetworkInterfaces) > 0 {
		interfaces := make([]gen.ServerAgentNetworkInterface, 0, len(s.NetworkInterfaces))
		for _, network := range s.NetworkInterfaces {
			iface := gen.ServerAgentNetworkInterface{
				Name:             network.Name,
				RxBytesPerSecond: float32(network.RXBytesPerSecond),
				TxBytesPerSecond: float32(network.TXBytesPerSecond),
			}
			if network.IPAddress != "" {
				ip := network.IPAddress
				iface.IpAddress = &ip
			}
			if network.MAC != "" {
				mac := network.MAC
				iface.MacAddress = &mac
			}
			if network.Subnet != "" {
				subnet := network.Subnet
				iface.Subnet = &subnet
			}
			if network.Netmask != "" {
				netmask := network.Netmask
				iface.Netmask = &netmask
			}
			if network.Gateway != "" {
				gw := network.Gateway
				iface.Gateway = &gw
			}
			isDef := network.IsDefault
			iface.IsDefault = &isDef
			if network.Role != "" {
				role := gen.ServerAgentNetworkInterfaceRole(network.Role)
				iface.Role = &role
			}
			interfaces = append(interfaces, iface)
		}
		out.NetworkInterfaces = &interfaces
	}
	if s.NetworkSampledAt != nil {
		out.NetworkSampledAt = s.NetworkSampledAt
	}
	if s.SampledAt != nil {
		out.SampledAt = s.SampledAt
	}
	if s.BinaryAvailable != nil {
		identity := serverAgentBinaryIdentity(*s.BinaryAvailable)
		out.BinaryAvailable = &identity
	}
	if s.BinaryObserved != nil {
		identity := serverAgentBinaryIdentity(*s.BinaryObserved)
		out.BinaryObserved = &identity
	}
	if s.BinaryOutdated != nil {
		out.BinaryOutdated = s.BinaryOutdated
	}
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
	if s.OS != "" {
		osName := s.OS
		out.Os = &osName
	}
	if s.Error != "" {
		out.Error = &s.Error
	}
	return out
}

func serverAgentBinaryIdentity(in agent.BinaryIdentity) gen.ServerAgentBinaryIdentity {
	out := gen.ServerAgentBinaryIdentity{Sha256: in.SHA256, Architecture: in.Architecture}
	if in.Version != "" {
		out.Version = &in.Version
	}
	if in.Commit != "" {
		out.Commit = &in.Commit
	}
	return out
}
func intptr(v int) *int             { return &v }
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
	result, err := h.Provision.Discover(ctx, a, r.ServerId, r.Body.InterfaceName)
	if err != nil {
		return nil, err
	}
	out := gen.OnvifDiscoveryResult{Devices: make([]gen.OnvifDiscoveryDevice, 0, len(result.Devices))}
	for _, device := range result.Devices {
		dev := gen.OnvifDiscoveryDevice{Xaddrs: device.XAddrs}
		if len(device.Scopes) > 0 {
			dev.Scopes = &device.Scopes
		}
		out.Devices = append(out.Devices, dev)
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
	result, err := h.Provision.ProbeOnvif(ctx, a, r.ServerId, input)
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
	if len(in.Profiles) > 0 {
		profiles := make([]gen.OnvifProbeProfile, 0, len(in.Profiles))
		for _, profile := range in.Profiles {
			profiles = append(profiles, gen.OnvifProbeProfile{
				Token: profile.Token, Name: profile.Name, Fixed: profile.Fixed,
				MediaVersion:              gen.OnvifProbeProfileMediaVersion(profile.MediaVersion),
				SourceConfigurationToken:  profile.SourceConfigurationToken,
				EncoderConfigurationToken: profile.EncoderConfigurationToken,
				PtzConfigurationToken:     profile.PTZConfigurationToken,
				Encoding:                  profile.Encoding, Width: profile.Width, Height: profile.Height,
				Fps: profile.FPS, FrameRate: float32(profile.FrameRate), Bitrate: profile.Bitrate,
				StreamUri: profile.StreamURI,
			})
		}
		out.Profiles = &profiles
	}
	for _, service := range in.Services {
		out.Services = append(out.Services, gen.OnvifProbeService{Namespace: service.Namespace, Xaddrs: service.XAddrs, Version: gen.OnvifProbeVersion{Major: service.Version.Major, Minor: service.Version.Minor}})
	}
	return out
}
