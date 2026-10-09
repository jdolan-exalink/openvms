package media

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/jdolan-exalink/openvms/internal/events"
	"github.com/jdolan-exalink/openvms/internal/frigate"
	"github.com/jdolan-exalink/openvms/internal/inventory"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

// ExportJobManager orchestrates multi-camera export jobs in the background (worker).
// It coordinates Frigate clips extraction, bandwidth-managed staging, forensic SHA-256
// hashing, and evidence manifest generation into local orchestrator storage.
type ExportJobManager struct {
	Store      *store.Store
	Adapters   *inventory.Adapters
	StorageDir string
	Limiter    *BandwidthLimiter
	Interval   time.Duration
	Log        interface {
		InfoContext(ctx context.Context, msg string, args ...any)
		WarnContext(ctx context.Context, msg string, args ...any)
		ErrorContext(ctx context.Context, msg string, args ...any)
	}

	mu       sync.Mutex
	inFlight map[uuid.UUID]context.CancelFunc
}

func (m *ExportJobManager) Run(ctx context.Context) {
	m.StorageDir = ensureExportDir(m.StorageDir)
	if m.Interval <= 0 {
		m.Interval = 2 * time.Second
	}
	tick := time.NewTicker(m.Interval)
	defer tick.Stop()

	for {
		m.Once(ctx)
		select {
		case <-ctx.Done():
			m.cancelAllInFlight()
			return
		case <-tick.C:
		}
	}
}

func ensureExportDir(dir string) string {
	if dir == "" {
		dir = "/mnt/openvms/exports"
	}
	if err := os.MkdirAll(dir, 0755); err == nil {
		return dir
	}
	fallback := "/var/lib/openvms/exports"
	if err := os.MkdirAll(fallback, 0755); err == nil {
		return fallback
	}
	local := "./data/exports"
	_ = os.MkdirAll(local, 0755)
	return local
}

func (m *ExportJobManager) cancelAllInFlight() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, cancel := range m.inFlight {
		cancel()
		delete(m.inFlight, id)
	}
}

type activeJobRow struct {
	id          uuid.UUID
	tenantID    uuid.UUID
	name        string
	start       time.Time
	end         time.Time
	status      string
	protected   bool
	expiresAt   *time.Time
	requester   string
	createdAt   time.Time
}

type activeItemRow struct {
	id             uuid.UUID
	jobID          uuid.UUID
	tenantID       uuid.UUID
	cameraID       uuid.UUID
	cameraName     string
	remoteName     string
	serverID       uuid.UUID
	serverName     string
	remoteExportID string
	status         string
	progress       float32
	error          string
	remotePath     string
	localPath      string
	sha256Hash     string
	totalBytes     int64
	transferred    int64
	createdAt      time.Time
	serverRow      db.FrigateServer
}

