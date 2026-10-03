package inventory

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/access"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

var slugRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,62}$`)

func (s *Service) ListTenants(ctx context.Context, actor authz.Actor) ([]db.Tenant, error) {
	var out []db.Tenant
	err := s.tx(ctx, actor, func(q *db.Queries, c *access.Checker) error {
		all, err := q.ListTenants(ctx, actor.TenantID)
		if err != nil {
			return err
		}
		for _, t := range all {
			if !actor.IsPlatform() || c.Can(authz.TenantManage, access.Tenant(t.ID)) {
				out = append(out, t)
			}
		}
		return nil
	})
	return out, err
}

func (s *Service) CreateTenant(ctx context.Context, actor authz.Actor, slug, name string) (db.Tenant, error) {
	var t db.Tenant
	slug, name = strings.TrimSpace(slug), strings.TrimSpace(name)
	if !slugRe.MatchString(slug) || name == "" {
		return t, invalid("slug must be lowercase letters, digits and dashes, and name is required")
	}
	err := s.tx(ctx, actor, func(q *db.Queries, c *access.Checker) error {
		if err := c.Require(authz.TenantManage, authz.Resource{Kind: authz.ScopePlatform}); err != nil {
			return err
		}
		var err error
		t, err = q.CreateTenant(ctx, db.CreateTenantParams{Slug: slug, Name: name})
		if err != nil {
			return store.Classify(err)
		}
		return audit(ctx, q, actor, &t.ID, ActionTenantCreated, "tenant", t.ID, map[string]any{"slug": slug, "name": name})
	})
	return t, err
}

// SiteView is a site plus what the caller can see inside it.
type SiteView struct {
	db.GetSiteRow
	CameraCount int
}

// visibleSites returns the sites the actor can list: those covered by sites.view plus
// those containing at least one camera covered by cameras.view, with camera counts.
func visibleSites(ctx context.Context, q *db.Queries, c *access.Checker) (map[uuid.UUID]int, error) {
	siteIDs, err := c.SiteIDs(ctx, authz.SitesView)
	if err != nil {
		return nil, err
	}
	camIDs, err := c.CameraIDs(ctx, authz.CamerasView)
	if err != nil {
		return nil, err
	}
	counts := make(map[uuid.UUID]int, len(siteIDs))
	for _, id := range siteIDs {
		counts[id] = 0
	}
	cams, err := q.ListCameras(ctx, db.ListCamerasParams{Ids: camIDs})
	if err != nil {
		return nil, err
	}
	for _, cam := range cams {
		counts[cam.SiteID]++
	}
	return counts, nil
}

func (s *Service) ListSites(ctx context.Context, actor authz.Actor, tenantID *uuid.UUID) ([]SiteView, error) {
	var out []SiteView
	err := s.tx(ctx, actor, func(q *db.Queries, c *access.Checker) error {
		counts, err := visibleSites(ctx, q, c)
		if err != nil {
			return err
		}
		ids := make([]uuid.UUID, 0, len(counts))
		for id := range counts {
			ids = append(ids, id)
		}
		rows, err := q.ListSites(ctx, db.ListSitesParams{Ids: ids, TenantID: tenantID})
		if err != nil {
			return err
		}
		for _, r := range rows {
			out = append(out, SiteView{GetSiteRow: db.GetSiteRow(r), CameraCount: counts[r.ID]})
		}
		return nil
	})
	return out, err
}

func (s *Service) GetSite(ctx context.Context, actor authz.Actor, id uuid.UUID) (SiteView, error) {
	var out SiteView
	err := s.tx(ctx, actor, func(q *db.Queries, c *access.Checker) error {
		site, err := q.GetSite(ctx, id)
		if err != nil {
			return notFoundOr(err)
		}
		counts, err := visibleSites(ctx, q, c)
		if err != nil {
			return err
		}
		n, ok := counts[id]
		if !ok {
			return access.ErrForbidden
		}
		out = SiteView{GetSiteRow: site, CameraCount: n}
		return nil
	})
	return out, err
}

type SiteInput struct {
	Name     *string
	Timezone *string
	Address  *string
}

func validateSite(in SiteInput) error {
	if in.Name != nil && strings.TrimSpace(*in.Name) == "" {
		return invalid("name cannot be empty")
	}
	if in.Timezone != nil {
		if _, err := time.LoadLocation(*in.Timezone); err != nil || *in.Timezone == "" {
			return invalid("unknown timezone %q", *in.Timezone)
		}
	}
	return nil
}

func (s *Service) CreateSite(ctx context.Context, actor authz.Actor, tenantID uuid.UUID, in SiteInput) (SiteView, error) {
	if in.Name == nil {
		return SiteView{}, invalid("name is required")
	}
	if in.Timezone == nil {
		in.Timezone = ptr("America/Argentina/Cordoba")
	}
	if in.Address == nil {
		in.Address = ptr("")
	}
	if err := validateSite(in); err != nil {
		return SiteView{}, err
	}
	var id uuid.UUID
	err := s.tx(ctx, actor, func(q *db.Queries, c *access.Checker) error {
		if !actor.IsPlatform() && *actor.TenantID != tenantID {
			return store.ErrNotFound
		}
		if _, err := q.GetTenant(ctx, tenantID); err != nil {
			return notFoundOr(err)
		}
		if err := c.Require(authz.SitesManage, access.Tenant(tenantID)); err != nil {
			return err
		}
		var err error
		id, err = q.CreateSite(ctx, db.CreateSiteParams{TenantID: tenantID, Name: strings.TrimSpace(*in.Name), Timezone: *in.Timezone, Address: *in.Address})
		if err != nil {
			return store.Classify(err)
		}
		return audit(ctx, q, actor, &tenantID, ActionSiteCreated, "site", id, map[string]any{"name": *in.Name})
	})
	if err != nil {
		return SiteView{}, err
	}
	return s.GetSite(ctx, actor, id)
}

func (s *Service) UpdateSite(ctx context.Context, actor authz.Actor, id uuid.UUID, in SiteInput) (SiteView, error) {
	if err := validateSite(in); err != nil {
		return SiteView{}, err
	}
	err := s.tx(ctx, actor, func(q *db.Queries, c *access.Checker) error {
		site, err := q.GetSite(ctx, id)
		if err != nil {
			return notFoundOr(err)
		}
		if err := c.Require(authz.SitesManage, access.Site(site.TenantID, site.ID)); err != nil {
			return err
		}
		if in.Name != nil {
			in.Name = ptr(strings.TrimSpace(*in.Name))
		}
		if err := q.UpdateSite(ctx, db.UpdateSiteParams{ID: id, Name: in.Name, Timezone: in.Timezone, Address: in.Address}); err != nil {
			return store.Classify(err)
		}
		return audit(ctx, q, actor, &site.TenantID, ActionSiteUpdated, "site", id, nil)
	})
	if err != nil {
		return SiteView{}, err
	}
	return s.GetSite(ctx, actor, id)
}

// ErrSiteNotEmpty is returned when deleting a site that still has servers.
var ErrSiteNotEmpty = errors.New("este sitio todavía tiene servidores; movelos a otro sitio antes de eliminarlo")

// ErrLastSite is returned when deleting the tenant's only remaining site.
var ErrLastSite = errors.New("siempre tiene que quedar al menos un sitio")

func (s *Service) DeleteSite(ctx context.Context, actor authz.Actor, id uuid.UUID) error {
	return s.tx(ctx, actor, func(q *db.Queries, c *access.Checker) error {
		site, err := q.GetSite(ctx, id)
		if err != nil {
			return notFoundOr(err)
		}
		if err := c.Require(authz.SitesManage, access.Site(site.TenantID, site.ID)); err != nil {
			return err
		}
		n, err := q.CountTenantSites(ctx, site.TenantID)
		if err != nil {
			return err
		}
		if n <= 1 {
			return ErrLastSite
		}
		if site.ServerCount > 0 {
			return ErrSiteNotEmpty
		}
		if err := q.SoftDeleteSite(ctx, db.SoftDeleteSiteParams{ID: id, DeletedBy: &actor.UserID}); err != nil {
			return err
		}
		return audit(ctx, q, actor, &site.TenantID, ActionSiteRemoved, "site", id, map[string]any{"name": site.Name})
	})
}
