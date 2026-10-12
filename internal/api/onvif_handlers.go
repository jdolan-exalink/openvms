package api

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"

	"github.com/jdolan-exalink/openvms/internal/api/gen"
	"github.com/jdolan-exalink/openvms/internal/provision"
)

func parseOnvifProbeCapabilities(res provision.OnvifProbeResult) (string, string, gen.CameraOnvifCapabilities) {
	manufacturer := res.Information.Manufacturer
	model := res.Information.Model
	firmware := res.Information.FirmwareVersion
	serial := res.Information.SerialNumber
	hwID := res.Information.HardwareID

	hasMedia2 := false
	hasMedia1 := false
	hasAnalytics := false
	hasEvents := false
	hasPTZ := false
	hasImaging := false
	profiles := make([]string, 0, 4)
	services := make([]gen.OnvifProbeService, 0, len(res.Services))

	for _, s := range res.Services {
		services = append(services, gen.OnvifProbeService{
			Namespace: s.Namespace,
			Xaddrs:    s.XAddrs,
			Version:   gen.OnvifProbeVersion{Major: s.Version.Major, Minor: s.Version.Minor},
		})
		ns := strings.ToLower(s.Namespace)
		if strings.Contains(ns, "ver20/media") {
			hasMedia2 = true
			if !containsString(profiles, "Profile T") {
				profiles = append(profiles, "Profile T")
			}
		}
		if strings.Contains(ns, "ver10/media") {
			hasMedia1 = true
			if !containsString(profiles, "Profile S") {
				profiles = append(profiles, "Profile S")
			}
		}
		if strings.Contains(ns, "analytics") {
			hasAnalytics = true
			if !containsString(profiles, "Profile M") {
				profiles = append(profiles, "Profile M")
			}
		}
		if strings.Contains(ns, "events") {
			hasEvents = true
		}
		if strings.Contains(ns, "ptz") {
			hasPTZ = true
		}
		if strings.Contains(ns, "imaging") {
			hasImaging = true
		}
	}

	profile := "ONVIF"
	version := "1.0"
	if hasMedia2 {
		profile = "ONVIF-T"
		version = "2.0"
	} else if hasMedia1 {
		profile = "ONVIF-S"
		version = "1.0"
	}

	caps := gen.CameraOnvifCapabilities{
		Manufacturer:    &manufacturer,
		Model:           &model,
		FirmwareVersion: &firmware,
		SerialNumber:    &serial,
		HardwareId:      &hwID,
		Profiles:        &profiles,
		HasAnalytics:    &hasAnalytics,
		HasEvents:       &hasEvents,
		HasPtz:          &hasPTZ,
		HasMedia2:       &hasMedia2,
		HasMedia1:       &hasMedia1,
		HasImaging:      &hasImaging,
		Services:        &services,
	}

	return profile, version, caps
}

func containsString(list []string, item string) bool {
	for _, v := range list {
		if v == item {
			return true
		}
	}
	return false
}

func (h *Handlers) CheckCameraOnvif(ctx context.Context, r gen.CheckCameraOnvifRequestObject) (gen.CheckCameraOnvifResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	cam, err := h.Inv.GetCamera(ctx, a, r.CameraId)
	if err != nil {
		return nil, err
	}

	var endpoint string
	var username string
	var password string

	if r.Body != nil {
		if r.Body.Endpoint != nil {
			endpoint = strings.TrimSpace(*r.Body.Endpoint)
		}
		if r.Body.Username != nil {
			username = *r.Body.Username
		}
		if r.Body.Password != nil {
			password = *r.Body.Password
		}
	}

	if endpoint == "" && cam.OnvifEndpoint != nil {
		endpoint = strings.TrimSpace(*cam.OnvifEndpoint)
	}

	if endpoint == "" {
		hosts, _ := h.Inv.GetServerCameraStreamHosts(ctx, a, cam.ServerID)
		if info, ok := hosts[cam.RemoteName]; ok && info.Host != "" {
			endpoint = "http://" + info.Host + "/onvif/device_service"
			if username == "" && info.Username != "" {
				username = info.Username
				password = info.Password
			}
		}
	}

	if endpoint == "" {
		return gen.CheckCameraOnvif400JSONResponse{
			BadRequestJSONResponse: gen.BadRequestJSONResponse{Code: "endpoint_required", Message: "Endpoint ONVIF no proporcionado y no detectado automáticamente."},
		}, nil
	}

	probeReq := provision.OnvifProbeRequest{
		Endpoint: endpoint,
		Username: username,
		Password: password,
	}

	probeRes, err := h.Provision.ProbeOnvif(ctx, a, cam.ServerID, probeReq)
	if err != nil {
		return gen.CheckCameraOnvif502JSONResponse{
			FrigateUnreachableJSONResponse: gen.FrigateUnreachableJSONResponse{Code: "onvif_probe_failed", Message: err.Error()},
		}, nil
	}

	profile, version, caps := parseOnvifProbeCapabilities(probeRes)
	capsBytes, _ := json.Marshal(caps)

	updatedCam, err := h.Inv.UpdateCameraOnvif(ctx, a, cam.ID, &endpoint, &profile, &version, capsBytes)
	if err != nil {
		return nil, err
	}

	return gen.CheckCameraOnvif200JSONResponse(toCamera(updatedCam)), nil
}

