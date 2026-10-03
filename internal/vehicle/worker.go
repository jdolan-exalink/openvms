package vehicle

import (
	"bytes"
	"context"
	"errors"
	"image"
	_ "image/jpeg"
	_ "image/png"

	_ "golang.org/x/image/webp"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/jdolan-exalink/openvms/internal/rules"
	"github.com/jdolan-exalink/openvms/internal/store"
)

// Blobs reads the thumbnail the event syncer already stored.
type Blobs interface {
	Get(ctx context.Context, key string) ([]byte, string, error)
}

// Publisher notifies listeners that an event gained attributes.
type Publisher func(ctx context.Context, tenantID uuid.UUID, eventID uuid.UUID)

// modelVersion identifies the color sampler. Version 3 named a neutral body
// from the mean of the whole frame, so a white vehicle on gray pavement came
// out gray. Version 4 reads the light body when it stands above that pavement.
const modelVersion = "4"

// personModelVersion identifies the clothing sampler. Version 1 read the street
// when the person box was blue, and it dropped near-black cloth.
const personModelVersion = "2"

// Worker drains vehicle_attribute_jobs. It never blocks event ingest.
type Worker struct {
	Store *store.Store
	Blobs Blobs
	Log   *slog.Logger
	Rules interface {
		EvaluateEvent(ctx context.Context, ev rules.EventContext) error
	}
	Publish  Publisher
	Interval time.Duration
	// Crops reads the Frigate object snapshot. The stored review thumbnail is the
	// whole scene, so color is taken from the crop when Frigate still has it.
	Crops func(ctx context.Context, serverID uuid.UUID, detectionID string) ([]byte, error)
	// Rects reads the tracked object's label and its box and detector region,
	// each [x, y, width, height] normalized to the frame.
	Rects func(ctx context.Context, serverID uuid.UUID, detectionID string) (label string, box, region [4]float64, ok bool)
	// Body names sedan, SUV, pickup, van and the other car bodies from the
	// green-box crop. Nil keeps the Frigate label.
	Body *BodyClassifier
}

