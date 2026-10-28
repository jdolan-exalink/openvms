package inventory

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/provisionnvr"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

// nvrLockedRetry is how long the poller leaves a recorder alone after it reported the account locked.
const nvrLockedRetry = 10 * time.Minute

// nvrSession is the poller's logged-in client for one recorder. It is replaced when the stored
// connection changes, and it remembers a rejected password so the poller stops sending it: every wrong
// login counts toward the recorder's own lockout.
type nvrSession struct {
	sealed  []byte
	base    string
	client  *provisionnvr.Client
	blocked bool
	// retryAt is when a locked account may be tried again; zero for a rejected password, which is not
	// tried again until the stored password changes.
	retryAt time.Time
}

func (p *HealthPoller) nvrSessionFor(srv db.FrigateServer) (*nvrSession, error) {
	p.nvrMu.Lock()
	defer p.nvrMu.Unlock()
	if p.nvr == nil {
		p.nvr = map[uuid.UUID]*nvrSession{}
	}
	if s, ok := p.nvr[srv.ID]; ok && bytes.Equal(s.sealed, srv.PasswordSealed) && s.base == srv.BaseUrl {
		return s, nil
	}
	pw, err := p.Svc.Sealer.Open(srv.PasswordSealed, srv.ID[:])
	if err != nil {
		return nil, err
	}
	client, err := provisionnvr.NewClient(srv.BaseUrl, provisionnvr.NewCredentials(srv.Username, string(pw)), provisionnvr.Config{})
	if err != nil {
		return nil, err
	}
	s := &nvrSession{sealed: srv.PasswordSealed, base: srv.BaseUrl, client: client}
	p.nvr[srv.ID] = s
	return s, nil
}

// dropNVRSession makes the next poll build a fresh client (and log in again).
func (p *HealthPoller) dropNVRSession(id uuid.UUID) {
	p.nvrMu.Lock()
	delete(p.nvr, id)
	p.nvrMu.Unlock()
}