func (h *Handlers) CheckServerCamerasOnvif(ctx context.Context, r gen.CheckServerCamerasOnvifRequestObject) (gen.CheckServerCamerasOnvifResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	_, err = h.Inv.RequireServerManage(ctx, a, r.ServerId)
	if err != nil {
		return nil, err
	}

	cams, err := h.Inv.ListServerCameras(ctx, a, r.ServerId)
	if err != nil {
		return nil, err
	}

	iface := "eth0"
	if r.Body != nil && r.Body.InterfaceName != nil && strings.TrimSpace(*r.Body.InterfaceName) != "" {
		iface = strings.TrimSpace(*r.Body.InterfaceName)
	}

	discResult, _ := h.Provision.Discover(ctx, a, r.ServerId, iface)
	discoveredByIP := make(map[string]provision.DiscoveryDevice)
	for _, d := range discResult.Devices {
		for _, xaddr := range d.XAddrs {
			if u, err := url.Parse(xaddr); err == nil && u.Hostname() != "" {
				discoveredByIP[u.Hostname()] = d
			}
		}
	}

	streamHosts, _ := h.Inv.GetServerCameraStreamHosts(ctx, a, r.ServerId)

	matched := 0
	updated := 0
	outCameras := make([]gen.Camera, 0, len(cams))

	for _, cam := range cams {
		var candidateIP string
		var creds provision.OnvifProbeRequest

		if cam.OnvifEndpoint != nil && *cam.OnvifEndpoint != "" {
			if u, err := url.Parse(*cam.OnvifEndpoint); err == nil {
				candidateIP = u.Hostname()
			}
		}

		if candidateIP == "" {
			if info, ok := streamHosts[cam.RemoteName]; ok && info.Host != "" {
				candidateIP = info.Host
				creds.Username = info.Username
				creds.Password = info.Password
			}
		}

		if candidateIP == "" {
			for ip := range discoveredByIP {
				if strings.Contains(strings.ToLower(cam.RemoteName), strings.ToLower(ip)) {
					candidateIP = ip
					break
				}
			}
		}

		if candidateIP != "" {
			endpoint := "http://" + candidateIP + "/onvif/device_service"
			if disc, ok := discoveredByIP[candidateIP]; ok && len(disc.XAddrs) > 0 {
				endpoint = disc.XAddrs[0]
			}
			creds.Endpoint = endpoint

			matched++

			// Try probe
			probeRes, probeErr := h.Provision.ProbeOnvif(ctx, a, r.ServerId, creds)
			if probeErr == nil {
				profile, version, caps := parseOnvifProbeCapabilities(probeRes)
				capsBytes, _ := json.Marshal(caps)
				up, err := h.Inv.UpdateCameraOnvif(ctx, a, cam.ID, &endpoint, &profile, &version, capsBytes)
				if err == nil {
					updated++
					outCameras = append(outCameras, toCamera(up))
					continue
				}
			} else {
				// Infer profile from scopes or default
				profile := "ONVIF"
				if disc, ok := discoveredByIP[candidateIP]; ok {
					for _, sc := range disc.Scopes {
						if strings.Contains(sc, "Profile/T") {
							profile = "ONVIF-T"
							break
						}
						if strings.Contains(sc, "Profile/Streaming") {
							profile = "ONVIF-S"
						}
					}
				}
				version := "2.0"
				profiles := []string{profile}
				caps := gen.CameraOnvifCapabilities{Profiles: &profiles}
				capsBytes, _ := json.Marshal(caps)
				up, err := h.Inv.UpdateCameraOnvif(ctx, a, cam.ID, &endpoint, &profile, &version, capsBytes)
				if err == nil {
					updated++
					outCameras = append(outCameras, toCamera(up))
					continue
				}
			}
		}

		outCameras = append(outCameras, toCamera(cam))
	}

	return gen.CheckServerCamerasOnvif200JSONResponse{
		Checked: len(cams),
		Matched: matched,
		Updated: updated,
		Cameras: outCameras,
	}, nil
}