func (m *ExportJobManager) Once(ctx context.Context) {
	m.mu.Lock()
	if m.inFlight == nil {
		m.inFlight = make(map[uuid.UUID]context.CancelFunc)
	}
	m.mu.Unlock()

	// 1. Check active jobs
	var jobs []activeJobRow
	err := m.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
SELECT j.id, j.tenant_id, j.name, j.start_time, j.end_time, j.status, j.protected, j.expires_at, u.username, j.created_at
FROM export_jobs j
JOIN users u ON u.id = j.requested_by
WHERE j.status IN ('queued', 'preparing', 'transferring', 'processing') AND j.deleted_at IS NULL
ORDER BY j.created_at ASC LIMIT 50`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var r activeJobRow
			if err := rows.Scan(&r.id, &r.tenantID, &r.name, &r.start, &r.end, &r.status, &r.protected, &r.expiresAt, &r.requester, &r.createdAt); err != nil {
				return err
			}
			jobs = append(jobs, r)
		}
		return rows.Err()
	})
	if err != nil {
		m.Log.WarnContext(ctx, "export manager: query active jobs", "error", err)
		return
	}

	for _, j := range jobs {
		m.processJob(ctx, j)
	}

	// 2. Retention cleanup for expired/deleted jobs
	m.cleanupRetention(ctx)
}

func (m *ExportJobManager) processJob(ctx context.Context, j activeJobRow) {
	// Query items for this job
	var items []activeItemRow
	err := m.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
SELECT i.id, i.job_id, i.tenant_id, i.camera_id, COALESCE(c.display_name, ''), COALESCE(c.remote_name, ''),
       i.server_id, COALESCE(s.name, ''), i.remote_export_id, i.status, i.progress, i.remote_path, i.local_path,
       i.sha256_hash, i.total_bytes, i.transferred_bytes, i.created_at, COALESCE(i.error, '')
FROM export_job_items i
LEFT JOIN cameras c ON c.id = i.camera_id
LEFT JOIN frigate_servers s ON s.id = i.server_id
WHERE i.job_id = $1
ORDER BY i.created_at ASC`, j.id)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var it activeItemRow
			if err := rows.Scan(
				&it.id, &it.jobID, &it.tenantID, &it.cameraID, &it.cameraName, &it.remoteName,
				&it.serverID, &it.serverName, &it.remoteExportID, &it.status, &it.progress, &it.remotePath,
				&it.localPath, &it.sha256Hash, &it.totalBytes, &it.transferred, &it.createdAt, &it.error,
			); err != nil {
				return err
			}
			items = append(items, it)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		rows.Close()

		serverCache := make(map[uuid.UUID]db.FrigateServer)
		q := db.New(tx)
		for idx := range items {
			sid := items[idx].serverID
			if sid == uuid.Nil {
				continue
			}
			srv, ok := serverCache[sid]
			if !ok {
				var serr error
				srv, serr = q.GetServerRow(ctx, sid)
				if serr != nil {
					m.Log.WarnContext(ctx, "export manager: get server row", "server_id", sid, "error", serr)
					continue
				}
				serverCache[sid] = srv
			}
			items[idx].serverRow = srv
		}
		return nil
	})
	if err != nil {
		m.Log.WarnContext(ctx, "export manager: query job items", "job_id", j.id, "error", err)
		return
	}

	allReady := len(items) > 0
	anyFailed := false
	var failedReasons []string

	for idx := range items {
		it := items[idx]
		switch it.status {
		case "queued":
			allReady = false
			m.startRemoteExport(ctx, j, &it)
		case "preparing":
			allReady = false
			m.pollRemoteExport(ctx, j, &it)
		case "transferring":
			allReady = false
			m.ensureTransferring(ctx, j, it)
		case "failed":
			anyFailed = true
			camName := it.cameraName
			if camName == "" {
				camName = it.cameraID.String()[:8]
			}
			errDesc := it.error
			if errDesc == "" {
				errDesc = "export failed"
			}
			failedReasons = append(failedReasons, fmt.Sprintf("%s: %s", camName, errDesc))
		case "ready":
			// Completed item
		default:
			allReady = false
		}
	}

	// If all items are ready, generate manifest and mark job as ready!
	if allReady && !anyFailed {
		m.finalizeJob(ctx, j, items)
		return
	}

	// If any item failed and no items are progressing, fail the job
	if anyFailed {
		hasProgressing := false
		for _, it := range items {
			if it.status == "queued" || it.status == "preparing" || it.status == "transferring" {
				hasProgressing = true
				break
			}
		}
		if !hasProgressing {
			detailedError := strings.Join(failedReasons, "; ")
			if detailedError == "" {
				detailedError = "at least one camera export failed"
			}
			_ = m.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
				_, err := tx.Exec(ctx, `UPDATE export_jobs SET status = 'failed', error = $2, updated_at = now() WHERE id = $1`, j.id, detailedError)
				return err
			})
		}
	}
}

