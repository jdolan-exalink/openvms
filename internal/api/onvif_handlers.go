package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jdolan-exalink/openvms/internal/agent/onvifdiscover"
	"github.com/jdolan-exalink/openvms/internal/api/gen"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/inventory"
	"github.com/jdolan-exalink/openvms/internal/onvif"
	"github.com/jdolan-exalink/openvms/internal/provision"
	"github.com/jdolan-exalink/openvms/internal/store/db"
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

	if res.Streams.MainURL != "" {
		caps.Set("main_stream_url", res.Streams.MainURL)
	}
	if res.Streams.SubURL != "" {
		caps.Set("sub_stream_url", res.Streams.SubURL)
	}
	if len(res.Profiles) > 0 {
		caps.Set("media_profiles", res.Profiles)
	}
	if len(res.Analytics.SupportedTopics) > 0 {
		caps.Set("supported_analytics", res.Analytics.SupportedTopics)
	}
	if len(res.PTZ.Presets) > 0 {
		caps.Set("ptz_presets", res.PTZ.Presets)
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

	var reqEp, reqUser, reqPass *string
	if r.Body != nil {
		reqEp = r.Body.Endpoint
		reqUser = r.Body.Username
		reqPass = r.Body.Password
	}
	endpoint, username, password := h.resolveCameraOnvifCredentials(ctx, a, cam, reqEp, reqUser, reqPass)

	if endpoint == "" {
		return gen.CheckCameraOnvif400JSONResponse{
			BadRequestJSONResponse: gen.BadRequestJSONResponse{Code: "endpoint_required", Message: "Endpoint ONVIF no proporcionado y no detectado automáticamente."},
		}, nil
	}

	if h.Provision == nil {
		return gen.CheckCameraOnvif400JSONResponse{
			BadRequestJSONResponse: gen.BadRequestJSONResponse{Code: "provision_unavailable", Message: "Servicio de provisionamiento no disponible."},
		}, nil
	}

	probeReq := provision.OnvifProbeRequest{
		Endpoint: endpoint,
		Username: username,
		Password: password,
	}

	probeRes, err := h.Provision.ProbeOnvif(ctx, a, cam.ServerID, probeReq)
	if err != nil && (reqEp == nil || *reqEp == "") {
		if u, parseErr := url.Parse(endpoint); parseErr == nil && u.Port() == "" {
			candidateEndpoints := []string{
				"http://" + u.Hostname() + ":8080/onvif/device_service",
				"http://" + u.Hostname() + ":8000/onvif/device_service",
				"http://" + u.Hostname() + ":8899/onvif/device_service",
				"http://" + u.Hostname() + ":5000/onvif/device_service",
				"http://" + u.Hostname() + "/device_service",
				"http://" + u.Hostname() + ":8080/device_service",
			}
			for _, altEp := range candidateEndpoints {
				altReq := probeReq
				altReq.Endpoint = altEp
				if altRes, altErr := h.Provision.ProbeOnvif(ctx, a, cam.ServerID, altReq); altErr == nil {
					probeRes = altRes
					endpoint = altEp
					err = nil
					break
				}
			}
		}
	}
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

func findStreamInfo(hosts map[string]inventory.CameraStreamHostInfo, names ...string) inventory.CameraStreamHostInfo {
	for _, n := range names {
		n = strings.TrimSpace(n)
		if n == "" {
			continue
		}
		if info, ok := hosts[n]; ok && (info.Host != "" || info.Username != "") {
			return info
		}
		if info, ok := hosts[strings.ToLower(n)]; ok && (info.Host != "" || info.Username != "") {
			return info
		}
		for k, info := range hosts {
			if strings.EqualFold(k, n) && (info.Host != "" || info.Username != "") {
				return info
			}
		}
	}
	return inventory.CameraStreamHostInfo{}
}

func (h *Handlers) CheckServerCamerasOnvif(ctx context.Context, r gen.CheckServerCamerasOnvifRequestObject) (gen.CheckServerCamerasOnvifResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	var ifacePtr *string
	if r.Body != nil {
		ifacePtr = r.Body.InterfaceName
	}
	res, err := h.SweepServerCamerasOnvif(ctx, a, r.ServerId, ifacePtr)
	if err != nil {
		return nil, err
	}
	return res, nil
}

func (h *Handlers) SweepServerCamerasOnvif(ctx context.Context, a authz.Actor, serverID uuid.UUID, ifaceName *string) (gen.CheckServerCamerasOnvif200JSONResponse, error) {
	if !a.System && a.UserID != uuid.Nil {
		_, err := h.Inv.RequireServerManage(ctx, a, serverID)
		if err != nil {
			return gen.CheckServerCamerasOnvif200JSONResponse{}, err
		}
	} else if a.UserID == uuid.Nil && !a.System {
		a = authz.SystemActor()
	}

	if h.Provision == nil || h.Inv == nil {
		return gen.CheckServerCamerasOnvif200JSONResponse{}, nil
	}

	cams, err := h.Inv.ListServerCameras(ctx, a, serverID)
	if err != nil {
		return gen.CheckServerCamerasOnvif200JSONResponse{}, err
	}

	iface := "*"
	if ifaceName != nil && strings.TrimSpace(*ifaceName) != "" {
		iface = strings.TrimSpace(*ifaceName)
	}

	discResult, _ := h.Provision.Discover(ctx, a, serverID, iface)
	discoveredByIP := make(map[string]provision.DiscoveryDevice)
	for _, d := range discResult.Devices {
		for _, xaddr := range d.XAddrs {
			if u, err := url.Parse(xaddr); err == nil && u.Hostname() != "" {
				discoveredByIP[u.Hostname()] = d
			}
		}
	}

	streamHosts, _ := h.Inv.GetServerCameraStreamHosts(ctx, a, serverID)
	srvUser, srvPass, _ := h.Inv.GetServerCredentials(ctx, a, serverID)

	type camResult struct {
		camera  gen.Camera
		matched bool
		updated bool
	}

	results := make([]camResult, len(cams))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)

	for i, cam := range cams {
		wg.Add(1)
		go func(idx int, c db.GetCameraRow) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			var candidateIP string
			var creds provision.OnvifProbeRequest

			streamInfo := findStreamInfo(streamHosts, c.RemoteName, c.DisplayName)
			if streamInfo.Username != "" {
				creds.Username = streamInfo.Username
				creds.Password = streamInfo.Password
			}
			if creds.Username == "" {
				for _, rawURL := range []string{c.LiveStream, c.HqStream} {
					if u, err := url.Parse(rawURL); err == nil && u.User != nil {
						creds.Username = u.User.Username()
						creds.Password, _ = u.User.Password()
						if creds.Username != "" {
							break
						}
					}
				}
			}
			if creds.Username == "" && srvUser != "" {
				creds.Username = srvUser
				creds.Password = srvPass
			}

			if c.OnvifEndpoint != nil && *c.OnvifEndpoint != "" {
				if u, err := url.Parse(*c.OnvifEndpoint); err == nil {
					candidateIP = u.Hostname()
				}
			}

			if candidateIP == "" && streamInfo.Host != "" {
				candidateIP = streamInfo.Host
			}

			if candidateIP == "" {
				for ip := range discoveredByIP {
					if strings.Contains(strings.ToLower(c.RemoteName), strings.ToLower(ip)) ||
						strings.Contains(strings.ToLower(c.DisplayName), strings.ToLower(ip)) {
						candidateIP = ip
						break
					}
				}
			}

			if candidateIP != "" {
				endpoint := "http://" + candidateIP + "/onvif/device_service"
				if disc, ok := discoveredByIP[candidateIP]; ok && len(disc.XAddrs) > 0 {
					endpoint = disc.XAddrs[0]
				} else if c.OnvifEndpoint != nil && *c.OnvifEndpoint != "" {
					endpoint = *c.OnvifEndpoint
				}
				creds.Endpoint = endpoint

				// Try probe
				probeRes, probeErr := h.Provision.ProbeOnvif(ctx, a, serverID, creds)
				if probeErr != nil {
					candidateEndpoints := []string{
						"http://" + candidateIP + ":8080/onvif/device_service",
						"http://" + candidateIP + ":8000/onvif/device_service",
						"http://" + candidateIP + ":8899/onvif/device_service",
						"http://" + candidateIP + ":5000/onvif/device_service",
						"http://" + candidateIP + "/device_service",
						"http://" + candidateIP + ":8080/device_service",
					}
					for _, altEp := range candidateEndpoints {
						if altEp == creds.Endpoint {
							continue
						}
						altCreds := creds
						altCreds.Endpoint = altEp
						if altRes, altErr := h.Provision.ProbeOnvif(ctx, a, serverID, altCreds); altErr == nil {
							probeRes = altRes
							probeErr = nil
							endpoint = altEp
							break
						}
					}
				}
				if probeErr == nil {
					profile, version, caps := parseOnvifProbeCapabilities(probeRes)
					capsBytes, _ := json.Marshal(caps)
					up, err := h.Inv.UpdateCameraOnvif(ctx, a, c.ID, &endpoint, &profile, &version, capsBytes)
					if err == nil {
						results[idx] = camResult{camera: toCamera(up), matched: true, updated: true}
						return
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
					up, err := h.Inv.UpdateCameraOnvif(ctx, a, c.ID, &endpoint, &profile, &version, capsBytes)
					if err == nil {
						results[idx] = camResult{camera: toCamera(up), matched: true, updated: true}
						return
					}
				}
				results[idx] = camResult{camera: toCamera(c), matched: true, updated: false}
				return
			}

			results[idx] = camResult{camera: toCamera(c), matched: false, updated: false}
		}(i, cam)
	}
	wg.Wait()

	matched := 0
	updated := 0
	outCameras := make([]gen.Camera, len(results))
	for i, res := range results {
		if res.matched {
			matched++
		}
		if res.updated {
			updated++
		}
		outCameras[i] = res.camera
	}

	return gen.CheckServerCamerasOnvif200JSONResponse{
		Checked: len(cams),
		Matched: matched,
		Updated: updated,
		Cameras: outCameras,
	}, nil
}

