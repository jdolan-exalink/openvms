package frigate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

func (a *v017) Media() Media { return a.c }

type objectData struct {
	TopScore   *float64  `json:"top_score"`
	Plate      *string   `json:"recognized_license_plate"`
	PlateScore *float64  `json:"recognized_license_plate_score"`
	Box        []float64 `json:"box"`
	// PathData is kept raw so one malformed entry can be skipped instead of failing the page.
	PathData []json.RawMessage `json:"path_data"`
}

type objectResponse struct {
	ID          string          `json:"id"`
	Camera      string          `json:"camera"`
	Label       string          `json:"label"`
	SubLabel    json.RawMessage `json:"sub_label"`
	Zones       []string        `json:"zones"`
	StartTime   float64         `json:"start_time"`
	EndTime     *float64        `json:"end_time"`
	TopScore    *float64        `json:"top_score"`
	HasSnapshot bool            `json:"has_snapshot"`
	Data        objectData      `json:"data"`
}

// subLabel reads Frigate's sub_label, which is null, a string or [name, score].
func subLabel(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var pair []any
	if json.Unmarshal(raw, &pair) == nil && len(pair) > 0 {
		if v, ok := pair[0].(string); ok {
			return v
		}
	}
	return ""
}

// decodePath reads Frigate's path_data, a list of [[x, y], unix_ts]. Malformed entries are
// skipped: a partial trajectory is still real data, a failed page would lose the whole object.
func decodePath(raw []json.RawMessage) []TrackPoint {
	var out []TrackPoint
	for _, r := range raw {
		var e struct {
			Pos []float64
			T   float64
		}
		var tuple []json.RawMessage
		if json.Unmarshal(r, &tuple) != nil || len(tuple) != 2 {
			continue
		}
		if json.Unmarshal(tuple[0], &e.Pos) != nil || len(e.Pos) != 2 || json.Unmarshal(tuple[1], &e.T) != nil {
			continue
		}
		out = append(out, TrackPoint{X: e.Pos[0], Y: e.Pos[1], T: e.T})
	}
	return out
}

// TrackedObjects returns objects ordered by start time, oldest first, so callers can
// page forward with After.
func (a *v017) TrackedObjects(ctx context.Context, q ObjectQuery) ([]TrackedObject, error) {
	params := url.Values{"sort": {"date_asc"}, "include_thumbnails": {"0"}}
	if len(q.Cameras) > 0 {
		params.Set("cameras", strings.Join(q.Cameras, ","))
	}
	if q.After > 0 {
		params.Set("after", strconv.FormatFloat(q.After, 'f', 6, 64))
	}
	if q.Before > 0 {
		params.Set("before", strconv.FormatFloat(q.Before, 'f', 6, 64))
	}
	if q.Limit > 0 {
		params.Set("limit", strconv.Itoa(q.Limit))
	}
	var raw []objectResponse
	if err := a.c.getJSON(ctx, "/api/events", params, &raw); err != nil {
		return nil, err
	}
	out := make([]TrackedObject, 0, len(raw))
	for _, o := range raw {
		t := TrackedObject{
			ID: o.ID, Camera: o.Camera, Label: o.Label, SubLabel: subLabel(o.SubLabel), Zones: o.Zones,
			StartTime: o.StartTime, EndTime: o.EndTime, TopScore: o.Data.TopScore, PlateScore: o.Data.PlateScore,
			HasSnapshot: o.HasSnapshot, Path: decodePath(o.Data.PathData),
		}
		if len(o.Data.Box) == 4 {
			t.Box = o.Data.Box
		}
		if t.TopScore == nil {
			t.TopScore = o.TopScore
		}
		if o.Data.Plate != nil {
			t.Plate = strings.TrimSpace(*o.Data.Plate)
		}
		if t.Zones == nil {
			t.Zones = []string{}
		}
		out = append(out, t)
	}
	return out, nil
}

// ReviewThumbnail downloads the review thumbnail (webp in Frigate, jpeg in the mock).
func (a *v017) ReviewThumbnail(ctx context.Context, r Review) ([]byte, string, error) {
	p, err := MediaURLPath(r.ThumbPath)
	if err != nil {
		return nil, "", err
	}
	resp, err := a.c.Open(ctx, p, nil, nil)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, "", ErrNotFound
	}
	if resp.StatusCode >= 300 {
		return nil, "", fmt.Errorf("frigate thumbnail: status %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, "", err
	}
	ct := resp.Header.Get("Content-Type")
	if ct == "" || ct == "application/octet-stream" {
		ct = http.DetectContentType(b)
	}
	return b, ct, nil
}