// Run polls until ctx is cancelled. It does not revisit events that were
// already ingested: classification starts from new reviews only.
func (w *Worker) Run(ctx context.Context) {
	interval := w.Interval
	if interval <= 0 {
		interval = 2 * time.Second
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		if err := w.Drain(ctx); err != nil && ctx.Err() == nil {
			w.Log.WarnContext(ctx, "vehicle enrichment", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Drain processes every job whose thumbnail is already stored.
func (w *Worker) Drain(ctx context.Context) error {
	for {
		ok, err := w.one(ctx)
		if err != nil {
			return err
		}
		person, err := w.onePerson(ctx)
		if err != nil {
			return err
		}
		if !ok && !person {
			return nil
		}
	}
}

func (w *Worker) one(ctx context.Context) (bool, error) {
	var eventID uuid.UUID
	var reevaluate bool
	err := w.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			UPDATE vehicle_attribute_jobs j
			SET status = 'processing', attempts = j.attempts + 1, updated_at = now()
			WHERE j.event_id = (
				SELECT j2.event_id
				FROM vehicle_attribute_jobs j2
				JOIN events e ON e.id = j2.event_id
				WHERE j2.status = 'pending' AND e.thumb_key <> ''
				ORDER BY j2.created_at DESC
				FOR UPDATE OF j2 SKIP LOCKED
				LIMIT 1
			)
			RETURNING j.event_id, j.reevaluate`).Scan(&eventID, &reevaluate)
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	if err := w.enrich(ctx, eventID, reevaluate); err != nil {
		_ = w.fail(ctx, eventID, err)
		return true, nil
	}
	return true, nil
}

func (w *Worker) enrich(ctx context.Context, eventID uuid.UUID, reevaluate bool) error {
	var key string
	var labels, detectionIDs []string
	var tenantID, siteID, cameraID, serverID uuid.UUID
	var cameraName, severity string
	var start time.Time
	err := w.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			SELECT e.thumb_key, e.labels, e.detection_ids, e.tenant_id, e.site_id, e.camera_id, e.server_id,
			       c.display_name, e.severity, e.start_time
			FROM events e
			JOIN cameras c ON c.id = e.camera_id
			WHERE e.id = $1`, eventID).Scan(&key, &labels, &detectionIDs, &tenantID, &siteID, &cameraID, &serverID, &cameraName, &severity, &start)
	})
	if err != nil {
		return err
	}
	img, err := w.still(ctx, key, serverID, detectionIDs, HasGreenBox)
	if err != nil {
		return err
	}
	color := ClassifyColor(img)
	frigateType, frigateConf := SubjectType(labels, HasGreenBox(img))
	if color.Name == "unknown" || color.Name == "other" {
		if alt, ok := w.colorFromRegion(ctx, serverID, detectionIDs, frigateType); ok {
			color = alt
		}
	}
	bodyType, bodyConf := "", float32(0)
	attempted, accepted := false, false
	classifyOn := true
	if w.Body != nil {
		on, err := BodyEnabled(ctx, w.Store, serverID, cameraID)
		if err != nil {
			if w.Log != nil {
				w.Log.WarnContext(ctx, "body classify switch", "error", err)
			}
		} else {
			classifyOn = on
		}
	}
	if frigateType == "car" && w.Body != nil && classifyOn {
		if view, ok := bodyView(img); ok {
			attempted = true
			bodyType, bodyConf, accepted = w.Body.Classify(view)
		}
	}
	cab, trailer, rig := ClassifyRig(img)
	typ, typeConf, fromModel := applyBody(frigateType, frigateConf, bodyType, bodyConf, attempted, accepted, rig)
	modelName, version := "frigate-label+lab", modelVersion
	if fromModel {
		modelName, version = bodyModelName, bodyModelVersion
	}
	trailerName, trailerConf := "", float32(0)
	if typ == "truck_trailer" {
		trailerName, trailerConf = trailer.Name, trailer.Confidence
		if cab.Name != "unknown" && cab.Name != "other" {
			color = cab
		}
	}
	err = w.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			INSERT INTO vehicle_attributes (
				event_id, tenant_id, site_id, camera_id,
				vehicle_type, vehicle_type_confidence,
				vehicle_color, vehicle_color_confidence, color_quality,
				trailer_color, trailer_color_confidence,
				model_name, model_version)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
			ON CONFLICT (event_id) DO UPDATE SET
				vehicle_type = excluded.vehicle_type,
				vehicle_type_confidence = excluded.vehicle_type_confidence,
				vehicle_color = excluded.vehicle_color,
				vehicle_color_confidence = excluded.vehicle_color_confidence,
				color_quality = excluded.color_quality,
				trailer_color = excluded.trailer_color,
				trailer_color_confidence = excluded.trailer_color_confidence,
				model_name = excluded.model_name,
				model_version = excluded.model_version,
				processed_at = now()`,
			eventID, tenantID, siteID, cameraID, typ, typeConf, color.Name, color.Confidence, color.Quality, trailerName, trailerConf, modelName, version); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE vehicle_attribute_jobs SET status = 'completed', last_error = '', updated_at = now() WHERE event_id = $1`, eventID)
		return err
	})
	if err != nil {
		return err
	}
	if w.Publish != nil {
		w.Publish(ctx, tenantID, eventID)
	}
	if w.Rules != nil && reevaluate {
		_ = w.Rules.EvaluateEvent(ctx, rules.EventContext{
			ID: eventID, TenantID: tenantID, SiteID: siteID, ServerID: serverID, CameraID: cameraID,
			CameraName: cameraName, Severity: severity, Labels: labels, Start: start,
			VehicleType: typ, VehicleColor: color.Name,
		})
	}
	return nil
}

func (w *Worker) still(ctx context.Context, key string, serverID uuid.UUID, detectionIDs []string, want func(image.Image) bool) (image.Image, error) {
	var first image.Image
	if w.Crops != nil {
		for _, id := range detectionIDs {
			if id == "" {
				continue
			}
			body, err := w.Crops(ctx, serverID, id)
			if err != nil {
				continue
			}
			img, err := decodeStill(body)
			if err != nil {
				continue
			}
			if want != nil && want(img) {
				return img, nil
			}
			if first == nil {
				first = img
			}
		}
	}
	body, _, err := w.Blobs.Get(ctx, key)
	if err == nil {
		if img, err := decodeStill(body); err == nil && (want == nil || want(img) || first == nil) {
			return img, nil
		}
	}
	if first != nil {
		return first, nil
	}
	if err != nil {
		return nil, err
	}
	return nil, errors.New("no still")
}

