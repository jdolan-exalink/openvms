package events

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/jdolan-exalink/openvms/internal/provision"
	"github.com/jdolan-exalink/openvms/internal/store"
)

// AgentEventSource is an edge agent's durable queue of camera-reported plate readings, as central
// reads it: a page of events after a sequence, and an acknowledgement of what was stored.
// provision.Service implements it.
type AgentEventSource interface {
	PullProvisionLPREvents(ctx context.Context, serverID uuid.UUID, after uint64) (provision.LPREventsPage, error)
	AckProvisionLPREvents(ctx context.Context, serverID uuid.UUID, through uint64) error
}

const (
	// agentReadPrefix marks the lpr_reads of this source inside (server_id, remote_event_id), so they
	// can never collide with a Frigate event ID.
	agentReadPrefix = "agent:"
	// agentReadLabel is the label stored with a camera-reported plate.
	agentReadLabel = "license_plate"
	// A camera-reported reading has no crop in Frigate: the copier skips a read that has used up its
	// attempts, so it is stored as already tried.
	agentReadCropAttempts = maxThumbAttempts

	agentIngestMaxPages    = 20
	agentIngestServerLimit = time.Minute
)

// AgentIngestor pulls the plate readings Provision-ISR cameras report to their edge agent and stores
// them in lpr_reads, so they are searchable next to the readings Frigate produced.
//
// It is safe to run in several replicas and to repeat: a reading is keyed by the ID the agent gave it,
// so storing it again changes nothing, and the cursor only moves in the same transaction that stores
// the page. The agent keeps what was not acknowledged.
type AgentIngestor struct {
	Store       *store.Store
	Blobs       Blobs
	Source      AgentEventSource
	Log         *slog.Logger
	Interval    time.Duration
	Concurrency int
}