func (h *Handlers) resolveCameraOnvifCredentials(ctx context.Context, a authz.Actor, cam db.GetCameraRow, explicitEndpoint, explicitUser, explicitPass *string) (string, string, string) {
	var endpoint string
	var username string
	var password string

	if explicitEndpoint != nil {
		endpoint = strings.TrimSpace(*explicitEndpoint)
	}
	if explicitUser != nil {
		username = strings.TrimSpace(*explicitUser)
	}
	if explicitPass != nil {
		password = *explicitPass
	}

	if endpoint == "" && cam.OnvifEndpoint != nil {
		endpoint = strings.TrimSpace(*cam.OnvifEndpoint)
	}

	hosts, _ := h.Inv.GetServerCameraStreamHosts(ctx, a, cam.ServerID)
	streamInfo := findStreamInfo(hosts, cam.RemoteName, cam.DisplayName)

	if endpoint == "" && streamInfo.Host != "" {
		endpoint = "http://" + streamInfo.Host + "/onvif/device_service"
	}

	if username == "" && streamInfo.Username != "" {
		username = streamInfo.Username
		password = streamInfo.Password
	}

	if username == "" {
		for _, rawURL := range []string{cam.LiveStream, cam.HqStream} {
			if u, err := url.Parse(rawURL); err == nil && u.User != nil {
				username = u.User.Username()
				password, _ = u.User.Password()
				if username != "" {
					break
				}
			}
		}
	}

	if username == "" {
		if srvUser, srvPass, err := h.Inv.GetServerCredentials(ctx, a, cam.ServerID); err == nil && srvUser != "" {
			username = srvUser
			password = srvPass
		}
	}

	return endpoint, username, password
}