func (w *Worker) colorFromRegion(ctx context.Context, serverID uuid.UUID, detectionIDs []string, want string) (Color, bool) {
	if w.Rects == nil || w.Crops == nil || want == "unknown" || want == "" {
		return Color{}, false
	}
	for _, id := range detectionIDs {
		if id == "" {
			continue
		}
		label, box, region, ok := w.Rects(ctx, serverID, id)
		if !ok || label != want {
			continue
		}
		body, err := w.Crops(ctx, serverID, id)
		if err != nil {
			continue
		}
		img, err := decodeStill(body)
		if err != nil {
			continue
		}
		rect := RegionBox(img, region, box)
		if rect.Empty() {
			continue
		}
		color := ClassifyColorBox(img, rect)
		if color.Name != "unknown" && color.Name != "other" {
			return color, true
		}
	}
	return Color{}, false
}

func decodeStill(body []byte) (image.Image, error) {
	img, _, err := image.Decode(bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	return img, nil
}

func (w *Worker) fail(ctx context.Context, eventID uuid.UUID, cause error) error {
	return w.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			UPDATE vehicle_attribute_jobs
			SET status = CASE WHEN attempts >= 5 THEN 'failed' ELSE 'pending' END,
			    last_error = $2, updated_at = now()
			WHERE event_id = $1`, eventID, cause.Error())
		return err
	})
}

func (w *Worker) onePerson(ctx context.Context) (bool, error) {
	var eventID uuid.UUID
	err := w.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			UPDATE person_attribute_jobs j
			SET status = 'processing', attempts = j.attempts + 1, updated_at = now()
			WHERE j.event_id = (
				SELECT j2.event_id
				FROM person_attribute_jobs j2
				JOIN events e ON e.id = j2.event_id
				WHERE j2.status = 'pending' AND e.thumb_key <> ''
				ORDER BY j2.created_at DESC
				FOR UPDATE OF j2 SKIP LOCKED
				LIMIT 1
			)
			RETURNING j.event_id`).Scan(&eventID)
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		if isMissingRelation(err) {
			return false, nil
		}
		return false, err
	}
	if err := w.enrichPerson(ctx, eventID); err != nil {
		_ = w.failPerson(ctx, eventID, err)
		return true, nil
	}
	return true, nil
}

func isMissingRelation(err error) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && pg.Code == "42P01"
}