// Run ingests every agent each Interval until ctx ends.
func (a *AgentIngestor) Run(ctx context.Context) {
	t := time.NewTicker(a.Interval)
	defer t.Stop()
	for {
		a.IngestAll(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

type agentTarget struct {
	serverID, tenantID uuid.UUID
}

// IngestAll pulls every server that has an edge agent once.
func (a *AgentIngestor) IngestAll(ctx context.Context) {
	var targets []agentTarget
	err := a.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
SELECT sa.server_id, sa.tenant_id
FROM server_agents sa JOIN frigate_servers fs ON fs.id = sa.server_id
WHERE fs.deleted_at IS NULL`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var t agentTarget
			if err := rows.Scan(&t.serverID, &t.tenantID); err != nil {
				return err
			}
			targets = append(targets, t)
		}
		return rows.Err()
	})
	if err != nil {
		a.Log.ErrorContext(ctx, "agent ingest: list agents", "error", err)
		return
	}
	sem := make(chan struct{}, max(a.Concurrency, 4))
	var wg sync.WaitGroup
	for _, target := range targets {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer func() { <-sem; wg.Done() }()
			cctx, cancel := context.WithTimeout(ctx, agentIngestServerLimit)
			defer cancel()
			if err := a.IngestServer(cctx, target.serverID, target.tenantID); err != nil && ctx.Err() == nil {
				a.Log.WarnContext(ctx, "agent ingest: server", "server_id", target.serverID, "error", err)
			}
		}()
	}
	wg.Wait()
}

// IngestServer drains one agent's queue, a page at a time, and records the outcome.
func (a *AgentIngestor) IngestServer(ctx context.Context, serverID, tenantID uuid.UUID) error {
	cursor, epoch, err := a.loadCursor(ctx, serverID)
	if err != nil {
		return err
	}
	reset := false
	for range agentIngestMaxPages {
		page, err := a.Source.PullProvisionLPREvents(ctx, serverID, cursor)
		if err != nil {
			a.recordError(ctx, serverID, tenantID, err)
			return err
		}
		// The agent starts again from 1 after its state is wiped; events below the old cursor would
		// never be read. A new epoch says so even when its sequences have already caught up with the
		// cursor; a sequence that went backwards says so for an agent that sends no epoch.
		wiped := page.LastSeq < cursor || (page.Epoch != "" && epoch != "" && page.Epoch != epoch)
		if wiped && !reset {
			a.Log.WarnContext(ctx, "agent ingest: the agent's queue was recreated; reading from the start",
				"server_id", serverID, "cursor", cursor, "agent_last_seq", page.LastSeq, "epoch", epoch, "agent_epoch", page.Epoch)
			cursor, epoch, reset = 0, page.Epoch, true
			if err := a.saveCursorOnly(ctx, serverID, tenantID, 0, epoch); err != nil {
				return err
			}
			continue
		}
		epoch = page.Epoch
		if len(page.Events) == 0 {
			if err := a.touch(ctx, serverID, tenantID, page); err != nil {
				return err
			}
			if page.Depth > 0 && cursor > 0 {
				// A previous acknowledgement failed: everything up to the cursor is stored already.
				if err := a.Source.AckProvisionLPREvents(ctx, serverID, cursor); err != nil {
					a.recordError(ctx, serverID, tenantID, err)
					return err
				}
			}
			return nil
		}
		through, err := a.storePage(ctx, serverID, tenantID, cursor, page)
		if err != nil {
			a.recordError(ctx, serverID, tenantID, err)
			return err
		}
		cursor = through
		if err := a.Source.AckProvisionLPREvents(ctx, serverID, cursor); err != nil {
			// Stored and the cursor moved: the agent only keeps the events a little longer, and the
			// next run acknowledges them.
			a.recordError(ctx, serverID, tenantID, err)
			return err
		}
	}
	return nil
}

func (a *AgentIngestor) loadCursor(ctx context.Context, serverID uuid.UUID) (uint64, string, error) {
	var seq int64
	var epoch string
	err := a.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `SELECT last_seq, agent_epoch FROM server_agent_event_cursors WHERE server_id = $1`, serverID).Scan(&seq, &epoch)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return err
	})
	if err != nil {
		return 0, "", fmt.Errorf("load agent cursor: %w", err)
	}
	return uint64(max(seq, 0)), epoch, nil //nolint:gosec // clamped non-negative above
}

func decodeBase64Image(raw string) ([]byte, error) {
	raw = strings.TrimSpace(raw)
	if i := strings.Index(raw, ","); i != -1 && strings.HasPrefix(raw, "data:") {
		raw = raw[i+1:]
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, errors.New("empty image data")
	}
	data, err := base64.StdEncoding.DecodeString(raw)
	if err == nil {
		return data, nil
	}
	if data2, err2 := base64.RawStdEncoding.DecodeString(raw); err2 == nil {
		return data2, nil
	}
	return nil, err
}

// storePage stores the valid readings and smart analytic events of a page and moves the cursor, in one
// transaction. It returns the highest sequence handled, which is also what the agent is told to drop.
// Readings central cannot attribute are counted and left behind: holding the cursor for them would block
// every later reading.
func (a *AgentIngestor) storePage(ctx context.Context, serverID, tenantID uuid.UUID, cursor uint64, page provision.LPREventsPage) (uint64, error) {
	through := cursor
	var ingested, skipped int64
	err := a.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		ingested, skipped = 0, 0
		through = cursor
		resolver, err := serverCameras(ctx, tx, serverID, tenantID)
		if err != nil {
			return err
		}
		for _, e := range page.Events {
			if e.Seq <= cursor {
				continue
			}
			through = max(through, e.Seq)
			if e.ID == "" {
				skipped++
				continue
			}
			ref, known := resolver.resolve(e.Event.CameraID, e.Event.CameraIP, e.Event.ChannelID)
			if !known {
				skipped++
				continue
			}
			plate := NormalizePlate(e.Event.Plate)
			eventType := strings.TrimSpace(e.Event.EventType)
			if eventType == "" {
				if plate != "" {
					eventType = "lpr"
				} else {
					skipped++
					continue
				}
			}
			if eventType == "keepalive" || eventType == "heartbeat" {
				skipped++
				continue
			}
			if eventType == "lpr" && plate == "" {
				skipped++
				continue
			}

			cam := ref.id
			siteID := ref.siteID
			seen := e.Event.EventTime
			if seen.IsZero() {
				seen = e.Event.ReceivedTime
			}
			if seen.IsZero() {
				seen = time.Now()
			}

			var eventIngested bool

			// Store images to Blobs if available
			var cropKey, snapKey string
			if a.Blobs != nil {
				if e.Event.PlateCrop != nil && *e.Event.PlateCrop != "" {
					if b, err := decodeBase64Image(*e.Event.PlateCrop); err == nil && len(b) > 0 {
						cropKey = fmt.Sprintf("tenant/%s/plates/%s/%s.jpg", tenantID, serverID, e.ID)
						if err := a.Blobs.Put(ctx, cropKey, b, "image/jpeg"); err != nil {
							a.Log.WarnContext(ctx, "agent ingest: failed to save plate crop", "error", err, "event_id", e.ID)
							cropKey = ""
						}
					}
				}
				if e.Event.PanoramicImage != nil && *e.Event.PanoramicImage != "" {
					if b, err := decodeBase64Image(*e.Event.PanoramicImage); err == nil && len(b) > 0 {
						snapKey = fmt.Sprintf("tenant/%s/events/%s/%s.jpg", tenantID, serverID, e.ID)
						if err := a.Blobs.Put(ctx, snapKey, b, "image/jpeg"); err != nil {
							a.Log.WarnContext(ctx, "agent ingest: failed to save panoramic snapshot", "error", err, "event_id", e.ID)
							snapKey = ""
						}
					}
				}
			}

			// 1. If a plate is present, store in lpr_reads.
			if plate != "" {
				var score *float32
				if e.Event.Confidence != nil {
					v := float32(*e.Event.Confidence)
					score = &v
				}

				remoteEventID := agentReadPrefix + e.ID
				var existingID uuid.UUID
				err := tx.QueryRow(ctx, `
SELECT id FROM lpr_reads
WHERE (server_id = $1 AND remote_event_id = $2)
   OR (camera_id = $3 AND plate_normalized = $4
       AND seen_at >= ($5::timestamptz - interval '20 seconds')
       AND seen_at <= ($5::timestamptz + interval '20 seconds'))
ORDER BY (server_id = $1 AND remote_event_id = $2) DESC, abs(extract(epoch from (seen_at - $5::timestamptz))) ASC
LIMIT 1 FOR UPDATE`, serverID, remoteEventID, cam, plate, seen.UTC()).Scan(&existingID)

				if err == nil {
					// Existing read found: update crop, snapshot, score, crop_attempts
					tag, err := tx.Exec(ctx, `
UPDATE lpr_reads SET
    crop_key = CASE WHEN $2 <> '' AND (crop_key = '' OR crop_key IS NULL) THEN $2 ELSE crop_key END,
    snapshot_key = CASE WHEN $3 <> '' AND (snapshot_key = '' OR snapshot_key IS NULL) THEN $3 ELSE snapshot_key END,
    crop_attempts = CASE WHEN $2 <> '' THEN GREATEST(crop_attempts, $4) ELSE crop_attempts END,
    score = CASE WHEN $5::real IS NOT NULL AND ($5::real > score OR score IS NULL) THEN $5::real ELSE score END,
    updated_at = now()
WHERE id = $1`, existingID, cropKey, snapKey, agentReadCropAttempts, score)
					if err != nil {
						return fmt.Errorf("update dedup agent reading %s: %w", e.ID, err)
					}
					if tag.RowsAffected() > 0 {
						eventIngested = true
					}
				} else if errors.Is(err, pgx.ErrNoRows) {
					tag, err := tx.Exec(ctx, `
INSERT INTO lpr_reads (tenant_id, site_id, server_id, camera_id, remote_event_id, plate, plate_normalized, score, label, sub_label, zones, seen_at, crop_attempts, crop_key, snapshot_key)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, '', '{}', $10, $11, $12, $13)
ON CONFLICT (server_id, remote_event_id) DO UPDATE SET
    crop_key = CASE WHEN lpr_reads.crop_key = '' THEN EXCLUDED.crop_key ELSE lpr_reads.crop_key END,
    snapshot_key = CASE WHEN lpr_reads.snapshot_key = '' THEN EXCLUDED.snapshot_key ELSE lpr_reads.snapshot_key END,
    updated_at = now()`,
						tenantID, siteID, serverID, cam, remoteEventID, e.Event.Plate, plate, score, agentReadLabel, seen.UTC(), agentReadCropAttempts, cropKey, snapKey)
					if err != nil {
						return fmt.Errorf("store agent reading %s: %w", e.ID, err)
					}
					if tag.RowsAffected() > 0 {
						eventIngested = true
					}
				} else {
					return fmt.Errorf("find dedup agent reading %s: %w", e.ID, err)
				}
			}

			// 2. Store in events table for all events (both smart analytics and LPR)
			// so they are searchable in reviews and link vehicle attributes/snapshots.
			sev := "alert"
			if eventType == "motion" {
				sev = "detection"
			}
			var labels []string
			switch eventType {
			case "line_crossing":
				labels = []string{"line_crossing"}
			case "intrusion":
				labels = []string{"intrusion"}
			case "vehicle":
				labels = []string{"car", "vehicle"}
			case "human":
				labels = []string{"person", "human"}
			case "face":
				labels = []string{"face"}
			case "motion":
				labels = []string{"motion"}
			case "lpr":
				labels = []string{"car", "vehicle"}
			default:
				labels = []string{eventType}
			}
			plates := []string{}
			if plate != "" {
				plates = []string{plate}
			}

			thumbKey := snapKey
			if thumbKey == "" {
				thumbKey = cropKey
			}

			var evID uuid.UUID
			var isNew bool
			err = tx.QueryRow(ctx, `
INSERT INTO events (tenant_id, site_id, server_id, camera_id, remote_id, severity, labels, sub_labels, zones, audio,
                    detection_ids, start_time, reviewed, plates, has_snapshot, lpr, thumb_key)
VALUES ($1, $2, $3, $4, $5, $6, coalesce($7::text[], '{}'), '{}', '{}', '{}',
        $8, $9, false, coalesce($10::text[], '{}'), $11, $12, $13)
ON CONFLICT (server_id, remote_id) DO UPDATE SET
    thumb_key = CASE WHEN events.thumb_key = '' THEN EXCLUDED.thumb_key ELSE events.thumb_key END,
    has_snapshot = events.has_snapshot OR EXCLUDED.has_snapshot,
    updated_at = now()
RETURNING id, (xmax = 0)`,
				tenantID, siteID, serverID, cam, agentReadPrefix+e.ID, sev, labels,
				[]string{agentReadPrefix + e.ID}, seen.UTC(), plates, snapKey != "", plate != "" || eventType == "lpr", thumbKey).Scan(&evID, &isNew)
			if err != nil {
				return fmt.Errorf("store agent event %s: %w", e.ID, err)
			}
			if isNew {
				eventIngested = true
				ev := NewEvent{
					ID:       evID,
					TenantID: tenantID,
					SiteID:   siteID,
					ServerID: serverID,
					CameraID: cam,
					Severity: sev,
					Labels:   labels,
					Start:    seen.UTC(),
					Plates:   plates,
				}
				if err := enqueueRuleEvaluation(ctx, tx, ev, ref.name, false); err != nil {
					return fmt.Errorf("enqueue rule evaluation for agent event %s: %w", e.ID, err)
				}
			}

			// 3. Store vehicle attributes if present
			vType := "unknown"
			if e.Event.VehicleType != nil && *e.Event.VehicleType != "" {
				vType = *e.Event.VehicleType
			} else if plate != "" {
				vType = "car"
			}
			vColor := "unknown"
			if e.Event.VehicleColor != nil && *e.Event.VehicleColor != "" {
				vColor = *e.Event.VehicleColor
			}
			brand := ""
			if e.Event.VehicleBrand != nil {
				brand = *e.Event.VehicleBrand
			}
			model := ""
			if e.Event.VehicleModel != nil {
				model = *e.Event.VehicleModel
			}
			if vColor != "unknown" || brand != "" || model != "" || vType != "unknown" {
				_, err = tx.Exec(ctx, `
INSERT INTO vehicle_attributes (event_id, tenant_id, site_id, camera_id, vehicle_type, vehicle_type_confidence, vehicle_color, vehicle_color_confidence, color_quality, brand, model, processed_at)
VALUES ($1, $2, $3, $4, $5, 1.0, $6, 1.0, 'good', $7, $8, now())
ON CONFLICT (event_id) DO UPDATE SET
    vehicle_type = EXCLUDED.vehicle_type,
    vehicle_color = EXCLUDED.vehicle_color,
    brand = EXCLUDED.brand,
    model = EXCLUDED.model,
    processed_at = now()`,
					evID, tenantID, siteID, cam, vType, vColor, brand, model)
				if err != nil {
					return fmt.Errorf("store vehicle attributes for agent event %s: %w", e.ID, err)
				}
			}

			if eventIngested {
				ingested++
			}
		}
		_, err = tx.Exec(ctx, `
INSERT INTO server_agent_event_cursors (server_id, tenant_id, last_seq, ingested_total, skipped_total, agent_lost, agent_depth, agent_epoch, last_pull_at, last_error)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, now(), '')
ON CONFLICT (server_id) DO UPDATE SET
    last_seq = excluded.last_seq,
    agent_epoch = excluded.agent_epoch,
    ingested_total = server_agent_event_cursors.ingested_total + excluded.ingested_total,
    skipped_total = server_agent_event_cursors.skipped_total + excluded.skipped_total,
    agent_lost = excluded.agent_lost,
    agent_depth = excluded.agent_depth,
    last_pull_at = now(), last_error = '', updated_at = now()`,
			serverID, tenantID, int64(through), ingested, skipped, int64(min(page.Lost, 1<<62)), page.Depth, page.Epoch) //nolint:gosec // bounded above and below
		return err
	})
	if err != nil {
		return cursor, err
	}
	return through, nil
}

type agentCameraRef struct {
	id     uuid.UUID
	siteID uuid.UUID
	name   string
}

type cameraResolver struct {
	byID        map[uuid.UUID]agentCameraRef
	byIP        map[string]agentCameraRef
	byName      map[string]agentCameraRef
	byIPChannel map[string]agentCameraRef
}

func (r *cameraResolver) resolve(cameraID, cameraIP string, channelID *int) (agentCameraRef, bool) {
	if id, err := uuid.Parse(cameraID); err == nil {
		if ref, ok := r.byID[id]; ok {
			return ref, true
		}
	}
	if channelID != nil && *channelID > 0 {
		ch := *channelID
		if cameraIP != "" {
			if ref, ok := r.byIPChannel[fmt.Sprintf("%s:%d", cameraIP, ch)]; ok {
				return ref, true
			}
		}
		if ref, ok := r.byName[strconv.Itoa(ch)]; ok {
			return ref, true
		}
		if ref, ok := r.byName[fmt.Sprintf("%02d", ch)]; ok {
			return ref, true
		}
		if ref, ok := r.byName[fmt.Sprintf("ch%d", ch)]; ok {
			return ref, true
		}
		if ref, ok := r.byName[fmt.Sprintf("ch%02d", ch)]; ok {
			return ref, true
		}
		if ref, ok := r.byName[fmt.Sprintf("channel%d", ch)]; ok {
			return ref, true
		}
		if ref, ok := r.byName[fmt.Sprintf("channel %d", ch)]; ok {
			return ref, true
		}
	}
	if cameraIP != "" {
		if ref, ok := r.byIP[cameraIP]; ok {
			return ref, true
		}
	}
	if cameraID != "" {
		if ref, ok := r.byIP[cameraID]; ok {
			return ref, true
		}
		if ref, ok := r.byName[strings.ToLower(cameraID)]; ok {
			return ref, true
		}
	}
	return agentCameraRef{}, false
}

// extractChannel parses channel numbers from names or profiles like "1", "01", "ch1", "ch01", "channel1", "channel_1", "camera 1", "MediaProfile_Channel1".
func extractChannel(s string) (int, bool) {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return 0, false
	}
	if n, err := strconv.Atoi(s); err == nil && n >= 0 {
		return n, true
	}
	prefixes := []string{"channel_", "channel-", "channel ", "channel", "ch_", "ch-", "ch", "cam_", "cam-", "cam ", "cam", "camera_", "camera-", "camera ", "camera"}
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			rest := strings.TrimPrefix(s, p)
			var numStr string
			for _, r := range rest {
				if r >= '0' && r <= '9' {
					numStr += string(r)
				} else {
					break
				}
			}
			if numStr != "" {
				if n, err := strconv.Atoi(numStr); err == nil && n >= 0 {
					return n, true
				}
			}
		}
	}
	for _, part := range strings.FieldsFunc(s, func(r rune) bool {
		return r == '_' || r == '-' || r == '/' || r == '.' || r == ' '
	}) {
		for _, p := range prefixes {
			if strings.HasPrefix(part, p) {
				rest := strings.TrimPrefix(part, p)
				if n, err := strconv.Atoi(rest); err == nil && n >= 0 {
					return n, true
				}
			}
		}
	}
	return 0, false
}

// serverCameras returns the cameras central knows for the server, with their site: the only ones a
// reading may be attributed to. An agent cannot file a reading under a camera of another server.
func serverCameras(ctx context.Context, tx pgx.Tx, serverID, tenantID uuid.UUID) (*cameraResolver, error) {
	rows, err := tx.Query(ctx, `SELECT id, site_id, coalesce(onvif_endpoint, ''), remote_name, display_name, coalesce(onvif_profile, '') FROM cameras WHERE server_id = $1 AND tenant_id = $2 AND deleted_at IS NULL`, serverID, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	res := &cameraResolver{
		byID:        map[uuid.UUID]agentCameraRef{},
		byIP:        map[string]agentCameraRef{},
		byName:      map[string]agentCameraRef{},
		byIPChannel: map[string]agentCameraRef{},
	}
	for rows.Next() {
		var (
			id, site              uuid.UUID
			endpoint              string
			remote, disp, profile string
		)
		if err := rows.Scan(&id, &site, &endpoint, &remote, &disp, &profile); err != nil {
			return nil, err
		}
		name := remote
		if name == "" {
			name = disp
		}
		ref := agentCameraRef{id: id, siteID: site, name: name}
		res.byID[id] = ref
		var host string
		if endpoint != "" {
			if u, err := url.Parse(endpoint); err == nil && u.Hostname() != "" {
				host = u.Hostname()
				res.byIP[host] = ref
			}
		}
		if remote != "" {
			res.byName[strings.ToLower(remote)] = ref
		}
		if disp != "" {
			res.byName[strings.ToLower(disp)] = ref
		}
		if host != "" {
			if ch, ok := extractChannel(remote); ok {
				res.byIPChannel[fmt.Sprintf("%s:%d", host, ch)] = ref
			}
			if ch, ok := extractChannel(disp); ok {
				res.byIPChannel[fmt.Sprintf("%s:%d", host, ch)] = ref
			}
			if ch, ok := extractChannel(profile); ok {
				res.byIPChannel[fmt.Sprintf("%s:%d", host, ch)] = ref
			}
		}
	}
	return res, rows.Err()
}

// saveCursorOnly moves the cursor without touching the counters.
func (a *AgentIngestor) saveCursorOnly(ctx context.Context, serverID, tenantID uuid.UUID, seq int64, epoch string) error {
	return a.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
INSERT INTO server_agent_event_cursors (server_id, tenant_id, last_seq, agent_epoch) VALUES ($1, $2, $3, $4)
ON CONFLICT (server_id) DO UPDATE SET last_seq = excluded.last_seq, agent_epoch = excluded.agent_epoch, updated_at = now()`, serverID, tenantID, seq, epoch)
		return err
	})
}