// checkNVR polls one Provision recorder: login, per-channel state and disk state, read-only. It maps what
// the recorder says onto the same server and camera statuses Frigate servers use.
func (p *HealthPoller) checkNVR(ctx context.Context, srv db.FrigateServer) {
	sess, err := p.nvrSessionFor(srv)
	if err != nil {
		p.Svc.Log.ErrorContext(ctx, "health: recorder connection", "server_id", srv.ID, "error", err)
		return
	}
	p.nvrMu.Lock()
	skip := sess.blocked && (sess.retryAt.IsZero() || time.Now().Before(sess.retryAt))
	if sess.blocked && !skip {
		sess.blocked = false
	}
	p.nvrMu.Unlock()
	if skip {
		return
	}

	status, errMsg := "online", ""
	statuses, err := sess.client.ChannelStatuses(ctx)
	if err != nil {
		status, errMsg = "offline", nvrErrorMessage(err)
		var perr *provisionnvr.Error
		if errors.As(err, &perr) && (perr.Class == provisionnvr.ClassAuthFailed || perr.Class == provisionnvr.ClassLocked) {
			p.nvrMu.Lock()
			sess.blocked = true
			if perr.Class == provisionnvr.ClassLocked {
				sess.retryAt = time.Now().Add(nvrLockedRetry)
			}
			p.nvrMu.Unlock()
			p.Svc.Log.WarnContext(ctx, "health: recorder rejected the stored credentials; not retrying", "server_id", srv.ID, "class", perr.Class)
		} else {
			p.dropNVRSession(srv.ID)
		}
	}
	var storage provisionnvr.Storage
	var storageKnown bool
	if status == "online" {
		if st, serr := sess.client.Storage(ctx); serr == nil {
			storage, storageKnown = st, true
		}
	}
	byChannel := map[string]provisionnvr.ChannelStatus{}
	for _, c := range statuses {
		byChannel[c.ID] = c
	}

	var camChanges []CameraStatusChange
	now := time.Now().UTC()
	err = p.Svc.Store.Tx(ctx, store.AllTenants, func(q *db.Queries) error {
		cams, err := q.ListServerCameras(ctx, srv.ID)
		if err != nil {
			return err
		}
		if status == "offline" {
			if srv.Status != "offline" {
				for _, cam := range cams {
					if cam.Status != "unknown" {
						camChanges = append(camChanges, CameraStatusChange{CameraID: cam.ID, ServerID: srv.ID, TenantID: srv.TenantID, SiteID: srv.SiteID, From: cam.Status, To: "unknown", At: now})
					}
				}
			}
			if err := q.SetServerCamerasStatus(ctx, db.SetServerCamerasStatusParams{ServerID: srv.ID, Status: "unknown"}); err != nil {
				return err
			}
			return q.UpdateServerHealth(ctx, db.UpdateServerHealthParams{ID: srv.ID, Status: status, Reachable: false, LastError: errMsg, Stats: srv.Stats})
		}
		var offline []string
		for _, cam := range cams {
			camStatus := "unknown"
			if cam.Enabled {
				camStatus = "offline"
				if ch, ok := byChannel[cam.RemoteName]; ok && ch.Online {
					camStatus = "online"
				}
			}
			if camStatus == "offline" && cam.MissingSince == nil {
				offline = append(offline, cam.DisplayName)
			}
			if err := q.UpdateCameraHealth(ctx, db.UpdateCameraHealthParams{ServerID: srv.ID, RemoteName: cam.RemoteName, Status: camStatus}); err != nil {
				return err
			}
			if camStatus != cam.Status {
				camChanges = append(camChanges, CameraStatusChange{CameraID: cam.ID, ServerID: srv.ID, TenantID: srv.TenantID, SiteID: srv.SiteID, From: cam.Status, To: camStatus, At: now})
			}
		}
		var health HealthStats
		if len(offline) > 0 {
			health.Problems = append(health.Problems, HealthProblem{Code: ProblemCamerasOffline, Cameras: offline})
		}
		if storageKnown && hasBadDisk(storage) {
			health.Problems = append(health.Problems, HealthProblem{Code: ProblemRecorderDisk})
		}
		if len(health.Problems) > 0 {
			status = "degraded"
		}
		raw, _ := json.Marshal(health)
		return q.UpdateServerHealth(ctx, db.UpdateServerHealthParams{ID: srv.ID, Status: status, Reachable: true, LastError: "", Stats: raw})
	})
	if err != nil {
		p.Svc.Log.ErrorContext(ctx, "health: store recorder result", "server_id", srv.ID, "error", err)
		return
	}
	if status != srv.Status && p.OnChange != nil {
		p.OnChange(ctx, StatusChange{ServerID: srv.ID, TenantID: srv.TenantID, SiteID: srv.SiteID, From: srv.Status, To: status, Error: errMsg, At: now})
	}
	if p.OnCameraChange != nil {
		for _, c := range camChanges {
			p.OnCameraChange(ctx, c)
		}
	}
}

func hasBadDisk(s provisionnvr.Storage) bool {
	for _, d := range s.Disks {
		if d.Status == "bad" {
			return true
		}
	}
	return false
}

// nvrErrorMessage is the text stored in last_error for a recorder that failed. A rejected password gets
// a fixed sentence; nothing the recorder said is copied into it.
func nvrErrorMessage(err error) string {
	var perr *provisionnvr.Error
	if !errors.As(err, &perr) {
		return "offline: could not read the recorder"
	}
	switch perr.Class {
	case provisionnvr.ClassAuthFailed:
		return "auth_failed: the recorder rejected the stored username or password"
	case provisionnvr.ClassLocked:
		return "auth_failed: the recorder locked the account after failed logins"
	case provisionnvr.ClassUnsupported:
		return "unsupported: the device did not answer like a supported Provision-ISR recorder"
	default:
		return "offline: could not reach the recorder"
	}
}