func (h *Handlers) AddCameraToFrigate(ctx context.Context, r gen.AddCameraToFrigateRequestObject) (gen.AddCameraToFrigateResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if r.Body == nil {
		return gen.AddCameraToFrigate400JSONResponse{
			BadRequestJSONResponse: gen.BadRequestJSONResponse{Code: "invalid_request", Message: "Request body is required"},
		}, nil
	}

	var title, subURL, onvifHost, onvifUser, onvifPass string
	var onvifPort int
	if r.Body.Title != nil {
		title = *r.Body.Title
	}
	if r.Body.SubStreamUrl != nil {
		subURL = *r.Body.SubStreamUrl
	}
	if r.Body.OnvifHost != nil {
		onvifHost = *r.Body.OnvifHost
	}
	if r.Body.OnvifPort != nil {
		onvifPort = *r.Body.OnvifPort
	}
	if r.Body.OnvifUser != nil {
		onvifUser = *r.Body.OnvifUser
	}
	if r.Body.OnvifPassword != nil {
		onvifPass = *r.Body.OnvifPassword
	}

	res, err := h.Inv.AddCameraToFrigate(ctx, a, r.ServerId, inventory.AddCameraToFrigateInput{
		Name:          r.Body.Name,
		Title:         title,
		MainStreamURL: r.Body.MainStreamUrl,
		SubStreamURL:  subURL,
		OnvifHost:     onvifHost,
		OnvifPort:     onvifPort,
		OnvifUser:     onvifUser,
		OnvifPassword: onvifPass,
	})
	if err != nil {
		return gen.AddCameraToFrigate400JSONResponse{
			BadRequestJSONResponse: gen.BadRequestJSONResponse{Code: "add_camera_failed", Message: err.Error()},
		}, nil
	}

	resp := gen.AddCameraToFrigate200JSONResponse{
		RevisionId:      res.RevisionID,
		RestartRequired: res.RestartRequired,
	}
	if res.Camera != nil {
		cam := toCamera(*res.Camera)
		resp.Camera = &cam

		var ep *string
		if onvifHost != "" {
			port := 80
			if onvifPort > 0 {
				port = onvifPort
			}
			e := fmt.Sprintf("http://%s:%d/onvif/device_service", onvifHost, port)
			ep = &e
		}
		var uPtr, pPtr *string
		if onvifUser != "" {
			uPtr = &onvifUser
		}
		if onvifPass != "" {
			pPtr = &onvifPass
		}
		checkObj, checkErr := h.CheckCameraOnvif(ctx, gen.CheckCameraOnvifRequestObject{
			CameraId: res.Camera.ID,
			Body: &gen.CheckCameraOnvifJSONRequestBody{
				Endpoint: ep,
				Username: uPtr,
				Password: pPtr,
			},
		})
		if checkErr == nil {
			if okResp, ok := checkObj.(gen.CheckCameraOnvif200JSONResponse); ok {
				camCopy := gen.Camera(okResp)
				resp.Camera = &camCopy
			}
		}
	}
	return resp, nil
}

