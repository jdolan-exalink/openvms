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
		ch := gen.ProvisionNvrChannel{Id: c.ID, Name: c.Name, Address: optString(c.Address), Model: optString(c.Model), Protocol: optString(c.Protocol)}
		if c.Port > 0 {
			port := c.Port
			ch.Port = &port
		}
		if c.Status != nil {
			online := c.Status.Online
			ch.Online = &online
			ch.Recording, ch.Motion = optString(c.Status.Recording), optString(c.Status.Motion)
		}
		out.Channels = append(out.Channels, ch)
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
