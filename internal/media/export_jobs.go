package media

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/jdolan-exalink/openvms/internal/access"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/identity"
	"github.com/jdolan-exalink/openvms/internal/platform/httpx"
	"github.com/jdolan-exalink/openvms/internal/platform/logging"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

const (
	ActionExportCancelled     = "EXPORT_CANCELLED"
	ActionExportRetried       = "EXPORT_RETRIED"
	ActionExportDeleted       = "EXPORT_DELETED"
	ActionExportShared        = "EXPORT_SHARED"
	ActionExportShareRevoked  = "EXPORT_SHARE_REVOKED"
	ActionExportShareAccessed = "EXPORT_SHARE_ACCESSED"
)

var (
	ErrSharePasswordRequired = errors.New("share password required")
	ErrSharePasswordInvalid  = errors.New("invalid share password")
	ErrShareExpired          = errors.New("share has expired")
	ErrShareRevoked          = errors.New("share has been revoked")
)

type ExportJob struct {
	ID               uuid.UUID
	TenantID         uuid.UUID
	SiteID           *uuid.UUID
	RequestedBy      uuid.UUID
	Requester        string
	Name             string
	Start            time.Time
	End              time.Time
	Status           string
	Progress         float32
	Error            string
	TotalBytes       int64
	TransferredBytes int64
	SpeedBps         float32
	ETASeconds       int
	CameraCount      int
	LocalPath        string
	Manifest         map[string]any
	Protected        bool
	ExpiresAt        *time.Time
	Items            []ExportJobItem
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type ExportJobItem struct {
	ID               uuid.UUID
	JobID            uuid.UUID
	TenantID         uuid.UUID
	CameraID         uuid.UUID
	CameraName       string
	ServerID         uuid.UUID
	ServerName       string
	RemoteExportID   string
	Status           string
	Progress         float32
	Error            string
	TotalBytes       int64
	TransferredBytes int64
	RemotePath       string
	LocalPath        string
	SHA256Hash       string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type CreateExportJobInput struct {
	CameraIDs []uuid.UUID
	Start     time.Time
	End       time.Time
	Name      string
	Protected bool
}

// MaxExportJobLength limits multi-camera export range to 24 hours.
const MaxExportJobLength = 24 * time.Hour

func (s *Service) CreateExportJob(ctx context.Context, actor authz.Actor, in CreateExportJobInput) (ExportJob, error) {
	in.Name = strings.TrimSpace(in.Name)
	if len(in.CameraIDs) == 0 {
		return ExportJob{}, invalid("at least one camera is required")
	}
	switch {
	case !in.End.After(in.Start):
		return ExportJob{}, invalid("end must be after start")
	case in.End.Sub(in.Start) > MaxExportJobLength:
		return ExportJob{}, invalid("an export job can be at most 24 hours long")
	case in.End.After(time.Now().Add(time.Minute)):
		return ExportJob{}, invalid("the range cannot end in the future")
	case len(in.Name) > 200:
		return ExportJob{}, invalid("name is too long")
	}

	type targetCamera struct {
		id         uuid.UUID
		remoteName string
		tenantID   uuid.UUID
		siteID     uuid.UUID
		serverID   uuid.UUID
	}
	targets := make([]targetCamera, 0, len(in.CameraIDs))
	seen := make(map[uuid.UUID]bool, len(in.CameraIDs))

	for _, cid := range in.CameraIDs {
		if seen[cid] {
			continue
		}
		seen[cid] = true
		cam, err := s.Authorize(ctx, actor, cid, authz.ExportsCreate)
		if err != nil {
			return ExportJob{}, err
		}
		targets = append(targets, targetCamera{
			id:         cam.ID,
			remoteName: cam.RemoteName,
			tenantID:   cam.TenantID,
			siteID:     cam.SiteID,
			serverID:   cam.Server.ID,
		})
	}

	if in.Name == "" {
		if len(targets) == 1 {
			in.Name = fmt.Sprintf("%s %s", targets[0].remoteName, in.Start.Local().Format("2006-01-02 15:04"))
		} else {
			in.Name = fmt.Sprintf("Exportación %s (%d cámaras)", in.Start.Local().Format("2006-01-02 15:04"), len(targets))
		}
	}

	var jobID uuid.UUID
	firstTenant := targets[0].tenantID
	var firstSite *uuid.UUID
	if targets[0].siteID != uuid.Nil {
		s := targets[0].siteID
		firstSite = &s
	}

	err := s.rawTx(ctx, actor, func(tx pgx.Tx, _ *access.Checker) error {
		err := tx.QueryRow(ctx, `
INSERT INTO export_jobs (tenant_id, site_id, requested_by, name, start_time, end_time, status, protected, expires_at)
VALUES ($1, $2, $3, $4, $5, $6, 'queued', $7, CASE WHEN $7 = true THEN NULL ELSE now() + interval '30 days' END) RETURNING id`,
			firstTenant, firstSite, actor.UserID, in.Name, in.Start, in.End, in.Protected).Scan(&jobID)
		if err != nil {
			return err
		}

		for _, t := range targets {
			_, err := tx.Exec(ctx, `
INSERT INTO export_job_items (job_id, tenant_id, camera_id, server_id, status)
VALUES ($1, $2, $3, $4, 'queued')`,
				jobID, t.tenantID, t.id, t.serverID)
			if err != nil {
				return err
			}
		}

		b, _ := json.Marshal(map[string]any{
			"job_id":    jobID,
			"cameras":   len(targets),
			"start":     in.Start,
			"end":       in.End,
			"name":      in.Name,
			"protected": in.Protected,
		})
		return db.New(tx).InsertAudit(ctx, db.InsertAuditParams{
			TenantID:   &firstTenant,
			ActorID:    &actor.UserID,
			ActorName:  actor.Username,
			Action:     ActionExportCreated,
			TargetType: "export_job",
			TargetID:   &jobID,
			RequestID:  logging.RequestID(ctx),
			Ip:         httpx.ClientIP(ctx),
			Details:    b,
		})
	})
	if err != nil {
		return ExportJob{}, err
	}

	return s.GetExportJob(ctx, actor, jobID)
}

const exportJobColumns = `j.id, j.tenant_id, j.site_id, j.requested_by, u.username, j.name, j.start_time, j.end_time,
j.status, j.progress, j.error, j.total_bytes, j.transferred_bytes, j.speed_bps, j.eta_seconds, j.local_path,
j.manifest, j.protected, j.expires_at, j.created_at, j.updated_at,
COALESCE((SELECT count(*) FROM export_job_items i WHERE i.job_id = j.id), 0) AS camera_count`

func scanExportJob(row pgx.Row) (ExportJob, error) {
	var j ExportJob
	var manifestBytes []byte
	err := row.Scan(
		&j.ID, &j.TenantID, &j.SiteID, &j.RequestedBy, &j.Requester, &j.Name, &j.Start, &j.End,
		&j.Status, &j.Progress, &j.Error, &j.TotalBytes, &j.TransferredBytes, &j.SpeedBps, &j.ETASeconds, &j.LocalPath,
		&manifestBytes, &j.Protected, &j.ExpiresAt, &j.CreatedAt, &j.UpdatedAt, &j.CameraCount,
	)
	if err != nil {
		return j, err
	}
	if len(manifestBytes) > 0 {
		_ = json.Unmarshal(manifestBytes, &j.Manifest)
	}
	if j.Manifest == nil {
		j.Manifest = map[string]any{}
	}
	return j, nil
}

func (s *Service) ListExportJobs(ctx context.Context, actor authz.Actor) ([]ExportJob, error) {
	var out []ExportJob
	err := s.rawTx(ctx, actor, func(tx pgx.Tx, c *access.Checker) error {
		rows, err := tx.Query(ctx, `
SELECT `+exportJobColumns+`
FROM export_jobs j
JOIN users u ON u.id = j.requested_by
WHERE j.deleted_at IS NULL
ORDER BY j.created_at DESC LIMIT 200`)
		if err != nil {
			return err
		}
		defer rows.Close()
		jobIndex := make(map[uuid.UUID]int)
		var jobIDs []uuid.UUID
		for rows.Next() {
			job, err := scanExportJob(rows)
			if err != nil {
				return err
			}
			job.Items = []ExportJobItem{}
			jobIndex[job.ID] = len(out)
			jobIDs = append(jobIDs, job.ID)
			out = append(out, job)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		rows.Close()

		if len(jobIDs) > 0 {
			itemRows, err := tx.Query(ctx, `
SELECT i.id, i.job_id, i.tenant_id, i.camera_id, COALESCE(c.display_name, ''), i.server_id, COALESCE(s.name, ''),
       i.remote_export_id, i.status, i.progress, COALESCE(i.error, ''), i.total_bytes, i.transferred_bytes, i.remote_path, i.local_path,
       i.sha256_hash, i.created_at, i.updated_at
FROM export_job_items i
LEFT JOIN cameras c ON c.id = i.camera_id
LEFT JOIN frigate_servers s ON s.id = i.server_id
WHERE i.job_id = ANY($1)
ORDER BY i.created_at ASC`, jobIDs)
			if err != nil {
				return err
			}
			defer itemRows.Close()
			for itemRows.Next() {
				var it ExportJobItem
				if err := itemRows.Scan(
					&it.ID, &it.JobID, &it.TenantID, &it.CameraID, &it.CameraName, &it.ServerID, &it.ServerName,
					&it.RemoteExportID, &it.Status, &it.Progress, &it.Error, &it.TotalBytes, &it.TransferredBytes,
					&it.RemotePath, &it.LocalPath, &it.SHA256Hash, &it.CreatedAt, &it.UpdatedAt,
				); err != nil {
					return err
				}
				if idx, ok := jobIndex[it.JobID]; ok {
					out[idx].Items = append(out[idx].Items, it)
				}
			}
			if err := itemRows.Err(); err != nil {
				return err
			}
		}
		return nil
	})
	if out == nil {
		out = []ExportJob{}
	}
	return out, err
}

func (s *Service) GetExportJob(ctx context.Context, actor authz.Actor, id uuid.UUID) (ExportJob, error) {
	var job ExportJob
	err := s.rawTx(ctx, actor, func(tx pgx.Tx, c *access.Checker) error {
		row := tx.QueryRow(ctx, `
SELECT `+exportJobColumns+`
FROM export_jobs j
JOIN users u ON u.id = j.requested_by
WHERE j.id = $1 AND j.deleted_at IS NULL`, id)
		var err error
		job, err = scanExportJob(row)
		if err != nil {
			return store.Classify(err)
		}

		itemRows, err := tx.Query(ctx, `
SELECT i.id, i.job_id, i.tenant_id, i.camera_id, COALESCE(c.display_name, ''), i.server_id, COALESCE(s.name, ''),
i.remote_export_id, i.status, i.progress, i.error, i.total_bytes, i.transferred_bytes, i.remote_path, i.local_path,
i.sha256_hash, i.created_at, i.updated_at
FROM export_job_items i
LEFT JOIN cameras c ON c.id = i.camera_id
LEFT JOIN frigate_servers s ON s.id = i.server_id
WHERE i.job_id = $1
ORDER BY i.created_at ASC`, id)
		if err != nil {
			return err
		}
		defer itemRows.Close()

		for itemRows.Next() {
			var it ExportJobItem
			if err := itemRows.Scan(
				&it.ID, &it.JobID, &it.TenantID, &it.CameraID, &it.CameraName, &it.ServerID, &it.ServerName,
				&it.RemoteExportID, &it.Status, &it.Progress, &it.Error, &it.TotalBytes, &it.TransferredBytes,
				&it.RemotePath, &it.LocalPath, &it.SHA256Hash, &it.CreatedAt, &it.UpdatedAt,
			); err != nil {
				return err
			}
			job.Items = append(job.Items, it)
		}
		return itemRows.Err()
	})
	if job.Items == nil {
		job.Items = []ExportJobItem{}
	}
	return job, err
}

func (s *Service) CancelExportJob(ctx context.Context, actor authz.Actor, id uuid.UUID) (ExportJob, error) {
	job, err := s.GetExportJob(ctx, actor, id)
	if err != nil {
		return ExportJob{}, err
	}
	err = s.rawTx(ctx, actor, func(tx pgx.Tx, c *access.Checker) error {
		tag, err := tx.Exec(ctx, `
UPDATE export_jobs SET status = 'cancelled', updated_at = now()
WHERE id = $1 AND deleted_at IS NULL AND status IN ('queued', 'preparing', 'transferring', 'processing')`, id)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return nil
		}
		_, err = tx.Exec(ctx, `
UPDATE export_job_items SET status = 'cancelled', updated_at = now()
WHERE job_id = $1 AND status NOT IN ('ready', 'failed')`, id)
		if err != nil {
			return err
		}
		return db.New(tx).InsertAudit(ctx, db.InsertAuditParams{
			TenantID:   &job.TenantID,
			ActorID:    &actor.UserID,
			ActorName:  actor.Username,
			Action:     ActionExportCancelled,
			TargetType: "export_job",
			TargetID:   &id,
			RequestID:  logging.RequestID(ctx),
			Ip:         httpx.ClientIP(ctx),
			Details:    []byte(`{"name":"` + job.Name + `"}`),
		})
	})
	if err != nil {
		return ExportJob{}, err
	}
	return s.GetExportJob(ctx, actor, id)
}

func (s *Service) RetryExportJob(ctx context.Context, actor authz.Actor, id uuid.UUID) (ExportJob, error) {
	job, err := s.GetExportJob(ctx, actor, id)
	if err != nil {
		return ExportJob{}, err
	}
	err = s.rawTx(ctx, actor, func(tx pgx.Tx, c *access.Checker) error {
		tag, err := tx.Exec(ctx, `
UPDATE export_jobs SET status = 'queued', error = '', progress = 0, updated_at = now()
WHERE id = $1 AND deleted_at IS NULL AND status IN ('failed', 'cancelled')`, id)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return nil
		}
		_, err = tx.Exec(ctx, `
UPDATE export_job_items SET status = 'queued', error = '', progress = 0, updated_at = now()
WHERE job_id = $1 AND status != 'ready'`, id)
		if err != nil {
			return err
		}
		return db.New(tx).InsertAudit(ctx, db.InsertAuditParams{
			TenantID:   &job.TenantID,
			ActorID:    &actor.UserID,
			ActorName:  actor.Username,
			Action:     ActionExportRetried,
			TargetType: "export_job",
			TargetID:   &id,
			RequestID:  logging.RequestID(ctx),
			Ip:         httpx.ClientIP(ctx),
			Details:    []byte(`{"name":"` + job.Name + `"}`),
		})
	})
	if err != nil {
		return ExportJob{}, err
	}
	return s.GetExportJob(ctx, actor, id)
}

type UpdateExportJobInput struct {
	Name      *string `json:"name"`
	Protected *bool   `json:"protected"`
}

func (s *Service) UpdateExportJob(ctx context.Context, actor authz.Actor, id uuid.UUID, in UpdateExportJobInput) (ExportJob, error) {
	job, err := s.GetExportJob(ctx, actor, id)
	if err != nil {
		return ExportJob{}, err
	}
	if job.RequestedBy != actor.UserID {
		return ExportJob{}, access.ErrForbidden
	}

	err = s.rawTx(ctx, actor, func(tx pgx.Tx, _ *access.Checker) error {
		newName := job.Name
		if in.Name != nil && strings.TrimSpace(*in.Name) != "" {
			newName = strings.TrimSpace(*in.Name)
		}
		newProtected := job.Protected
		if in.Protected != nil {
			newProtected = *in.Protected
		}

		_, err := tx.Exec(ctx, `
UPDATE export_jobs
SET name = $2,
    protected = $3,
    expires_at = CASE 
        WHEN $3 = true THEN NULL 
        WHEN $3 = false AND expires_at IS NULL THEN now() + interval '30 days'
        ELSE expires_at 
    END,
    updated_at = now()
WHERE id = $1 AND deleted_at IS NULL`, id, newName, newProtected)
		if err != nil {
			return err
		}

		b, _ := json.Marshal(map[string]any{
			"name":      newName,
			"protected": newProtected,
		})
		return db.New(tx).InsertAudit(ctx, db.InsertAuditParams{
			TenantID:   &job.TenantID,
			ActorID:    &actor.UserID,
			ActorName:  actor.Username,
			Action:     "EXPORT_UPDATED",
			TargetType: "export_job",
			TargetID:   &id,
			RequestID:  logging.RequestID(ctx),
			Ip:         httpx.ClientIP(ctx),
			Details:    b,
		})
	})
	if err != nil {
		return ExportJob{}, err
	}
	return s.GetExportJob(ctx, actor, id)
}

func (s *Service) DeleteExportJob(ctx context.Context, actor authz.Actor, id uuid.UUID) error {
	job, err := s.GetExportJob(ctx, actor, id)
	if err != nil {
		return err
	}
	if job.RequestedBy != actor.UserID {
		return access.ErrForbidden
	}
	return s.rawTx(ctx, actor, func(tx pgx.Tx, _ *access.Checker) error {
		_, err := tx.Exec(ctx, `UPDATE export_jobs SET deleted_at = now() WHERE id = $1`, id)
		if err != nil {
			return err
		}
		return db.New(tx).InsertAudit(ctx, db.InsertAuditParams{
			TenantID:   &job.TenantID,
			ActorID:    &actor.UserID,
			ActorName:  actor.Username,
			Action:     ActionExportDeleted,
			TargetType: "export_job",
			TargetID:   &id,
			RequestID:  logging.RequestID(ctx),
			Ip:         httpx.ClientIP(ctx),
			Details:    []byte(`{"name":"` + job.Name + `"}`),
		})
	})
}

type ExportShare struct {
	ID          uuid.UUID  `json:"id"`
	TenantID    uuid.UUID  `json:"tenant_id"`
	JobID       uuid.UUID  `json:"job_id"`
	ShareToken  string     `json:"share_token"`
	CreatedBy   uuid.UUID  `json:"created_by"`
	ExpiresAt   *time.Time `json:"expires_at"`
	HasPassword bool       `json:"has_password"`
	ViewsCount  int        `json:"views_count"`
	CreatedAt   time.Time  `json:"created_at"`
	RevokedAt   *time.Time `json:"revoked_at"`

	// AccessCookie is set only when GetPublicShare verified a password; callers turn it
	// into a cookie. It is never serialized.
	AccessCookie    string    `json:"-"`
	AccessCookieExp time.Time `json:"-"`
}

type CreateExportShareInput struct {
	ExpiresAt *time.Time `json:"expires_at"`
	Password  string     `json:"password"`
}

func (s *Service) CreateExportShare(ctx context.Context, actor authz.Actor, jobID uuid.UUID, in CreateExportShareInput) (ExportShare, error) {
	job, err := s.GetExportJob(ctx, actor, jobID)
	if err != nil {
		return ExportShare{}, err
	}
	if job.Status != "ready" {
		return ExportShare{}, invalid("export job is not ready yet")
	}

	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return ExportShare{}, err
	}
	token := hex.EncodeToString(b)

	var pwHash string
	if in.Password != "" {
		h, err := identity.HashPassword(in.Password)
		if err != nil {
			return ExportShare{}, fmt.Errorf("hash share password: %w", err)
		}
		pwHash = h
	}

	var share ExportShare
	share.ID = uuid.New()
	share.TenantID = job.TenantID
	share.JobID = jobID
	share.ShareToken = token
	share.CreatedBy = actor.UserID
	share.ExpiresAt = in.ExpiresAt
	share.HasPassword = in.Password != ""
	share.CreatedAt = time.Now()

	err = s.rawTx(ctx, actor, func(tx pgx.Tx, _ *access.Checker) error {
		err := tx.QueryRow(ctx, `
INSERT INTO export_shares (id, tenant_id, job_id, share_token, created_by, expires_at, password_hash)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING id, created_at, updated_at`,
			share.ID, share.TenantID, share.JobID, share.ShareToken, share.CreatedBy, share.ExpiresAt, pwHash,
		).Scan(&share.ID, &share.CreatedAt, &share.CreatedAt)
		if err != nil {
			return err
		}

		bDetails, _ := json.Marshal(map[string]any{
			"job_id":       jobID,
			"job_name":     job.Name,
			"has_password": in.Password != "",
			"expires_at":   in.ExpiresAt,
		})
		return db.New(tx).InsertAudit(ctx, db.InsertAuditParams{
			TenantID:   &job.TenantID,
			ActorID:    &actor.UserID,
			ActorName:  actor.Username,
			Action:     ActionExportShared,
			TargetType: "export_share",
			TargetID:   &share.ID,
			RequestID:  logging.RequestID(ctx),
			Ip:         httpx.ClientIP(ctx),
			Details:    bDetails,
		})
	})
	if err != nil {
		return ExportShare{}, err
	}
	return share, nil
}

