package api

import (
	"context"

	"github.com/jdolan-exalink/openvms/internal/api/gen"
	"github.com/jdolan-exalink/openvms/internal/inventory"
	"github.com/jdolan-exalink/openvms/internal/provisionnvr"
)

func (h *Handlers) DetectProvisionNvr(ctx context.Context, r gen.DetectProvisionNvrRequestObject) (gen.DetectProvisionNvrResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	b := r.Body
	conn := inventory.NVRConnInput{Host: b.Host, Username: b.Username, Password: b.Password}
	if b.Port != nil {
		conn.Port = *b.Port
	}
	if b.Https != nil {
		conn.HTTPS = *b.Https
	}
	det, err := h.Inv.DetectProvisionNvr(ctx, a, b.SiteId, conn)
	if err != nil {
		return nil, err
	}
	return gen.DetectProvisionNvr200JSONResponse(toNvrDetection(det)), nil
}

func toNvrDetection(d *provisionnvr.Detection) gen.ProvisionNvrDetection {
	out := gen.ProvisionNvrDetection{
		Device: gen.ProvisionNvrDevice{
			Name: d.Device.Name, Model: d.Device.Model, HardwareModel: d.Device.HardwareModel,
			Firmware: d.Device.SoftwareVersion, LaunchDate: optString(d.Device.LaunchDate),
		},
		ChannelCapacity: d.SystemCaps.IPChannelMax,
		Channels:        make([]gen.ProvisionNvrChannel, 0, len(d.Channels)),
		Disks:           make([]gen.ProvisionNvrDisk, 0, len(d.Storage.Disks)),
		CycleRecord:     d.Storage.CycleRecord,
		Capabilities:    make([]gen.ProvisionNvrCapability, 0, len(d.Capabilities)),
	}
	for _, c := range d.Channels {
		out.Channels = append(out.Channels, toNvrChannel(c.Channel, c.Status))
	}
	for _, dk := range d.Storage.Disks {
		slot := dk.Slot
		out.Disks = append(out.Disks, gen.ProvisionNvrDisk{
			Id: dk.ID, Slot: &slot, Model: dk.Model, Status: optString(dk.Status), SizeMb: dk.SizeMB, FreeMb: dk.FreeMB,
			RecordStart: optString(dk.RecordStart), RecordEnd: optString(dk.RecordEnd),
		})
	}
	for _, c := range d.Capabilities {
		out.Capabilities = append(out.Capabilities, gen.ProvisionNvrCapability{Name: c.Name, State: gen.ProvisionNvrCapabilityState(c.State), Source: c.Source, Detail: c.Detail})
	}
	if d.ONVIF != nil {
		out.Onvif = &gen.ProvisionNvrOnvif{
			Services: append([]string{}, d.ONVIF.Services...), Profiles: d.ONVIF.Profiles, PtzProfiles: d.ONVIF.PTZProfiles,
			StreamUrisOk: d.ONVIF.StreamURIsOK, SampleStreamPath: optString(d.ONVIF.SampleStreamPath),
		}
	}
	return out
}

func (h *Handlers) CreateProvisionNvrServer(ctx context.Context, r gen.CreateProvisionNvrServerRequestObject) (gen.CreateProvisionNvrServerResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	b := r.Body
	conn := inventory.NVRConnInput{Host: b.Host, Username: b.Username, Password: b.Password}
	if b.Port != nil {
		conn.Port = *b.Port
	}
	if b.Https != nil {
		conn.HTTPS = *b.Https
	}
	var ids []string
	if b.ChannelIds != nil {
		ids = *b.ChannelIds
	}
	srv, err := h.Inv.CreateProvisionNVR(ctx, a, inventory.CreateNVRInput{SiteID: b.SiteId, Name: b.Name, Conn: conn, ChannelIDs: ids})
	if err != nil {
		return nil, err
	}
	return gen.CreateProvisionNvrServer201JSONResponse(toServer(srv)), nil
}

func toNvrInfo(i *inventory.NVRInfo) *gen.ProvisionNvrInfo {
	if i == nil {
		return nil
	}
	out := &gen.ProvisionNvrInfo{
		Model: i.Model, HardwareModel: i.HardwareModel, Firmware: i.Firmware, ChannelCapacity: i.ChannelCapacity,
		Capabilities: make([]gen.ProvisionNvrCapability, 0, len(i.Capabilities)),
	}
	for _, c := range i.Capabilities {
		out.Capabilities = append(out.Capabilities, gen.ProvisionNvrCapability{Name: c.Name, State: gen.ProvisionNvrCapabilityState(c.State), Source: c.Source, Detail: c.Detail})
	}
	return out
}

func toNvrChannel(c provisionnvr.Channel, st *provisionnvr.ChannelStatus) gen.ProvisionNvrChannel {
	ch := gen.ProvisionNvrChannel{Id: c.ID, Name: c.Name, Address: optString(c.Address), Model: optString(c.Model), Protocol: optString(c.Protocol)}
	if c.Port > 0 {
		port := c.Port
		ch.Port = &port
	}
	if st != nil {
		online := st.Online
		ch.Online = &online
		ch.Recording, ch.Motion = optString(st.Recording), optString(st.Motion)
	}
	return ch
}

func (h *Handlers) ListProvisionNvrChannels(ctx context.Context, r gen.ListProvisionNvrChannelsRequestObject) (gen.ListProvisionNvrChannelsResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	views, err := h.Inv.ListProvisionNVRChannels(ctx, a, r.ServerId)
	if err != nil {
		return nil, err
	}
	out := gen.ListProvisionNvrChannels200JSONResponse{Items: make([]gen.ProvisionNvrChannel, 0, len(views))}
	for _, v := range views {
		ch := toNvrChannel(v.Channel, v.Status)
		imported := v.Imported
		ch.Imported = &imported
		out.Items = append(out.Items, ch)
	}
	return out, nil
}

func (h *Handlers) ImportProvisionNvrChannels(ctx context.Context, r gen.ImportProvisionNvrChannelsRequestObject) (gen.ImportProvisionNvrChannelsResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	added, skipped, err := h.Inv.ImportProvisionNVRChannels(ctx, a, r.ServerId, r.Body.ChannelIds)
	if err != nil {
		return nil, err
	}
	return gen.ImportProvisionNvrChannels200JSONResponse{Added: added, Skipped: skipped}, nil
}