func (h *Handlers) SyncCameraOnvifTime(ctx context.Context, r gen.SyncCameraOnvifTimeRequestObject) (gen.SyncCameraOnvifTimeResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	cam, err := h.Inv.GetCamera(ctx, a, r.CameraId)
	if err != nil {
		return nil, err
	}

	var reqEp, reqUser, reqPass *string
	if r.Body != nil {
		reqEp = r.Body.Endpoint
		reqUser = r.Body.Username
		reqPass = r.Body.Password
	}
	ep, user, pass := h.resolveCameraOnvifCredentials(ctx, a, cam, reqEp, reqUser, reqPass)
	if ep == "" {
		return gen.SyncCameraOnvifTime400JSONResponse{
			BadRequestJSONResponse: gen.BadRequestJSONResponse{Code: "endpoint_required", Message: "Camera ONVIF endpoint could not be determined"},
		}, nil
	}

	out, err := h.Provision.ExecuteOnvifSyncTime(ctx, a, cam.ServerID, onvifdiscover.SyncTimeRequest{
		Endpoint: ep,
		Username: user,
		Password: pass,
	})
	if err != nil {
		return gen.SyncCameraOnvifTime502JSONResponse{
			FrigateUnreachableJSONResponse: gen.FrigateUnreachableJSONResponse{Code: "sync_time_failed", Message: err.Error()},
		}, nil
	}

	syncedAt := time.Now().UTC()
	if s, ok := out["synced_at"].(string); ok {
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			syncedAt = t
		}
	}
	return gen.SyncCameraOnvifTime200JSONResponse{
		Success:  true,
		SyncedAt: syncedAt,
	}, nil
}