func (m *ExportJobManager) startRemoteExport(ctx context.Context, j activeJobRow, it *activeItemRow) {
	if it.remoteName == "" {
		m.failItem(ctx, it.id, "camera remote name missing or camera deleted")
		return
	}
	if it.serverRow.ID == uuid.Nil {
		m.failItem(ctx, it.id, "frigate server not found or deleted")
		return
	}
	a, err := m.Adapters.Get(ctx, it.serverRow)
	if err != nil {
		m.Log.WarnContext(ctx, "export manager: adapter get", "server_id", it.serverID, "error", err)
		m.failItem(ctx, it.id, fmt.Sprintf("adapter error: %v", err))
		return
	}

	remoteID, err := a.StartExport(ctx, it.remoteName, float64(j.start.Unix()), float64(j.end.Unix()), fmt.Sprintf("%s-%s", j.name, it.remoteName))
	if err != nil {
		m.Log.WarnContext(ctx, "export manager: start export failed", "camera", it.remoteName, "error", err)
		m.failItem(ctx, it.id, err.Error())
		return
	}

	_ = m.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
UPDATE export_job_items SET status = 'preparing', remote_export_id = $2, updated_at = now() WHERE id = $1`,
			it.id, remoteID)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE export_jobs SET status = 'preparing', updated_at = now() WHERE id = $1 AND status = 'queued'`, j.id)
		return err
	})
	it.status = "preparing"
	it.remoteExportID = remoteID
}

func (m *ExportJobManager) pollRemoteExport(ctx context.Context, j activeJobRow, it *activeItemRow) {
	if it.remoteExportID == "" || it.serverRow.ID == uuid.Nil {
		return
	}
	a, err := m.Adapters.Get(ctx, it.serverRow)
	if err != nil {
		return
	}

	info, err := a.Export(ctx, it.remoteExportID)
	switch {
	case err == nil && info.Failed:
		errMsg := info.Error
		if errMsg == "" {
			errMsg = "Frigate export failed"
		}
		m.failItem(ctx, it.id, errMsg)
	case err == nil && !info.InProgress && info.VideoPath != "":
		// Video file is ready in Frigate, transition to transferring!
		_ = m.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `
UPDATE export_job_items SET status = 'transferring', remote_path = $2, progress = 50, updated_at = now() WHERE id = $1`,
				it.id, info.VideoPath)
			if err != nil {
				return err
			}
			_, err = tx.Exec(ctx, `UPDATE export_jobs SET status = 'transferring', updated_at = now() WHERE id = $1`, j.id)
			return err
		})
		it.status = "transferring"
		it.remotePath = info.VideoPath
		// Immediately trigger transfer
		m.ensureTransferring(ctx, j, *it)
	case err == nil:
		progress := float32(info.Progress) * 0.5
		_ = m.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `UPDATE export_job_items SET progress = $2, updated_at = now() WHERE id = $1`, it.id, progress)
			return err
		})
	case errors.Is(err, frigate.ErrNotFound) && time.Since(it.createdAt) > 2*time.Minute:
		m.failItem(ctx, it.id, "Frigate no longer has this export")
	}
}

func (m *ExportJobManager) ensureTransferring(parentCtx context.Context, j activeJobRow, it activeItemRow) {
	m.mu.Lock()
	if _, active := m.inFlight[it.id]; active {
		m.mu.Unlock()
		return
	}

	transferCtx, cancel := context.WithCancel(context.Background())
	m.inFlight[it.id] = cancel
	m.mu.Unlock()

	go func() {
		defer func() {
			m.mu.Lock()
			delete(m.inFlight, it.id)
			m.mu.Unlock()
			cancel()
		}()

		if err := m.transferItem(transferCtx, j, it); err != nil {
			if !errors.Is(err, context.Canceled) {
				m.Log.WarnContext(parentCtx, "export manager: transfer failed", "item_id", it.id, "error", err)
				m.failItem(parentCtx, it.id, err.Error())
			}
		}
	}()
}

