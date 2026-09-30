package maps

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
)

// ProviderConfig describes a tile provider configuration.
type ProviderConfig struct {
	ID            string   `json:"id"`
	Kind          string   `json:"kind"`
	StyleURLLight *string  `json:"style_url_light,omitempty"`
	StyleURLDark  *string  `json:"style_url_dark,omitempty"`
	Tiles         []string `json:"tiles,omitempty"`
	Attribution   string   `json:"attribution"`
	MaxZoom       int      `json:"max_zoom"`
	Offline       bool     `json:"offline"`
}

// DefaultCenter describes the default geographic center coordinate.
type DefaultCenter struct {
	Lat float64 `json:"lat"`
	Lng float64 `json:"lng"`
}

// Config holds map system configuration.
type Config struct {
	Provider      ProviderConfig `json:"provider"`
	DefaultCenter DefaultCenter  `json:"default_center"`
	DefaultZoom   int            `json:"default_zoom"`
}

// DefaultConfig returns the default self-hosted offline PMTiles map configuration.
func DefaultConfig() Config {
	return Config{
		Provider: ProviderConfig{
			ID:          "protomaps-local",
			Kind:        "pmtiles",
			Tiles:       []string{"/tiles/world.pmtiles"},
			Attribution: "© OpenStreetMap contributors",
			MaxZoom:     18,
			Offline:     true,
		},
		DefaultCenter: DefaultCenter{
			Lat: 0,
			Lng: 0,
		},
		DefaultZoom: 2,
	}
}

// SiteOverview provides aggregate health and geo info for a site.
type SiteOverview struct {
	ID              uuid.UUID  `json:"id"`
	Name            string     `json:"name"`
	Lat             *float64   `json:"lat,omitempty"`
	Lng             *float64   `json:"lng,omitempty"`
	DefaultZoom     *float32   `json:"default_zoom,omitempty"`
	RegionID        *uuid.UUID `json:"region_id,omitempty"`
	RegionName      *string    `json:"region_name,omitempty"`
	CameraCount     int        `json:"camera_count"`
	OnlineCameras   int        `json:"online_cameras"`
	OfflineCameras  int        `json:"offline_cameras"`
	DegradedCameras int        `json:"degraded_cameras"`
	AlarmCount      int        `json:"alarm_count"`
}

// Floor represents a building floor.
type Floor struct {
	ID              uuid.UUID       `json:"id"`
	BuildingID      uuid.UUID       `json:"building_id"`
	Name            string          `json:"name"`
	Ordinal         int             `json:"ordinal"`
	PlanKey         *string         `json:"plan_key,omitempty"`
	PlanContentType *string         `json:"plan_content_type,omitempty"`
	PlanWidthPx     *int            `json:"plan_width_px,omitempty"`
	PlanHeightPx    *int            `json:"plan_height_px,omitempty"`
	Georef          json.RawMessage `json:"georef,omitempty"`
}

// Building represents a physical building on a site.
type Building struct {
	ID        uuid.UUID       `json:"id"`
	SiteID    uuid.UUID       `json:"site_id"`
	Name      string          `json:"name"`
	Footprint json.RawMessage `json:"footprint,omitempty"`
	Lat       *float64        `json:"lat,omitempty"`
	Lng       *float64        `json:"lng,omitempty"`
	Floors    []Floor         `json:"floors"`
}

// Zone represents a security zone (geo or floor-level).
type Zone struct {
	ID       uuid.UUID       `json:"id"`
	SiteID   uuid.UUID       `json:"site_id"`
	FloorID  *uuid.UUID      `json:"floor_id,omitempty"`
	Name     string          `json:"name"`
	Kind     string          `json:"kind"`
	Geometry json.RawMessage `json:"geometry"`
	MinLat   *float64        `json:"min_lat,omitempty"`
	MinLng   *float64        `json:"min_lng,omitempty"`
	MaxLat   *float64        `json:"max_lat,omitempty"`
	MaxLng   *float64        `json:"max_lng,omitempty"`
	Style    json.RawMessage `json:"style,omitempty"`
	Metadata json.RawMessage `json:"metadata,omitempty"`
}

// SiteDetails contains structural map data for a single site.
type SiteDetails struct {
	ID          uuid.UUID  `json:"id"`
	Name        string     `json:"name"`
	Lat         *float64   `json:"lat,omitempty"`
	Lng         *float64   `json:"lng,omitempty"`
	DefaultZoom *float32   `json:"default_zoom,omitempty"`
	RegionID    *uuid.UUID `json:"region_id,omitempty"`
	Buildings   []Building `json:"buildings"`
	Zones       []Zone     `json:"zones"`
}

// EntitiesFilter filters placed entities.
type EntitiesFilter struct {
	FloorID *uuid.UUID
	IsGeo   *bool
	MinLat  *float64
	MinLng  *float64
	MaxLat  *float64
	MaxLng  *float64
}

