package camconfig

// MaxBulkCameras caps the cameras one bulk call may name.
const MaxBulkCameras = 200

// Statuses of one camera in a bulk review or apply.
const (
	BulkWillApply    = "will_apply"
	BulkApplied      = "applied"
	BulkFailed       = "failed"
	BulkIncompatible = "incompatible"
	BulkSkipped      = "skipped"
	BulkUnreachable  = "unreachable"
)

// BulkRow is what a bulk review or apply says about one camera.
type BulkRow struct {
	CameraID string       `json:"camera_id"`
	Name     string       `json:"name"`
	Status   string       `json:"status"`
	Changes  []Change     `json:"changes"`
	Impact   []ImpactItem `json:"impact"`
	Reason   string       `json:"reason"`
}

// BulkTotals counts rows by status.
type BulkTotals struct {
	Cameras      int `json:"cameras"`
	WillApply    int `json:"will_apply"`
	Applied      int `json:"applied"`
	Failed       int `json:"failed"`
	Incompatible int `json:"incompatible"`
	Skipped      int `json:"skipped"`
	Unreachable  int `json:"unreachable"`
}

// BulkReport is the answer of a bulk review or apply.
type BulkReport struct {
	Section string     `json:"section"`
	Rows    []BulkRow  `json:"rows"`
	Totals  BulkTotals `json:"totals"`
	// Restarts counts cameras whose change needs an engine restart.
	Restarts int `json:"restarts"`
}

// Section returns the only top-level section a bulk patch touches: detection, recording,
// analytics or streams. A bulk change applies one section, so the other cameras' unrelated
// parameters are never overwritten; zero or several sections are a ValidationError.
func Section(p Patch) (string, error) {
	n := p.Normalized()
	var found []string
	if n.Detection != nil {
		found = append(found, "detection")
	}
	if n.Recording != nil {
		found = append(found, "recording")
	}
	if n.Analytics != nil {
		found = append(found, "analytics")
	}
	if n.Streams != nil {
		found = append(found, "streams")
	}
	switch len(found) {
	case 1:
		return found[0], nil
	case 0:
		return "", invalid("patch is empty")
	}
	return "", invalid("a bulk change applies exactly one section (detection, recording, analytics or streams); got %d", len(found))
}

// Summarize fills the totals and the restart count of a report from its rows.
func Summarize(section string, rows []BulkRow) BulkReport {
	r := BulkReport{Section: section, Rows: rows}
	r.Totals.Cameras = len(rows)
	for _, row := range rows {
		switch row.Status {
		case BulkWillApply:
			r.Totals.WillApply++
		case BulkApplied:
			r.Totals.Applied++
		case BulkFailed:
			r.Totals.Failed++
		case BulkIncompatible:
			r.Totals.Incompatible++
		case BulkSkipped:
			r.Totals.Skipped++
		case BulkUnreachable:
			r.Totals.Unreachable++
		}
		for _, im := range row.Impact {
			if im.Code == ImpactEngineRestart && (row.Status == BulkWillApply || row.Status == BulkApplied) {
				r.Restarts++
				break
			}
		}
	}
	return r
}