func (m *ExportJobManager) transferItem(ctx context.Context, j activeJobRow, it activeItemRow) error {
	a, err := m.Adapters.Get(ctx, it.serverRow)
	if err != nil {
		return err
	}

	resp, err := a.Media().Open(ctx, it.remotePath, nil, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("frigate HTTP %d for %s", resp.StatusCode, it.remotePath)
	}

	totalSize := resp.ContentLength
	if totalSize < 0 {
		totalSize = 0
	}

	jobDir := filepath.Join(m.StorageDir, j.id.String())
	if err := os.MkdirAll(jobDir, 0755); err != nil {
		return err
	}

	partPath := filepath.Join(jobDir, fmt.Sprintf("%s.mp4.part", it.id.String()))
	finalPath := filepath.Join(jobDir, fmt.Sprintf("%s.mp4", it.id.String()))

	f, err := os.OpenFile(partPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	defer func() {
		_ = f.Close()
		if ctx.Err() != nil {
			_ = os.Remove(partPath)
		}
	}()

	hasher := sha256.New()
	var transferred int64
	var lastTransferred int64
	lastSample := time.Now()

	onBytes := func(n int) {
		cur := atomic.AddInt64(&transferred, int64(n))
		now := time.Now()
		elapsed := now.Sub(lastSample).Seconds()
		if elapsed >= 1.0 {
			delta := cur - lastTransferred
			speedBps := float32(float64(delta*8) / elapsed)
			lastTransferred = cur
			lastSample = now

			var etaSec int
			if totalSize > cur && speedBps > 0 {
				etaSec = int(float64((totalSize - cur)*8) / float64(speedBps))
			}

			var prog float32 = 50
			if totalSize > 0 {
				prog = 50 + float32(float64(cur)/float64(totalSize))*50
			}

			_ = m.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
				_, err := tx.Exec(ctx, `
UPDATE export_job_items SET transferred_bytes = $2, total_bytes = $3, progress = $4, updated_at = now() WHERE id = $1`,
					it.id, cur, totalSize, prog)
				if err != nil {
					return err
				}
				_, err = tx.Exec(ctx, `
UPDATE export_jobs SET transferred_bytes = (SELECT COALESCE(sum(transferred_bytes), 0) FROM export_job_items WHERE job_id = $1),
                      total_bytes = (SELECT COALESCE(sum(total_bytes), 0) FROM export_job_items WHERE job_id = $1),
                      progress = (SELECT COALESCE(avg(progress), 0) FROM export_job_items WHERE job_id = $1),
                      speed_bps = $2, eta_seconds = $3, updated_at = now()
WHERE id = $1`, j.id, speedBps, etaSec)
				return err
			})
		}
	}

	throttled := NewThrottledReader(ctx, resp.Body, m.Limiter, it.serverID.String(), onBytes)
	writer := io.MultiWriter(f, hasher)

	copied, err := io.Copy(writer, throttled)
	if err != nil {
		return err
	}

	if err := f.Sync(); err != nil {
		return err
	}
	_ = f.Close()

	if err := os.Rename(partPath, finalPath); err != nil {
		return err
	}

	hashStr := hex.EncodeToString(hasher.Sum(nil))

	return m.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
