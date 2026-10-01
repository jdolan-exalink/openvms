package maps

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/access"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/platform/httpx"
	"github.com/jdolan-exalink/openvms/internal/platform/logging"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

// siteCamera is the camera identity a CSV row can reference: UUID, display name, or
// remote (Frigate) name.
type siteCamera struct {
	ID          uuid.UUID
	DisplayName string
	RemoteName  string
}

// placementCSVRow is one validated data row of a placement import.
type placementCSVRow struct {
	Line       int
	CameraID   uuid.UUID
	Lat        float64
	Lng        float64
	BearingDeg *float32
	FovDeg     *float32
	RangeM     *float32
}

// ImportRowError points at the physical CSV line that failed validation.
type ImportRowError struct {
	Line    int    `json:"line"`
	Message string `json:"message"`
}

// ImportReport is the outcome of a placement CSV import: the rows seen, how many
// placements were written, and the rejected lines (at most one error per row).
type ImportReport struct {
	DryRun   bool
	Rows     int
	Upserted int
	Errors   []ImportRowError
}

// placementRequiredColumns are validated before any row is read, so a header that
// cannot work fails as a single line-1 error instead of one error per row.
var placementRequiredColumns = []string{"camera", "lat", "lng"}

// parsePlacementCSV validates a camera,lat,lng CSV (optional bearing,fov,range columns).
// A row resolves its camera by UUID, display name, or remote name (case-insensitive,
// trimmed); a camera may appear only once among the valid rows. rows holds the valid
// rows in file order, errs reports every rejected line, and total counts all data rows
// (header excluded), valid or not.
func parsePlacementCSV(csvText string, cameras []siteCamera) ([]placementCSVRow, []ImportRowError, int) {
	// Excel exports often start with a UTF-8 BOM, which would break the header match.
	csvText = strings.TrimPrefix(csvText, "\ufeff")

	reader := csv.NewReader(strings.NewReader(csvText))
	// Rows may be short: a missing optional trailing cell reads as empty, not as an error.
	reader.FieldsPerRecord = -1

	header, err := reader.Read()
	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil, []ImportRowError{{Line: 1, Message: "csv vacío"}}, 0
		}
		return nil, []ImportRowError{csvReadError(err)}, 0
	}
	headerLine := 1
	if len(header) > 0 {
		headerLine, _ = reader.FieldPos(0)
	}

	cols := make(map[string]int, len(header))
	for i, name := range header {
		key := strings.ToLower(strings.TrimSpace(name))
		if key != "" {
			if _, dup := cols[key]; !dup {
				cols[key] = i
			}
		}
	}
	var missing []string
	for _, want := range placementRequiredColumns {
		if _, ok := cols[want]; !ok {
			missing = append(missing, want)
		}
	}
	if len(missing) > 0 {
		return nil, []ImportRowError{{
			Line:    headerLine,
			Message: "faltan columnas requeridas: " + strings.Join(missing, ", "),
		}}, 0
	}

	var (
		rows  []placementCSVRow
		errs  []ImportRowError
		total int
		seen  = make(map[uuid.UUID]bool)
	)
	for {
		rec, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			errs = append(errs, csvReadError(err))
			break
		}
		if len(rec) == 0 {
			continue
		}
		line, _ := reader.FieldPos(0)
		total++

		row, message := placementRow(line, rec, cols, cameras, seen)
		if message != "" {
			errs = append(errs, ImportRowError{Line: line, Message: message})
			continue
		}
		seen[row.CameraID] = true
		rows = append(rows, row)
	}
	return rows, errs, total
}

// placementRow validates one record and returns either the row or the single reason
// it was rejected, in check order: camera first (it is the row's identity), then the
// coordinates, then the optional columns.
func placementRow(line int, rec []string, cols map[string]int, cameras []siteCamera, seen map[uuid.UUID]bool) (placementCSVRow, string) {
	cell := func(col string) string {
		i, ok := cols[col]
		if !ok || i >= len(rec) {
			return ""
		}
		return strings.TrimSpace(rec[i])
	}

	token := cell("camera")
	if token == "" {
		return placementCSVRow{}, "cámara vacía"
	}
	cameraID, message := resolveCamera(token, cameras)
	if message != "" {
		return placementCSVRow{}, message
	}
	if seen[cameraID] {
		return placementCSVRow{}, fmt.Sprintf("cámara repetida: %q", token)
	}

	lat, message := parseCoordinate("lat", cell("lat"), -90, 90)
	if message != "" {
		return placementCSVRow{}, message
	}
	lng, message := parseCoordinate("lng", cell("lng"), -180, 180)
	if message != "" {
		return placementCSVRow{}, message
	}
	bearing, message := parseOptional("bearing", cell("bearing"), 0, 360)
	if message != "" {
		return placementCSVRow{}, message
	}
	fov, message := parseOptional("fov", cell("fov"), 1, 360)
	if message != "" {
		return placementCSVRow{}, message
	}
	rangeM, message := parseOptional("range", cell("range"), 0, math.MaxFloat32)
	if message != "" {
		return placementCSVRow{}, message
	}

	return placementCSVRow{
		Line:       line,
		CameraID:   cameraID,
		Lat:        lat,
		Lng:        lng,
		BearingDeg: bearing,
		FovDeg:     fov,
		RangeM:     rangeM,
	}, ""
}