func (h *Handlers) ControlCameraOnvifPTZ(ctx context.Context, r gen.ControlCameraOnvifPTZRequestObject) (gen.ControlCameraOnvifPTZResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	cam, err := h.Inv.GetCamera(ctx, a, r.CameraId)
	if err != nil {
		return nil, err
	}
	if r.Body == nil {
		return gen.ControlCameraOnvifPTZ400JSONResponse{
			BadRequestJSONResponse: gen.BadRequestJSONResponse{Code: "invalid_request", Message: "Request body is required"},
		}, nil
	}

	ep, user, pass := h.resolveCameraOnvifCredentials(ctx, a, cam, r.Body.Endpoint, r.Body.Username, r.Body.Password)
	if ep == "" {
		return gen.ControlCameraOnvifPTZ400JSONResponse{
			BadRequestJSONResponse: gen.BadRequestJSONResponse{Code: "endpoint_required", Message: "Camera ONVIF endpoint could not be determined"},
		}, nil
	}

	ptzReq := onvifdiscover.PTZRequest{
		Endpoint: ep,
		Username: user,
		Password: pass,
		Action:   string(r.Body.Action),
	}
	if r.Body.ProfileToken != nil {
		ptzReq.ProfileToken = *r.Body.ProfileToken
	}
	if r.Body.Pan != nil {
		ptzReq.Pan = float64(*r.Body.Pan)
	}
	if r.Body.Tilt != nil {
		ptzReq.Tilt = float64(*r.Body.Tilt)
	}
	if r.Body.Zoom != nil {
		ptzReq.Zoom = float64(*r.Body.Zoom)
	}
	if r.Body.PresetToken != nil {
		ptzReq.PresetToken = *r.Body.PresetToken
	}

	out, err := h.Provision.ExecuteOnvifPTZ(ctx, a, cam.ServerID, ptzReq)
	if err != nil {
		return gen.ControlCameraOnvifPTZ502JSONResponse{
			FrigateUnreachableJSONResponse: gen.FrigateUnreachableJSONResponse{Code: "ptz_failed", Message: err.Error()},
		}, nil
	}

	var presets []struct {
		Name  *string `json:"name,omitempty"`
		Token *string `json:"token,omitempty"`
	}
	if pList, ok := out["presets"].([]any); ok {
		for _, item := range pList {
			if m, ok := item.(map[string]any); ok {
				tokenStr, _ := m["token"].(string)
				nameStr, _ := m["name"].(string)
				presets = append(presets, struct {
					Name  *string `json:"name,omitempty"`
					Token *string `json:"token,omitempty"`
				}{
					Name:  &nameStr,
					Token: &tokenStr,
				})
			}
		}
	}

	resp := gen.ControlCameraOnvifPTZ200JSONResponse{
		Success: true,
	}
	if len(presets) > 0 {
		resp.Presets = &presets
	}
	return resp, nil
}