func (s *Service) ListExportShares(ctx context.Context, actor authz.Actor, jobID uuid.UUID) ([]ExportShare, error) {
	_, err := s.GetExportJob(ctx, actor, jobID)
	if err != nil {
		return nil, err
	}
	var out []ExportShare
	err = s.rawTx(ctx, actor, func(tx pgx.Tx, _ *access.Checker) error {
		rows, err := tx.Query(ctx, `
SELECT id, tenant_id, job_id, share_token, created_by, expires_at, (password_hash != ''), views_count, created_at, revoked_at
FROM export_shares
WHERE job_id = $1 AND revoked_at IS NULL
ORDER BY created_at DESC`, jobID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var sh ExportShare
			if err := rows.Scan(
				&sh.ID, &sh.TenantID, &sh.JobID, &sh.ShareToken, &sh.CreatedBy,
				&sh.ExpiresAt, &sh.HasPassword, &sh.ViewsCount, &sh.CreatedAt, &sh.RevokedAt,
			); err != nil {
				return err
			}
			out = append(out, sh)
		}
		return rows.Err()
	})
	if out == nil {
		out = []ExportShare{}
	}
	return out, err
}

func (s *Service) RevokeExportShare(ctx context.Context, actor authz.Actor, shareID uuid.UUID) error {
	return s.rawTx(ctx, actor, func(tx pgx.Tx, _ *access.Checker) error {
		var tenantID uuid.UUID
		var jobID uuid.UUID
		err := tx.QueryRow(ctx, `
UPDATE export_shares SET revoked_at = now()
WHERE id = $1 AND revoked_at IS NULL
RETURNING tenant_id, job_id`, shareID).Scan(&tenantID, &jobID)
		if err != nil {
			return err
		}
		return db.New(tx).InsertAudit(ctx, db.InsertAuditParams{
			TenantID:   &tenantID,
			ActorID:    &actor.UserID,
			ActorName:  actor.Username,
			Action:     ActionExportShareRevoked,
			TargetType: "export_share",
			TargetID:   &shareID,
			RequestID:  logging.RequestID(ctx),
			Ip:         httpx.ClientIP(ctx),
			Details:    []byte(`{"job_id":"` + jobID.String() + `"}`),
		})
	})
}

