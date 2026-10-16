package inventory

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/google/uuid"
)

// ErrCameraGroupVersionConflict is returned when a camera group update carries a stale version.
var ErrCameraGroupVersionConflict = errors.New("camera group was modified by someone else; reload and retry with the current version")

const (
	maxGroupFilterValues = 100
	maxGroupFilterName   = 200
	maxGroupFilterTag    = 64
)

// CameraGroupFilter selects the cameras of a dynamic group. Fields combine with AND; the values
// of one field combine with OR. Tags match case-insensitively, NameContains is a case-insensitive
// substring of the camera display name. At least one field must be set.
type CameraGroupFilter struct {
	SiteIDs      []uuid.UUID `json:"site_ids,omitempty"`
	ServerIDs    []uuid.UUID `json:"server_ids,omitempty"`
	Tags         []string    `json:"tags,omitempty"`
	NameContains string      `json:"name_contains,omitempty"`
}

// normalizeCameraGroupFilter trims and de-duplicates the filter and rejects an empty or oversized one.
func normalizeCameraGroupFilter(in CameraGroupFilter) (CameraGroupFilter, error) {
	out := CameraGroupFilter{NameContains: strings.TrimSpace(in.NameContains)}
	if len(in.SiteIDs) > maxGroupFilterValues || len(in.ServerIDs) > maxGroupFilterValues || len(in.Tags) > maxGroupFilterValues {
		return out, invalid("filter lists accept at most %d values", maxGroupFilterValues)
	}
	for _, id := range in.SiteIDs {
		if id == uuid.Nil {
			return out, invalid("filter.site_ids contains an invalid id")
		}
	}
	for _, id := range in.ServerIDs {
		if id == uuid.Nil {
			return out, invalid("filter.server_ids contains an invalid id")
		}
	}
	out.SiteIDs = dedupe(in.SiteIDs)
	out.ServerIDs = dedupe(in.ServerIDs)
	seen := map[string]bool{}
	for _, raw := range in.Tags {
		tag := strings.TrimSpace(raw)
		key := strings.ToLower(tag)
		if tag == "" || seen[key] {
			continue
		}
		if len(tag) > maxGroupFilterTag {
			return out, invalid("filter tags are limited to %d characters", maxGroupFilterTag)
		}
		seen[key] = true
		out.Tags = append(out.Tags, tag)
	}
	if len(out.NameContains) > maxGroupFilterName {
		return out, invalid("filter.name_contains is too long (max %d characters)", maxGroupFilterName)
	}
	if len(out.SiteIDs) == 0 && len(out.ServerIDs) == 0 && len(out.Tags) == 0 && out.NameContains == "" {
		return out, invalid("filter must set at least one of site_ids, server_ids, tags or name_contains")
	}
	return out, nil
}

// marshal encodes only the keys that are set: the SQL materializer treats a missing key as "any".
func (f CameraGroupFilter) marshal() ([]byte, error) { return json.Marshal(f) }

func parseGroupFilter(raw []byte) *CameraGroupFilter {
	if len(raw) == 0 {
		return nil
	}
	var f CameraGroupFilter
	if json.Unmarshal(raw, &f) != nil {
		return nil
	}
	return &f
}