// touch records a successful pull that returned nothing new.
func (a *AgentIngestor) touch(ctx context.Context, serverID, tenantID uuid.UUID, page provision.LPREventsPage) error {
	return a.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
INSERT INTO server_agent_event_cursors (server_id, tenant_id, agent_lost, agent_depth, agent_epoch, last_pull_at)
VALUES ($1, $2, $3, $4, $5, now())
ON CONFLICT (server_id) DO UPDATE SET agent_lost = excluded.agent_lost, agent_depth = excluded.agent_depth,
    agent_epoch = excluded.agent_epoch, last_pull_at = now(), last_error = '', updated_at = now()`,
			serverID, tenantID, int64(min(page.Lost, 1<<62)), page.Depth, page.Epoch) //nolint:gosec // bounded above
		return err
	})
}

// recordError keeps the last failure where an operator can see it. It never hides the original error.
func (a *AgentIngestor) recordError(ctx context.Context, serverID, tenantID uuid.UUID, cause error) {
	msg := strings.ToValidUTF8(cause.Error(), "")
	if len(msg) > 500 {
		msg = strings.ToValidUTF8(msg[:500], "") // cutting inside a multibyte character leaves an invalid tail
	}
	err := a.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
INSERT INTO server_agent_event_cursors (server_id, tenant_id, last_pull_at, last_error) VALUES ($1, $2, now(), $3)
ON CONFLICT (server_id) DO UPDATE SET last_pull_at = now(), last_error = excluded.last_error, updated_at = now()`,
			serverID, tenantID, msg)
		return err
	})
	if err != nil {
		a.Log.WarnContext(ctx, "agent ingest: record error", "server_id", serverID, "error", err)
	}
}