func (s *Service) GetPublicShare(ctx context.Context, token string, cred ShareCredential, clientIP *netip.Addr) (ExportJob, ExportShare, error) {
	var share ExportShare
	var pwHash string
	err := s.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		row := tx.QueryRow(ctx, `
SELECT id, tenant_id, job_id, share_token, created_by, expires_at, password_hash, views_count, created_at, revoked_at
FROM export_shares
WHERE share_token = $1`, token)
		if err := row.Scan(
			&share.ID, &share.TenantID, &share.JobID, &share.ShareToken, &share.CreatedBy,
			&share.ExpiresAt, &pwHash, &share.ViewsCount, &share.CreatedAt, &share.RevokedAt,
		); err != nil {
			return err
		}
		share.HasPassword = pwHash != ""
		if share.RevokedAt != nil {
			return ErrShareRevoked
		}
		if share.ExpiresAt != nil && share.ExpiresAt.Before(time.Now()) {
			return ErrShareExpired
		}
		if pwHash != "" {
			now := time.Now()
			switch {
			case cred.Password != "":
				if !identity.VerifyPassword(pwHash, cred.Password) {
					return ErrSharePasswordInvalid
				}
				exp := shareCookieExpiry(now, share.ExpiresAt)
				share.AccessCookie = signShareCookie(pwHash, token, exp)
				share.AccessCookieExp = exp
			case verifyShareCookie(pwHash, token, cred.Cookie, now):
			default:
				return ErrSharePasswordRequired
			}
		}

		// Increment views and audit access
		_, _ = tx.Exec(ctx, `UPDATE export_shares SET views_count = views_count + 1 WHERE id = $1`, share.ID)
		_ = db.New(tx).InsertAudit(ctx, db.InsertAuditParams{
			TenantID:   &share.TenantID,
			Action:     ActionExportShareAccessed,
			TargetType: "export_share",
			TargetID:   &share.ID,
			RequestID:  logging.RequestID(ctx),
			Ip:         clientIP,
			Details:    []byte(`{"token":"` + token[:8] + `..."}`),
		})
		return nil
	})
	if err != nil {
		return ExportJob{}, ExportShare{}, err
	}

	// Fetch the job using AllTenants
	var job ExportJob
	err = s.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		row := tx.QueryRow(ctx, `
SELECT `+exportJobColumns+`
FROM export_jobs j
JOIN users u ON u.id = j.requested_by
WHERE j.id = $1 AND j.deleted_at IS NULL`, share.JobID)
		var jerr error
		job, jerr = scanExportJob(row)
		if jerr != nil {
			return jerr
		}

		itemRows, err := tx.Query(ctx, `
SELECT i.id, i.job_id, i.tenant_id, i.camera_id, COALESCE(c.display_name, ''), i.server_id, COALESCE(s.name, ''),
       i.remote_export_id, i.status, i.progress, COALESCE(i.error, ''), i.total_bytes, i.transferred_bytes, i.remote_path, i.local_path,
       i.sha256_hash, i.created_at, i.updated_at
FROM export_job_items i
LEFT JOIN cameras c ON c.id = i.camera_id
LEFT JOIN frigate_servers s ON s.id = i.server_id
WHERE i.job_id = $1
ORDER BY i.created_at ASC`, share.JobID)
		if err != nil {
			return err
		}
		defer itemRows.Close()
		for itemRows.Next() {
			var it ExportJobItem
			if err := itemRows.Scan(
				&it.ID, &it.JobID, &it.TenantID, &it.CameraID, &it.CameraName, &it.ServerID, &it.ServerName,
				&it.RemoteExportID, &it.Status, &it.Progress, &it.Error, &it.TotalBytes, &it.TransferredBytes,
				&it.RemotePath, &it.LocalPath, &it.SHA256Hash, &it.CreatedAt, &it.UpdatedAt,
			); err != nil {
				return err
			}
			job.Items = append(job.Items, it)
		}
		return itemRows.Err()
	})
	if err != nil {
		return ExportJob{}, ExportShare{}, err
	}
	return job, share, nil
}