UPDATE export_job_items
SET status = 'ready', local_path = $2, sha256_hash = $3, total_bytes = $4, transferred_bytes = $4, progress = 100, updated_at = now()
WHERE id = $1`, it.id, finalPath, hashStr, copied)
		return err
	})
}

func (m *ExportJobManager) finalizeJob(ctx context.Context, j activeJobRow, items []activeItemRow) {
	jobDir := filepath.Join(m.StorageDir, j.id.String())
	manifestPath := filepath.Join(jobDir, "manifest.json")

	type cameraManifest struct {
		ItemID     string `json:"item_id"`
		CameraID   string `json:"camera_id"`
		CameraName string `json:"camera_name"`
		ServerID   string `json:"server_id"`
		ServerName string `json:"server_name"`
		File       string `json:"file"`
		SizeBytes  int64  `json:"size_bytes"`
		SHA256     string `json:"sha256"`
	}

	var camList []cameraManifest
	var totalBytes int64
	hashes := make(map[string]string)
	camIDs := make([]uuid.UUID, 0, len(items))
	for _, it := range items {
		filename := fmt.Sprintf("%s.mp4", it.id.String())
		camList = append(camList, cameraManifest{
			ItemID:     it.id.String(),
			CameraID:   it.cameraID.String(),
			CameraName: it.cameraName,
			ServerID:   it.serverID.String(),
			ServerName: it.serverName,
			File:       filename,
			SizeBytes:  it.totalBytes,
			SHA256:     it.sha256Hash,
		})
		totalBytes += it.totalBytes
		hashes[filename] = it.sha256Hash
		camIDs = append(camIDs, it.cameraID)
	}

	type eventManifestItem struct {
		ID        string   `json:"id"`
		CameraID  string   `json:"camera_id"`
		StartTime string   `json:"start_time"`
		EndTime   string   `json:"end_time"`
		Labels    []string `json:"labels"`
		Zones     []string `json:"zones"`
		Plates    []string `json:"plates"`
		Severity  string   `json:"severity"`
		// Tracks freezes the real Frigate trajectories at export time (same JSON as the API's
		// ObjectTrack) so shared links and offline copies need no live lookup.
		Tracks json.RawMessage `json:"tracks"`
	}
	manifestEvents := []eventManifestItem{}

	if len(camIDs) > 0 {
		// An event still open is only taken when it started within events.StaleOpenWindow of the
		// window: the syncer stops re-reading reviews open longer than that (Frigate restarted
		// mid-review and never closed them), so older open rows are stale and would leak into
		// every export.
		err := m.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
			rows, qerr := tx.Query(ctx, `
SELECT e.id, e.camera_id, e.start_time, COALESCE(e.end_time, e.start_time), e.labels, e.zones, e.plates, e.severity,
`+events.TracksSubquery+`
FROM events e
WHERE e.camera_id = ANY($1) AND e.start_time <= $3
  AND `+events.AliveAtOrAfter("$2")+`
ORDER BY e.start_time ASC`, camIDs, j.start, j.end)
			if qerr != nil {
				return qerr
			}
			defer rows.Close()
			for rows.Next() {
				var evID, cID uuid.UUID
				var st, et time.Time
				var labels, zones, plates []string
				var sev string
				var tracks []byte
				if err := rows.Scan(&evID, &cID, &st, &et, &labels, &zones, &plates, &sev, &tracks); err != nil {
					return err
				}
				manifestEvents = append(manifestEvents, eventManifestItem{
					ID:        evID.String(),
					CameraID:  cID.String(),
					StartTime: st.UTC().Format(time.RFC3339),
					EndTime:   et.UTC().Format(time.RFC3339),
					Labels:    labels,
					Zones:     zones,
					Plates:    plates,
					Severity:  sev,
					Tracks:    tracks,
				})
			}
			return rows.Err()
		})
		if err != nil {
			// The video is still valid evidence; the manifest just lacks the event index.
			m.Log.WarnContext(ctx, "export manager: manifest events not loaded", "job_id", j.id, "err", err)
			manifestEvents = []eventManifestItem{}
		}
	}

	manifestZones := m.manifestZones(ctx, j.id, items)

	manifestObj := map[string]any{
		"export_id":           j.id.String(),
		"name":                j.name,
		"tenant_id":           j.tenantID.String(),
		"created_at":          j.createdAt.UTC().Format(time.RFC3339),
		"requested_by":        j.requester,
		"start_time":          j.start.UTC().Format(time.RFC3339),
		"end_time":            j.end.UTC().Format(time.RFC3339),
		"protected":           j.protected,
		"cameras":             camList,
		"events":              manifestEvents,
		"zones":               manifestZones,
		"total_bytes":         totalBytes,
		"hashes":              hashes,
		"integrity_algorithm": "SHA-256",
	}

	if b, err := json.MarshalIndent(manifestObj, "", "  "); err == nil {
		_ = os.WriteFile(manifestPath, b, 0644)
	}

	bJSON, _ := json.Marshal(manifestObj)
	_ = m.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
UPDATE export_jobs
SET status = 'ready', progress = 100, local_path = $2, manifest = $3, total_bytes = $4, transferred_bytes = $4,
    speed_bps = 0, eta_seconds = 0, updated_at = now()
WHERE id = $1`, j.id, jobDir, bJSON, totalBytes)
		return err
	})
	m.Log.InfoContext(ctx, "export manager: job finalized", "job_id", j.id, "cameras", len(items), "bytes", totalBytes)
}