// Position is a tagged union for geo or floor position.
type Position struct {
	Kind    string     `json:"k"`
	Lat     *float64   `json:"lat,omitempty"`
	Lng     *float64   `json:"lng,omitempty"`
	FloorID *uuid.UUID `json:"floor_id,omitempty"`
	X       *float32   `json:"x,omitempty"`
	Y       *float32   `json:"y,omitempty"`
}

// CameraProps contains camera specific map properties.
type CameraProps struct {
	Bearing float32 `json:"bearing"`
	Fov     float32 `json:"fov"`
	Range   float32 `json:"range"`
	Type    string  `json:"type"`
	PTZ     bool    `json:"ptz"`
	LPR     bool    `json:"lpr"`
}

// Entity represents a placed entity on the map.
type Entity struct {
	ID       uuid.UUID    `json:"id"`
	Type     string       `json:"t"`
	SiteID   uuid.UUID    `json:"site"`
	ServerID *uuid.UUID   `json:"srv,omitempty"`
	Name     string       `json:"name"`
	Position Position     `json:"pos"`
	Camera   *CameraProps `json:"cam,omitempty"`
	Status   string       `json:"st"`
	Alarms   int          `json:"alarms"`
}

// EntitiesResult wraps the collection of placed entities and the current revision.
type EntitiesResult struct {
	Revision int64    `json:"revision"`
	Entities []Entity `json:"entities"`
}

// UnplacedCamera represents a camera without geographic placement.
type UnplacedCamera struct {
	ID         uuid.UUID `json:"id"`
	Name       string    `json:"name"`
	RemoteName *string   `json:"remote_name,omitempty"`
	SiteID     uuid.UUID `json:"site_id"`
	Status     string    `json:"status"`
}

// Placement represents a placed entity (camera, server, device) in a site.
type Placement struct {
	ID         uuid.UUID       `json:"id"`
	SiteID     uuid.UUID       `json:"site_id"`
	EntityType string          `json:"entity_type"`
	EntityID   uuid.UUID       `json:"entity_id"`
	FloorID    *uuid.UUID      `json:"floor_id,omitempty"`
	Lat        *float64        `json:"lat,omitempty"`
	Lng        *float64        `json:"lng,omitempty"`
	X          *float32        `json:"x,omitempty"`
	Y          *float32        `json:"y,omitempty"`
	BearingDeg *float32        `json:"bearing_deg,omitempty"`
	FovDeg     *float32        `json:"fov_deg,omitempty"`
	RangeM     *float32        `json:"range_m,omitempty"`
	Props      json.RawMessage `json:"props,omitempty"`
	Revision   int64           `json:"revision"`
	CreatedAt  time.Time       `json:"created_at"`
	UpdatedAt  time.Time       `json:"updated_at"`
}

// UpsertPlacementRequest specifies coordinates and attributes for entity placement.
type UpsertPlacementRequest struct {
	SiteID     uuid.UUID       `json:"site_id"`
	FloorID    *uuid.UUID      `json:"floor_id,omitempty"`
	Lat        *float64        `json:"lat,omitempty"`
	Lng        *float64        `json:"lng,omitempty"`
	X          *float32        `json:"x,omitempty"`
	Y          *float32        `json:"y,omitempty"`
	BearingDeg *float32        `json:"bearing_deg,omitempty"`
	FovDeg     *float32        `json:"fov_deg,omitempty"`
	RangeM     *float32        `json:"range_m,omitempty"`
	Props      json.RawMessage `json:"props,omitempty"`
}

// UpdateSiteGeoRequest specifies geographic updates for a site.
type UpdateSiteGeoRequest struct {
	Lat         *float64   `json:"lat,omitempty"`
	Lng         *float64   `json:"lng,omitempty"`
	DefaultZoom *float32   `json:"default_zoom,omitempty"`
	RegionID    *uuid.UUID `json:"region_id,omitempty"`
}

// SiteGeo represents geographic and overview properties of a site.
type SiteGeo struct {
	ID          uuid.UUID  `json:"id"`
	Name        string     `json:"name"`
	Lat         *float64   `json:"lat,omitempty"`
	Lng         *float64   `json:"lng,omitempty"`
	DefaultZoom *float32   `json:"default_zoom,omitempty"`
	RegionID    *uuid.UUID `json:"region_id,omitempty"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// ValidationError represents an input validation error.
type ValidationError struct {
	Msg string
}

func (e *ValidationError) Error() string {
	return e.Msg
}

var (
	// ErrOptimisticLockConflict is returned when an If-Match revision does not match current state.
	ErrOptimisticLockConflict = errors.New("optimistic lock conflict: revision mismatch")
)