func (w *Worker) enrichPerson(ctx context.Context, eventID uuid.UUID) error {
	var key string
	var detectionIDs []string
	var tenantID, siteID, cameraID, serverID uuid.UUID
	err := w.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			SELECT e.thumb_key, e.detection_ids, e.tenant_id, e.site_id, e.camera_id, e.server_id
			FROM events e WHERE e.id = $1`, eventID).Scan(&key, &detectionIDs, &tenantID, &siteID, &cameraID, &serverID)
	})
	if err != nil {
		return err
	}
	img, err := w.still(ctx, key, serverID, detectionIDs, HasPersonBox)
	if err != nil {
		return err
	}
	cloth := ClassifyClothing(img)
	if cloth.Upper.Name == "unknown" && cloth.Lower.Name == "unknown" {
		if alt, ok := w.clothingFromRegion(ctx, serverID, detectionIDs); ok {
			cloth = alt
		}
	}
	quality := cloth.Upper.Quality
	if cloth.Lower.Confidence > cloth.Upper.Confidence {
		quality = cloth.Lower.Quality
	}
	return w.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			INSERT INTO person_attributes (
				event_id, tenant_id, site_id, camera_id,
				upper_color, upper_color_confidence,
				lower_color, lower_color_confidence, color_quality,
				model_name, model_version)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,'frigate-box+lab',$10)
			ON CONFLICT (event_id) DO UPDATE SET
				upper_color = excluded.upper_color,
				upper_color_confidence = excluded.upper_color_confidence,
				lower_color = excluded.lower_color,
				lower_color_confidence = excluded.lower_color_confidence,
				color_quality = excluded.color_quality,
				model_name = excluded.model_name,
				model_version = excluded.model_version,
				processed_at = now()`,
			eventID, tenantID, siteID, cameraID,
			cloth.Upper.Name, cloth.Upper.Confidence, cloth.Lower.Name, cloth.Lower.Confidence, quality, personModelVersion); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE person_attribute_jobs SET status = 'completed', last_error = '', updated_at = now() WHERE event_id = $1`, eventID)
		return err
	})
}

func (w *Worker) clothingFromRegion(ctx context.Context, serverID uuid.UUID, detectionIDs []string) (Clothing, bool) {
	if w.Rects == nil || w.Crops == nil {
		return Clothing{}, false
	}
	for _, id := range detectionIDs {
		if id == "" {
			continue
		}
		label, box, region, ok := w.Rects(ctx, serverID, id)
		if !ok || label != "person" {
			continue
		}
		body, err := w.Crops(ctx, serverID, id)
		if err != nil {
			continue
		}
		img, err := decodeStill(body)
		if err != nil {
			continue
		}
		rect := RegionBox(img, region, box)
		if rect.Empty() {
			continue
		}
		cloth := ClassifyClothingBox(img, rect)
		if cloth.Upper.Name != "unknown" || cloth.Lower.Name != "unknown" {
			return cloth, true
		}
	}
	return Clothing{}, false
}

func (w *Worker) failPerson(ctx context.Context, eventID uuid.UUID, cause error) error {
	return w.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			UPDATE person_attribute_jobs
			SET status = CASE WHEN attempts >= 5 THEN 'failed' ELSE 'pending' END,
			    last_error = $2, updated_at = now()
			WHERE event_id = $1`, eventID, cause.Error())
		return err
	})
}

// EnqueuePerson records a clothing job. It is a no-op when the event has no person.
func EnqueuePerson(ctx context.Context, tx pgx.Tx, eventID, tenantID uuid.UUID, labels []string) error {
	if !IsPerson(labels) {
		return nil
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO person_attribute_jobs (event_id, tenant_id)
		VALUES ($1, $2)
		ON CONFLICT (event_id) DO NOTHING`, eventID, tenantID)
	return err
}

// Enqueue records a job for a newly ingested vehicle event. It is a no-op for other labels.
func Enqueue(ctx context.Context, tx pgx.Tx, eventID, tenantID uuid.UUID, labels []string) error {
	if !IsVehicle(labels) {
		return nil
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO vehicle_attribute_jobs (event_id, tenant_id)
		VALUES ($1, $2)
		ON CONFLICT (event_id) DO NOTHING`, eventID, tenantID)
	return err
}

// Requeue sends one event back through color and type classification. It does not
// raise alarms. created_at is refreshed so this event is read before the backlog.
func Requeue(ctx context.Context, tx pgx.Tx, eventID, tenantID uuid.UUID, labels []string) error {
	if !IsVehicle(labels) {
		return nil
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO vehicle_attribute_jobs (event_id, tenant_id, status, attempts, reevaluate, created_at, updated_at)
		VALUES ($1, $2, 'pending', 0, false, now(), now())
		ON CONFLICT (event_id) DO UPDATE SET
			status = 'pending', attempts = 0, reevaluate = false, last_error = '',
			created_at = now(), updated_at = now()`, eventID, tenantID)
	return err
}

// RequeuePerson sends one person back through clothing color. It does not raise alarms.
func RequeuePerson(ctx context.Context, tx pgx.Tx, eventID, tenantID uuid.UUID, labels []string) error {
	if !IsPerson(labels) {
		return nil
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO person_attribute_jobs (event_id, tenant_id, status, attempts, reevaluate, created_at, updated_at)
		VALUES ($1, $2, 'pending', 0, false, now(), now())
		ON CONFLICT (event_id) DO UPDATE SET
			status = 'pending', attempts = 0, reevaluate = false, last_error = '',
			created_at = now(), updated_at = now()`, eventID, tenantID)
	return err
}