func (h *Handlers) OperateCameraOnvifVideoConfig(ctx context.Context, r gen.OperateCameraOnvifVideoConfigRequestObject) (gen.OperateCameraOnvifVideoConfigResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	cam, err := h.Inv.GetCamera(ctx, a, r.CameraId)
	if err != nil {
		return nil, err
	}
	if r.Body == nil {
		return gen.OperateCameraOnvifVideoConfig400JSONResponse{
			BadRequestJSONResponse: gen.BadRequestJSONResponse{Code: "invalid_request", Message: "Request body is required"},
		}, nil
	}

	ep, user, pass := h.resolveCameraOnvifCredentials(ctx, a, cam, r.Body.Endpoint, r.Body.Username, r.Body.Password)
	if ep == "" {
		return gen.OperateCameraOnvifVideoConfig400JSONResponse{
			BadRequestJSONResponse: gen.BadRequestJSONResponse{Code: "endpoint_required", Message: "Camera ONVIF endpoint could not be determined"},
		}, nil
	}

	vReq := onvifdiscover.VideoConfigRequest{
		Endpoint: ep,
		Username: user,
		Password: pass,
		Action:   string(r.Body.Action),
	}
	if r.Body.Configuration != nil {
		c := r.Body.Configuration
		cfg := &onvif.VideoEncoderConfiguration{}
		if c.Token != nil {
			cfg.Token = *c.Token
		}
		if c.Name != nil {
			cfg.Name = *c.Name
		}
		if c.Encoding != nil {
			cfg.Encoding = *c.Encoding
		}
		if c.Width != nil {
			cfg.Resolution.Width = *c.Width
		}
		if c.Height != nil {
			cfg.Resolution.Height = *c.Height
		}
		if c.Fps != nil {
			cfg.RateControl.FrameRateLimit = int(*c.Fps)
		}
		if c.Bitrate != nil {
			cfg.RateControl.BitrateLimit = *c.Bitrate
		}
		vReq.Configuration = cfg
	}

	out, err := h.Provision.ExecuteOnvifVideoConfig(ctx, a, cam.ServerID, vReq)
	if err != nil {
		return gen.OperateCameraOnvifVideoConfig502JSONResponse{
			FrigateUnreachableJSONResponse: gen.FrigateUnreachableJSONResponse{Code: "video_config_failed", Message: err.Error()},
		}, nil
	}

	var configs []struct {
		Bitrate  *int     `json:"bitrate,omitempty"`
		Encoding *string  `json:"encoding,omitempty"`
		Fps      *float32 `json:"fps,omitempty"`
		Height   *int     `json:"height,omitempty"`
		Name     *string  `json:"name,omitempty"`
		Token    *string  `json:"token,omitempty"`
		Width    *int     `json:"width,omitempty"`
	}
	if cList, ok := out["configurations"].([]any); ok {
		for _, item := range cList {
			if m, ok := item.(map[string]any); ok {
				var token, name, encoding string
				var width, height, bitrate int
				var fps float32
				if v, ok := m["token"].(string); ok {
					token = v
				}
				if v, ok := m["name"].(string); ok {
					name = v
				}
				if v, ok := m["encoding"].(string); ok {
					encoding = v
				}
				if v, ok := m["width"].(float64); ok {
					width = int(v)
				}
				if v, ok := m["height"].(float64); ok {
					height = int(v)
				}
				if v, ok := m["frame_rate"].(float64); ok {
					fps = float32(v)
				}
				if v, ok := m["bitrate_limit"].(float64); ok {
					bitrate = int(v)
				}
				configs = append(configs, struct {
					Bitrate  *int     `json:"bitrate,omitempty"`
					Encoding *string  `json:"encoding,omitempty"`
					Fps      *float32 `json:"fps,omitempty"`
					Height   *int     `json:"height,omitempty"`
					Name     *string  `json:"name,omitempty"`
					Token    *string  `json:"token,omitempty"`
					Width    *int     `json:"width,omitempty"`
				}{
					Bitrate:  &bitrate,
					Encoding: &encoding,
					Fps:      &fps,
					Height:   &height,
					Name:     &name,
					Token:    &token,
					Width:    &width,
				})
			}
		}
	}

	resp := gen.OperateCameraOnvifVideoConfig200JSONResponse{
		Success: true,
	}
	if len(configs) > 0 {
		resp.Configurations = &configs
	}
	return resp, nil
}

