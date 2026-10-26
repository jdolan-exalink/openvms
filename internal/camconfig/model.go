// Package camconfig defines the engine-agnostic camera configuration model OpenVMS exposes
// to its UI, and the adapters that build it from a concrete engine (Frigate today).
//
// The model is what OpenVMS means by "a camera's configuration"; adapters translate engine
// documents into it, so no UI code depends on engine structures. Fields an adapter cannot
// establish stay at their zero value (omitted in JSON): nothing is invented.
package camconfig

import "context"

// Capability names reported in Camera.Capabilities.
const (
	CapONVIF           = "onvif"
	CapAudio           = "audio"
	CapPTZ             = "ptz"
	CapLPR             = "lpr"
	CapFisheye         = "fisheye"
	CapObjectDetection = "object_detection"
	CapRecording       = "recording"
	CapAnalytics       = "analytics"
	CapEvents          = "events"
)

// Source statuses reported in Source.Status.
const (
	SourceOK          = "ok"
	SourceUnavailable = "unavailable"
)

// Camera is the normalized configuration of one camera.
type Camera struct {
	Identity     Identity      `json:"identity"`
	Origin       Origin        `json:"origin"`
	Device       Device        `json:"device"`
	Capabilities []string      `json:"capabilities"`
	Streams      Streams       `json:"streams"`
	Recording    Recording     `json:"recording"`
	Detection    Detection     `json:"detection"`
	Analytics    Analytics     `json:"analytics"`
	Zones        []Zone        `json:"zones"`
	PTZ          PTZ           `json:"ptz"`
	Fisheye      Fisheye       `json:"fisheye"`
	Integrations []Integration `json:"integrations"`
	Overrides    Overrides     `json:"overrides"`
	// State compares the desired configuration kept by OpenVMS with this (effective) read. The
	// adapters leave it zero; the service fills it in.
	State State `json:"state"`
	// Sources tells where each part of the model came from and whether it could be read.
	Sources  []Source `json:"sources"`
	Warnings []string `json:"warnings"`
}

// Identity is how the camera is named and classified inside OpenVMS.
type Identity struct {
	CameraID    string   `json:"camera_id"`
	Name        string   `json:"name"`
	RemoteName  string   `json:"remote_name"`
	Description string   `json:"description,omitempty"`
	Location    string   `json:"location,omitempty"`
	Tags        []string `json:"tags"`
	Enabled     bool     `json:"enabled"`
}

// Origin is the engine and server that run the camera.
type Origin struct {
	Engine        string `json:"engine"`
	EngineVersion string `json:"engine_version,omitempty"`
	ServerID      string `json:"server_id"`
	ServerName    string `json:"server_name,omitempty"`
}

// Device is the physical camera, as far as ONVIF told us.
type Device struct {
	Manufacturer string `json:"manufacturer,omitempty"`
	Model        string `json:"model,omitempty"`
	Firmware     string `json:"firmware,omitempty"`
	SerialNumber string `json:"serial_number,omitempty"`
	Address      string `json:"address,omitempty"`
	ONVIFVersion string `json:"onvif_version,omitempty"`
	ONVIFProfile string `json:"onvif_profile,omitempty"`
}

// Streams are the camera's video streams by role. Recording is only set when it uses a
// different stream than Primary.
type Streams struct {
	Primary   *Stream `json:"primary,omitempty"`
	Detection *Stream `json:"detection,omitempty"`
	Recording *Stream `json:"recording,omitempty"`
}

// Stream describes one video stream. URL never carries credentials or query strings.
type Stream struct {
	Name   string  `json:"name,omitempty"`
	URL    string  `json:"url,omitempty"`
	Codec  string  `json:"codec,omitempty"`
	Width  int     `json:"width,omitempty"`
	Height int     `json:"height,omitempty"`
	FPS    float64 `json:"fps,omitempty"`
	// BitrateKbps and GOP come from the camera (ONVIF) when it could be read.
	BitrateKbps int      `json:"bitrate_kbps,omitempty"`
	GOP         int      `json:"gop,omitempty"`
	Roles       []string `json:"roles"`
}

// Recording is the recording policy; Enabled is nil when unknown.
type Recording struct {
	Enabled *bool `json:"enabled,omitempty"`
}

// Detection is the object detection policy; Enabled is nil when unknown.
type Detection struct {
	Enabled *bool    `json:"enabled,omitempty"`
	Objects []string `json:"objects"`
}

// Analytics are the optional analytics running on the camera; nil means unknown.
type Analytics struct {
	LPR   *bool `json:"lpr,omitempty"`
	Audio *bool `json:"audio,omitempty"`
}

// Zone is a named detection zone.
type Zone struct {
	Name    string   `json:"name"`
	Objects []string `json:"objects"`
}

// PTZ reports pan-tilt-zoom support.
type PTZ struct {
	Supported    bool  `json:"supported"`
	Autotracking *bool `json:"autotracking,omitempty"`
}

// Fisheye reports whether the camera is marked as a fisheye lens.
type Fisheye struct {
	Supported bool `json:"supported"`
}

// Integration is a feature of the platform wired to the camera.
type Integration struct {
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
}

// Overrides are the OpenVMS-side settings that take precedence over engine values.
type Overrides struct {
	DefaultLiveQuality string `json:"default_live_quality,omitempty"`
}

// Source says whether a part of the model could be read.
type Source struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

// Inventory is what OpenVMS itself knows about a camera, independent of any engine.
type Inventory struct {
	CameraID           string
	Name               string
	RemoteName         string
	Description        string
	Location           string
	Tags               []string
	Enabled            bool
	LPR                bool
	LiveStream         string
	HQStream           string
	LiveCodec          string
	HQCodec            string
	DefaultLiveQuality string
	ServerID           string
	ServerName         string
	ONVIFEndpoint      string
	ONVIFProfile       string
	ONVIFVersion       string
	// ONVIFCapabilities is the stored onvif_capabilities JSON document, possibly empty.
	ONVIFCapabilities []byte
}

// Adapter builds the normalized model of a camera from one engine.
//
// Read never fails because the engine is down: it returns what inventory knows and marks the
// engine source unavailable. The error is reserved for unusable input.
//
// Plan is pure: it compares a patch with the current model and describes what would change and
// what it would affect, without touching the engine. Apply writes the patch to the engine;
// callers verify the result by reading again.
type Adapter interface {
	Read(ctx context.Context, in Inventory) (Camera, error)
	Plan(ctx context.Context, current Camera, patch Patch) (Review, error)
	Apply(ctx context.Context, patch Patch) error
}