// zonesForManifest keeps only the polygon coordinates of each zone in a Frigate camera config
// section, exactly as Frigate returns them (comma-separated string or list), so frozen manifests
// carry no other config. Zones without coordinates are dropped.
func zonesForManifest(cfg map[string]any) map[string]any {
	raw, _ := cfg["zones"].(map[string]any)
	out := map[string]any{}
	for name, z := range raw {
		zm, ok := z.(map[string]any)
		if !ok {
			continue
		}
		if c, ok := zm["coordinates"]; ok && c != nil {
			out[name] = map[string]any{"coordinates": c}
		}
	}
	return out
}

// manifestZones freezes each exported camera's Frigate zones, keyed by our camera id. A camera
// whose zones cannot be read is omitted (with a warning); it never fails the export.
func (m *ExportJobManager) manifestZones(ctx context.Context, jobID uuid.UUID, items []activeItemRow) map[string]any {
	out := map[string]any{}
	if m.Adapters == nil {
		return out
	}
	for _, it := range items {
		if _, done := out[it.cameraID.String()]; done {
			continue
		}
		zctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		a, err := m.Adapters.Get(zctx, it.serverRow)
		if err == nil {
			var cfg map[string]any
			if cfg, err = a.CameraConfig(zctx, it.remoteName, false); err == nil {
				if z := zonesForManifest(cfg); len(z) > 0 {
					out[it.cameraID.String()] = z
				}
			}
		}
		cancel()
		if err != nil {
			m.Log.WarnContext(ctx, "export manager: camera zones not loaded", "job_id", jobID, "camera_id", it.cameraID, "err", err)
		}
	}
	return out
}

func (m *ExportJobManager) failItem(ctx context.Context, itemID uuid.UUID, errMsg string) {
	dbCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = m.Store.TxRaw(dbCtx, store.AllTenants, func(tx pgx.Tx) error {
		_, err := tx.Exec(dbCtx, `UPDATE export_job_items SET status = 'failed', error = $2, updated_at = now() WHERE id = $1`, itemID, errMsg)
		return err
	})
}

func (m *ExportJobManager) cleanupRetention(ctx context.Context) {
	var toClean []struct {
		id        uuid.UUID
		localPath string
	}
	err := m.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
SELECT id, local_path FROM export_jobs
WHERE (deleted_at IS NOT NULL OR (protected = false AND expires_at IS NOT NULL AND expires_at < now()))
  AND local_path != '' LIMIT 50`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var c struct {
				id        uuid.UUID
				localPath string
			}
			if err := rows.Scan(&c.id, &c.localPath); err != nil {
				return err
			}
			toClean = append(toClean, c)
		}
		return rows.Err()
	})
	if err != nil {
		return
	}

	for _, c := range toClean {
		if c.localPath != "" && filepath.IsAbs(c.localPath) && c.localPath != "/" {
			_ = os.RemoveAll(c.localPath)
		}
		_ = m.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `UPDATE export_jobs SET local_path = '', status = CASE WHEN status = 'ready' THEN 'expired' ELSE status END WHERE id = $1`, c.id)
			return err
		})
	}
}
