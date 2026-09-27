// Package media is the media gateway (PRD §47, §52): it relays live video, snapshots,
// recordings (HLS) and export downloads from the origin Frigate to the browser, after
// checking the caller's permission on the camera. Frigate credentials never leave the
// VMS and browsers never talk to a Frigate directly. It also keeps saved views and
// exports, whose metadata lives in the VMS.
package media

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/access"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/inventory"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

type Service struct {
	Store    *store.Store
	Adapters *inventory.Adapters
	Log      *slog.Logger

	mu    sync.Mutex
	cache map[cacheKey]cacheEntry
}

// Media requests arrive in bursts (an HLS player asks for a segment every few seconds per
// camera), so authorization results are cached briefly. A revoked grant stops working
// within cacheTTL.
const cacheTTL = 15 * time.Second

type cacheKey struct {
	user   uuid.UUID
	camera uuid.UUID
	perm   authz.Permission
}

type cacheEntry struct {
	cam     Camera
	expires time.Time
}

// Camera is what the gateway needs to reach a camera in its Frigate.
type Camera struct {
	ID         uuid.UUID
	TenantID   uuid.UUID
	SiteID     uuid.UUID
	RemoteName string
	LiveStream string
	HQStream   string
	Server     db.FrigateServer
}

func (s *Service) tx(ctx context.Context, actor authz.Actor, fn func(q *db.Queries, c *access.Checker) error) error {
	return s.Store.Tx(ctx, store.ScopeFor(actor), func(q *db.Queries) error {
		c, err := access.Load(ctx, q, actor)
		if err != nil {
			return err
		}
		return fn(q, c)
	})
}

// Authorize loads the camera and checks p on it. Cameras of other tenants answer
// store.ErrNotFound; a missing permission answers access.ErrForbidden.
func (s *Service) Authorize(ctx context.Context, actor authz.Actor, cameraID uuid.UUID, p authz.Permission) (Camera, error) {
	key := cacheKey{actor.UserID, cameraID, p}
	s.mu.Lock()
	if s.cache == nil {
		s.cache = map[cacheKey]cacheEntry{}
	}
	if e, ok := s.cache[key]; ok && time.Now().Before(e.expires) {
		s.mu.Unlock()
		return e.cam, nil
	}
	s.mu.Unlock()

	var out Camera
	err := s.tx(ctx, actor, func(q *db.Queries, c *access.Checker) error {
		cam, err := q.GetCamera(ctx, cameraID)
		if err != nil {
			return store.Classify(err)
		}
		if err := c.Require(p, access.Camera(cam.TenantID, cam.SiteID, cam.ServerID, cam.ID, cam.GroupIds)); err != nil {
			return err
		}
		srv, err := q.GetServerRow(ctx, cam.ServerID)
		if err != nil {
			return store.Classify(err)
		}
		out = Camera{ID: cam.ID, TenantID: cam.TenantID, SiteID: cam.SiteID, RemoteName: cam.RemoteName,
			LiveStream: cam.LiveStream, HQStream: cam.HqStream, Server: srv}
		return nil
	})
	if err != nil {
		return Camera{}, err
	}
	s.mu.Lock()
	if len(s.cache) > 10000 {
		s.cache = map[cacheKey]cacheEntry{}
	}
	s.cache[key] = cacheEntry{cam: out, expires: time.Now().Add(cacheTTL)}
	s.mu.Unlock()
	return out, nil
}