func (a *v017) Recordings(ctx context.Context, camera string, after, before float64) ([]RecordingSegment, error) {
	params := url.Values{
		"after":  {strconv.FormatFloat(after, 'f', 3, 64)},
		"before": {strconv.FormatFloat(before, 'f', 3, 64)},
	}
	var raw []RecordingSegment
	if err := a.c.getJSON(ctx, "/api/"+url.PathEscape(camera)+"/recordings", params, &raw); err != nil {
		return nil, err
	}
	return mergeSegments(raw), nil
}

// mergeSegments joins adjacent recording chunks (Frigate stores ~10 s files) into
// continuous spans, so a timeline can draw a few bars instead of thousands.
func mergeSegments(in []RecordingSegment) []RecordingSegment {
	if len(in) == 0 {
		return []RecordingSegment{}
	}
	sortSegments(in)
	out := []RecordingSegment{in[0]}
	for _, s := range in[1:] {
		last := &out[len(out)-1]
		if s.StartTime-last.EndTime <= 2 {
			if s.EndTime > last.EndTime {
				last.EndTime = s.EndTime
			}
			last.Motion += s.Motion
			last.Objects += s.Objects
			continue
		}
		out = append(out, s)
	}
	return out
}

func sortSegments(s []RecordingSegment) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j].StartTime < s[j-1].StartTime; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

type startExportResponse struct {
	Success  bool    `json:"success"`
	Message  string  `json:"message"`
	ExportID *string `json:"export_id"`
}

func (a *v017) StartExport(ctx context.Context, camera string, start, end float64, name string) (string, error) {
	path := fmt.Sprintf("/api/export/%s/start/%s/end/%s", url.PathEscape(camera),
		strconv.FormatFloat(start, 'f', 3, 64), strconv.FormatFloat(end, 'f', 3, 64))
	body, err := a.c.send(ctx, http.MethodPost, path, nil, map[string]any{"source": "recordings", "name": name})
	if err != nil {
		return "", err
	}
	var r startExportResponse
	if err := json.Unmarshal(body, &r); err != nil {
		return "", fmt.Errorf("decode export response: %w", err)
	}
	if !r.Success || r.ExportID == nil || *r.ExportID == "" {
		return "", fmt.Errorf("frigate refused the export: %s", r.Message)
	}
	return *r.ExportID, nil
}

type exportResponse struct {
	ID         string `json:"id"`
	Camera     string `json:"camera"`
	Name       string `json:"name"`
	VideoPath  string `json:"video_path"`
	InProgress bool   `json:"in_progress"`
}

func (a *v017) Export(ctx context.Context, id string) (ExportInfo, error) {
	var r exportResponse
	if err := a.c.getJSON(ctx, "/api/exports/"+url.PathEscape(id), nil, &r); err != nil {
		return ExportInfo{}, err
	}
	return exportInfo(r)
}

func exportInfo(r exportResponse) (ExportInfo, error) {
	info := ExportInfo{ID: r.ID, Camera: r.Camera, Name: r.Name, InProgress: r.InProgress}
	if r.VideoPath != "" {
		p, err := MediaURLPath(r.VideoPath)
		if err != nil {
			return info, err
		}
		info.VideoPath = p
	}
	if !info.InProgress {
		info.Progress = 100
	}
	return info, nil
}

type exportJob struct {
	Status          string   `json:"status"`
	Camera          string   `json:"camera"`
	Name            *string  `json:"name"`
	ErrorMessage    *string  `json:"error_message"`
	ProgressPercent *float64 `json:"progress_percent"`
}

// Export in 0.18 first looks at the finished export and, while the job is still queued or
// encoding (the export row does not exist yet), at the job queue.
func (a *v018) Export(ctx context.Context, id string) (ExportInfo, error) {
	info, err := a.v017.Export(ctx, id)
	if err == nil {
		return info, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return ExportInfo{}, err
	}
	var job exportJob
	if jerr := a.c.getJSON(ctx, "/api/jobs/export/"+url.PathEscape(id), nil, &job); jerr != nil {
		return ExportInfo{}, err
	}
	out := ExportInfo{ID: id, Camera: job.Camera, InProgress: true}
	if job.Name != nil {
		out.Name = *job.Name
	}
	if job.ProgressPercent != nil {
		out.Progress = *job.ProgressPercent
	}
	switch strings.ToLower(job.Status) {
	case "failed", "error", "cancelled", "canceled":
		out.InProgress, out.Failed = false, true
		if job.ErrorMessage != nil {
			out.Error = *job.ErrorMessage
		}
	}
	return out, nil
}
