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