func (h *Handlers) OperateCameraOnvifImaging(ctx context.Context, r gen.OperateCameraOnvifImagingRequestObject) (gen.OperateCameraOnvifImagingResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	cam, err := h.Inv.GetCamera(ctx, a, r.CameraId)
	if err != nil {
		return nil, err
	}
	if r.Body == nil {
		return gen.OperateCameraOnvifImaging400JSONResponse{
			BadRequestJSONResponse: gen.BadRequestJSONResponse{Code: "invalid_request", Message: "Request body is required"},
		}, nil
	}

	ep, user, pass := h.resolveCameraOnvifCredentials(ctx, a, cam, r.Body.Endpoint, r.Body.Username, r.Body.Password)
	if ep == "" {
		return gen.OperateCameraOnvifImaging400JSONResponse{
			BadRequestJSONResponse: gen.BadRequestJSONResponse{Code: "endpoint_required", Message: "Camera ONVIF endpoint could not be determined"},
		}, nil
	}

	videoSourceToken := "VideoSourceToken"
	if r.Body.VideoSourceToken != nil && *r.Body.VideoSourceToken != "" {
		videoSourceToken = *r.Body.VideoSourceToken
	}

	imgReq := onvifdiscover.ImagingRequest{
		Endpoint:         ep,
		Username:         user,
		Password:         pass,
		VideoSourceToken: videoSourceToken,
		Action:           string(r.Body.Action),
	}
	if r.Body.Settings != nil {
		s := r.Body.Settings
		settings := &onvif.ImagingSettings{}
		if s.Brightness != nil {
			settings.Brightness = float64(*s.Brightness)
		}
		if s.ColorSaturation != nil {
			settings.ColorSaturation = float64(*s.ColorSaturation)
		}
		if s.Contrast != nil {
			settings.Contrast = float64(*s.Contrast)
		}
		if s.Sharpness != nil {
			settings.Sharpness = float64(*s.Sharpness)
		}
		if s.IrCutFilter != nil {
			settings.IrCutFilter = *s.IrCutFilter
		}
		if s.WdrMode != nil || s.WdrLevel != nil {
			wdr := &onvif.WideDynamicRange{}
			if s.WdrMode != nil {
				wdr.Mode = *s.WdrMode
			}
			if s.WdrLevel != nil {
				wdr.Level = float64(*s.WdrLevel)
			}
			settings.WideDynamicRange = wdr
		}
		imgReq.Settings = settings
	}

	out, err := h.Provision.ExecuteOnvifImaging(ctx, a, cam.ServerID, imgReq)
	if err != nil {
		return gen.OperateCameraOnvifImaging502JSONResponse{
			FrigateUnreachableJSONResponse: gen.FrigateUnreachableJSONResponse{Code: "imaging_failed", Message: err.Error()},
		}, nil
	}

	resp := gen.OperateCameraOnvifImaging200JSONResponse{
		Success: true,
	}
	if sMap, ok := out["settings"].(map[string]any); ok {
		var brightness, colorSat, contrast, sharpness, wdrLevel float32
		var irCutFilter, wdrMode string
		if v, ok := sMap["brightness"].(float64); ok {
			brightness = float32(v)
		}
		if v, ok := sMap["color_saturation"].(float64); ok {
			colorSat = float32(v)
		}
		if v, ok := sMap["contrast"].(float64); ok {
			contrast = float32(v)
		}
		if v, ok := sMap["sharpness"].(float64); ok {
			sharpness = float32(v)
		}
		if v, ok := sMap["ir_cut_filter"].(string); ok {
			irCutFilter = v
		}
		if v, ok := sMap["wdr_mode"].(string); ok {
			wdrMode = v
		}
		if v, ok := sMap["wdr_level"].(float64); ok {
			wdrLevel = float32(v)
		}
		resp.Settings = &struct {
			Brightness      *float32 `json:"brightness,omitempty"`
			ColorSaturation *float32 `json:"color_saturation,omitempty"`
			Contrast        *float32 `json:"contrast,omitempty"`
			IrCutFilter     *string  `json:"ir_cut_filter,omitempty"`
			Sharpness       *float32 `json:"sharpness,omitempty"`
			WdrLevel        *float32 `json:"wdr_level,omitempty"`
			WdrMode         *string  `json:"wdr_mode,omitempty"`
		}{
			Brightness:      &brightness,
			ColorSaturation: &colorSat,
			Contrast:        &contrast,
			IrCutFilter:     &irCutFilter,
			Sharpness:       &sharpness,
			WdrLevel:        &wdrLevel,
			WdrMode:         &wdrMode,
		}
	}

	return resp, nil
}