// resolveCamera matches token against every camera's UUID, display name, and remote
// name (case-insensitive). Zero matches is unknown, more than one distinct camera is
// ambiguous; the error quotes the token exactly as the operator typed it.
func resolveCamera(token string, cameras []siteCamera) (uuid.UUID, string) {
	lower := strings.ToLower(token)
	var matches []uuid.UUID
	for _, c := range cameras {
		if lower != strings.ToLower(c.ID.String()) &&
			lower != strings.ToLower(strings.TrimSpace(c.DisplayName)) &&
			lower != strings.ToLower(strings.TrimSpace(c.RemoteName)) {
			continue
		}
		dup := false
		for _, id := range matches {
			if id == c.ID {
				dup = true
				break
			}
		}
		if !dup {
			matches = append(matches, c.ID)
		}
	}
	switch len(matches) {
	case 0:
		return uuid.Nil, fmt.Sprintf("cámara desconocida: %q", token)
	case 1:
		return matches[0], ""
	default:
		return uuid.Nil, fmt.Sprintf("cámara ambigua: %q", token)
	}
}

// parseCoordinate parses a required numeric column and enforces the same range the
// placement endpoint validates, so an import can never write a row the API would refuse.
func parseCoordinate(col, token string, min, max float64) (float64, string) {
	value, err := strconv.ParseFloat(token, 64)
	if err != nil || math.IsNaN(value) {
		return 0, fmt.Sprintf("%s inválida: %q", col, token)
	}
	if value < min || value > max {
		return 0, fmt.Sprintf("%s fuera de rango: %s", col, token)
	}
	return value, ""
}

// parseOptional parses an optional numeric column: empty stays nil, otherwise it must
// parse and fall inside [min, max].
func parseOptional(col, token string, min, max float64) (*float32, string) {
	if token == "" {
		return nil, ""
	}
	value, message := parseCoordinate(col, token, min, max)
	if message != "" {
		return nil, message
	}
	parsed := float32(value)
	return &parsed, ""
}

// csvReadError reports a malformed CSV line with its physical line number.
func csvReadError(err error) ImportRowError {
	var pe *csv.ParseError
	if errors.As(err, &pe) {
		return ImportRowError{Line: pe.Line, Message: fmt.Sprintf("CSV inválido: %s", pe.Err)}
	}
	return ImportRowError{Line: 1, Message: fmt.Sprintf("CSV inválido: %s", err)}
}

// ImportPlacements validates a camera placement CSV for a site and, unless it is a dry
// run, applies it atomically: any invalid row leaves the site untouched, and the report
// shows those errors in both modes. Requires maps.edit_device, dry runs included.
func (s *Service) ImportPlacements(ctx context.Context, actor authz.Actor, siteID uuid.UUID, csvText string, dryRun bool) (*ImportReport, error) {
	var report ImportReport
	err := s.Store.Tx(ctx, store.ScopeFor(actor), func(q *db.Queries) error {
		chk, err := access.Load(ctx, q, actor)
		if err != nil {
			return err
		}
		site, err := q.GetSiteGeo(ctx, db.GetSiteGeoParams{ID: siteID, TenantID: actor.TenantID})
		if err != nil {
			return store.Classify(err)
		}
		if err := chk.Require(authz.MapsEditDevice, access.Site(site.TenantID, siteID)); err != nil {
			return err
		}

		camRows, err := q.ListSiteCamerasForImport(ctx, db.ListSiteCamerasForImportParams{
			SiteID:   siteID,
			TenantID: actor.TenantID,
		})
		if err != nil {
			return store.Classify(err)
		}
		cameras := make([]siteCamera, 0, len(camRows))
		for _, c := range camRows {
			cameras = append(cameras, siteCamera{ID: c.ID, DisplayName: c.DisplayName, RemoteName: c.RemoteName})
		}

		rows, errs, total := parsePlacementCSV(csvText, cameras)
		report = ImportReport{DryRun: dryRun, Rows: total, Errors: errs}

		var uid *uuid.UUID
		if actor.UserID != uuid.Nil {
			uid = &actor.UserID
		}

		if !dryRun && len(errs) == 0 {
			for _, row := range rows {
				props := json.RawMessage("{}")
				existing, existErr := q.GetMapPlacementByEntityGeo(ctx, db.GetMapPlacementByEntityGeoParams{
					EntityType: "camera",
					EntityID:   row.CameraID,
					TenantID:   actor.TenantID,
				})
				switch {
				case existErr == nil:
					// Keep camera props (type, ptz, lpr) the placement already carries.
					if len(existing.Props) > 0 {
						props = existing.Props
					}
				case errors.Is(store.Classify(existErr), store.ErrNotFound):
				default:
					return store.Classify(existErr)
				}

				if _, err := q.UpsertGeoPlacement(ctx, db.UpsertGeoPlacementParams{
					TenantID:   site.TenantID,
					SiteID:     siteID,
					EntityType: "camera",
					EntityID:   row.CameraID,
					Lat:        &row.Lat,
					Lng:        &row.Lng,
					BearingDeg: row.BearingDeg,
					FovDeg:     row.FovDeg,
					RangeM:     row.RangeM,
					Props:      props,
					UserID:     uid,
				}); err != nil {
					return store.Classify(err)
				}
				report.Upserted++
			}
		}

		if !dryRun {
			// Applying (even when refused by row errors) is the auditable act; dry runs are not.
			auditPayload, _ := json.Marshal(map[string]interface{}{
				"site_id":  siteID,
				"rows":     total,
				"upserted": report.Upserted,
				"errors":   len(errs),
			})
			_ = q.InsertAudit(ctx, db.InsertAuditParams{
				TenantID:   &site.TenantID,
				ActorID:    uid,
				ActorName:  actor.Username,
				Action:     "maps.placement.import",
				TargetType: "site",
				TargetID:   &siteID,
				RequestID:  logging.RequestID(ctx),
				Ip:         httpx.ClientIP(ctx),
				Details:    auditPayload,
			})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &report, nil
}
