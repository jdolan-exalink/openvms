package api

import (
	"context"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/api/gen"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/frigate"
	"github.com/jdolan-exalink/openvms/internal/inventory"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

// Errors returned from these handlers are translated by statusFor into the error
// responses the contract declares (400, 403, 404, 409, 502).

func actor(ctx context.Context) (authz.Actor, error) {
	a, ok := ActorFrom(ctx)
	if !ok {
		return authz.Actor{}, errUnauthenticated
	}
	return a, nil
}

func (h *Handlers) GetMe(ctx context.Context, _ gen.GetMeRequestObject) (gen.GetMeResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	u, grants, err := h.Inv.Me(ctx, a)
	if err != nil {
		return nil, err
	}
	method := gen.Token
	if a.SessionID != nil {
		method = gen.Session
	}
	return gen.GetMe200JSONResponse(h.me(ctx, u, grants, method)), nil
}

func (h *Handlers) ListPermissionCatalog(context.Context, gen.ListPermissionCatalogRequestObject) (gen.ListPermissionCatalogResponseObject, error) {
	out := gen.ListPermissionCatalog200JSONResponse{Items: make([]gen.PermissionDefinition, 0, len(authz.Catalog))}
	for _, d := range authz.Catalog {
		out.Items = append(out.Items, gen.PermissionDefinition{Permission: string(d.Permission), Description: d.Description, NarrowestScope: gen.ScopeType(d.Narrowest)})
	}
	return out, nil
}

func toGrant(g db.PermissionGrant) gen.Grant {
	return gen.Grant{
		Id: g.ID, TenantId: g.TenantID, CreatedAt: g.CreatedAt,
		SubjectType: gen.SubjectType(g.SubjectType), SubjectId: g.SubjectID,
		Permission: g.Permission, Effect: gen.Effect(g.Effect),
		ScopeType: gen.ScopeType(g.ScopeType), ScopeId: g.ScopeID,
	}
}

func toGrants(gs []db.PermissionGrant) []gen.Grant {
	out := make([]gen.Grant, 0, len(gs))
	for _, g := range gs {
		out = append(out, toGrant(g))
	}
	return out
}

func (h *Handlers) ListGrants(ctx context.Context, r gen.ListGrantsRequestObject) (gen.ListGrantsResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	f := inventory.GrantFilter{SubjectID: r.Params.SubjectId}
	if r.Params.SubjectType != nil {
		st := string(*r.Params.SubjectType)
		f.SubjectType = &st
	}
	gs, err := h.Inv.ListGrants(ctx, a, f)
	if err != nil {
		return nil, err
	}
	return gen.ListGrants200JSONResponse{Items: toGrants(gs)}, nil
}

func (h *Handlers) CreateGrant(ctx context.Context, r gen.CreateGrantRequestObject) (gen.CreateGrantResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	b := r.Body
	g, err := h.Inv.CreateGrant(ctx, a, inventory.GrantInput{
		SubjectType: string(b.SubjectType), SubjectID: b.SubjectId, Permission: authz.Permission(b.Permission),
		Effect: authz.Effect(b.Effect), ScopeType: authz.ScopeType(b.ScopeType), ScopeID: b.ScopeId,
	})
	if err != nil {
		return nil, err
	}
	return gen.CreateGrant201JSONResponse(toGrant(g)), nil
}

func (h *Handlers) DeleteGrant(ctx context.Context, r gen.DeleteGrantRequestObject) (gen.DeleteGrantResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if err := h.Inv.DeleteGrant(ctx, a, r.GrantId); err != nil {
		return nil, err
	}
	return gen.DeleteGrant204Response{}, nil
}

func toTenant(t db.Tenant) gen.Tenant {
	return gen.Tenant{Id: t.ID, Slug: t.Slug, Name: t.Name, CreatedAt: t.CreatedAt}
}

func (h *Handlers) ListTenants(ctx context.Context, _ gen.ListTenantsRequestObject) (gen.ListTenantsResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	ts, err := h.Inv.ListTenants(ctx, a)
	if err != nil {
		return nil, err
	}
	out := gen.ListTenants200JSONResponse{Items: make([]gen.Tenant, 0, len(ts))}
	for _, t := range ts {
		out.Items = append(out.Items, toTenant(t))
	}
	return out, nil
}

func (h *Handlers) CreateTenant(ctx context.Context, r gen.CreateTenantRequestObject) (gen.CreateTenantResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	t, err := h.Inv.CreateTenant(ctx, a, r.Body.Slug, r.Body.Name)
	if err != nil {
		return nil, err
	}
	return gen.CreateTenant201JSONResponse(toTenant(t)), nil
}

func toSite(s inventory.SiteView) gen.Site {
	return gen.Site{
		Id: s.ID, TenantId: s.TenantID, Name: s.Name, Timezone: s.Timezone, Address: s.Address,
		ServerCount: int(s.ServerCount), CameraCount: s.CameraCount, CreatedAt: s.CreatedAt, UpdatedAt: s.UpdatedAt,
	}
}

func (h *Handlers) ListSites(ctx context.Context, r gen.ListSitesRequestObject) (gen.ListSitesResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	sites, err := h.Inv.ListSites(ctx, a, r.Params.TenantId)
	if err != nil {
		return nil, err
	}
	out := gen.ListSites200JSONResponse{Items: make([]gen.Site, 0, len(sites))}
	for _, s := range sites {
		out.Items = append(out.Items, toSite(s))
	}
	return out, nil
}

func (h *Handlers) CreateSite(ctx context.Context, r gen.CreateSiteRequestObject) (gen.CreateSiteResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	b := r.Body
	s, err := h.Inv.CreateSite(ctx, a, b.TenantId, inventory.SiteInput{Name: &b.Name, Timezone: b.Timezone, Address: b.Address})
	if err != nil {
		return nil, err
	}
	return gen.CreateSite201JSONResponse(toSite(s)), nil
}

func (h *Handlers) GetSite(ctx context.Context, r gen.GetSiteRequestObject) (gen.GetSiteResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	s, err := h.Inv.GetSite(ctx, a, r.SiteId)
	if err != nil {
		return nil, err
	}
	return gen.GetSite200JSONResponse(toSite(s)), nil
}

func (h *Handlers) UpdateSite(ctx context.Context, r gen.UpdateSiteRequestObject) (gen.UpdateSiteResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	b := r.Body
	s, err := h.Inv.UpdateSite(ctx, a, r.SiteId, inventory.SiteInput{Name: b.Name, Timezone: b.Timezone, Address: b.Address})
	if err != nil {
		return nil, err
	}
	return gen.UpdateSite200JSONResponse(toSite(s)), nil
}

func (h *Handlers) DeleteSite(ctx context.Context, r gen.DeleteSiteRequestObject) (gen.DeleteSiteResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if err := h.Inv.DeleteSite(ctx, a, r.SiteId); err != nil {
		return nil, err
	}
	return gen.DeleteSite204Response{}, nil
}

func toCaps(c inventory.ServerView) gen.Capabilities {
	x := c.Caps()
	return gen.Capabilities{
		Review: x.Review, Preview: x.Preview, Exports: x.Exports, Lpr: x.LPR,
		FaceRecognition: x.FaceRecognition, SemanticSearch: x.SemanticSearch, Audio: x.Audio, Ptz: x.PTZ,
	}
}

func toServer(s inventory.ServerView) gen.Server {
	out := gen.Server{
		Id: s.ID, TenantId: s.TenantID, SiteId: s.SiteID, Name: s.Name, BaseUrl: s.BaseUrl, AuthMode: gen.AuthMode(s.AuthMode), Username: s.Username,
		TlsSkipVerify: s.TlsSkipVerify, FrigateVersion: s.FrigateVersion, Capabilities: toCaps(s),
		Status: gen.HealthStatus(s.Status), LastSeenAt: s.LastSeenAt, LastCheckedAt: s.LastCheckedAt,
		LastError: s.LastError, CameraCount: int(s.CameraCount), CreatedAt: s.CreatedAt, UpdatedAt: s.UpdatedAt,
	}
	hs := s.HealthStats()
	if s.LastSeenAt != nil {
		up := int(hs.UptimeSeconds)
		out.UptimeSeconds = &up
	}
	if r := hs.Recordings; r != nil {
		out.Storage = &gen.ServerStorage{TotalMb: float32(r.TotalMB), UsedMb: float32(r.UsedMB), FreeMb: float32(r.FreeMB)}
	}
	return out
}

func (h *Handlers) ListServers(ctx context.Context, r gen.ListServersRequestObject) (gen.ListServersResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	servers, err := h.Inv.ListServers(ctx, a, r.Params.SiteId)
	if err != nil {
		return nil, err
	}
	out := gen.ListServers200JSONResponse{Items: make([]gen.Server, 0, len(servers))}
	for _, s := range servers {
		out.Items = append(out.Items, toServer(s))
	}
	return out, nil
}

func (h *Handlers) ProbeServer(ctx context.Context, r gen.ProbeServerRequestObject) (gen.ProbeServerResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	b := r.Body
	res, err := h.Inv.ProbeServer(ctx, a, b.SiteId, connInput(b.BaseUrl, b.AuthMode, b.Username, b.Password, b.TlsSkipVerify))
	if err != nil {
		return nil, err
	}
	out := gen.ProbeServer200JSONResponse{
		FrigateVersion: res.Version, Adapter: res.Adapter,
		Capabilities: gen.Capabilities{
			Review: res.Capabilities.Review, Preview: res.Capabilities.Preview, Exports: res.Capabilities.Exports,
			Lpr: res.Capabilities.LPR, FaceRecognition: res.Capabilities.FaceRecognition,
			SemanticSearch: res.Capabilities.SemanticSearch, Audio: res.Capabilities.Audio, Ptz: res.Capabilities.PTZ,
		},
		Cameras: make([]gen.DiscoveredCamera, 0, len(res.Cameras)),
	}
	for _, c := range res.Cameras {
		out.Cameras = append(out.Cameras, gen.DiscoveredCamera{RemoteName: c.Name, Enabled: c.Enabled, Zones: c.Zones, Lpr: c.LPR})
	}
	return out, nil
}

func (h *Handlers) CreateServer(ctx context.Context, r gen.CreateServerRequestObject) (gen.CreateServerResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	b := r.Body
	importCams := true
	if b.ImportCameras != nil {
		importCams = *b.ImportCameras
	}
	s, err := h.Inv.CreateServer(ctx, a, inventory.CreateServerInput{
		SiteID: b.SiteId, Name: b.Name, ImportCameras: importCams,
		Conn: connInput(b.BaseUrl, b.AuthMode, b.Username, b.Password, b.TlsSkipVerify),
	})
	if err != nil {
		return nil, err
	}
	return gen.CreateServer201JSONResponse(toServer(s)), nil
}

func (h *Handlers) GetServer(ctx context.Context, r gen.GetServerRequestObject) (gen.GetServerResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	s, err := h.Inv.GetServer(ctx, a, r.ServerId)
	if err != nil {
		return nil, err
	}
	return gen.GetServer200JSONResponse(toServer(s)), nil
}

func (h *Handlers) UpdateServer(ctx context.Context, r gen.UpdateServerRequestObject) (gen.UpdateServerResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	b := r.Body
	s, err := h.Inv.UpdateServer(ctx, a, r.ServerId, inventory.UpdateServerInput{
		Name: b.Name, SiteID: b.SiteId, BaseURL: b.BaseUrl, AuthMode: authMode(b.AuthMode), Username: b.Username, Password: b.Password, TLSSkipVerify: b.TlsSkipVerify,
	})
	if err != nil {
		return nil, err
	}
	return gen.UpdateServer200JSONResponse(toServer(s)), nil
}

func (h *Handlers) DeleteServer(ctx context.Context, r gen.DeleteServerRequestObject) (gen.DeleteServerResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if err := h.Inv.DeleteServer(ctx, a, r.ServerId); err != nil {
		return nil, err
	}
	return gen.DeleteServer204Response{}, nil
}

func (h *Handlers) SyncServer(ctx context.Context, r gen.SyncServerRequestObject) (gen.SyncServerResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	res, err := h.Inv.SyncServer(ctx, a, r.ServerId)
	if err != nil {
		return nil, err
	}
	return gen.SyncServer200JSONResponse{Server: toServer(res.Server), Added: res.Added, Updated: res.Updated, Missing: res.Missing}, nil
}

func toCamera(c db.GetCameraRow) gen.Camera {
	out := gen.Camera{
		Id: c.ID, TenantId: c.TenantID, SiteId: c.SiteID, ServerId: c.ServerID,
		RemoteName: c.RemoteName, DisplayName: c.DisplayName, Enabled: c.Enabled,
		Zones: c.Zones, Lpr: c.Lpr, Status: gen.HealthStatus(c.Status),
		MissingSince: c.MissingSince, GroupIds: c.GroupIds, CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt,
		DefaultLiveQuality: gen.CameraLiveQuality(c.DefaultLiveQuality), Description: c.Description, Location: c.Location,
		Tags: append([]string{}, c.Tags...),
	}
	if c.Fps != nil {
		out.Fps = c.Fps
	}
	return out
}

func (h *Handlers) ListCameras(ctx context.Context, r gen.ListCamerasRequestObject) (gen.ListCamerasResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	p := r.Params
	cams, err := h.Inv.ListCameras(ctx, a, inventory.CameraFilter{SiteID: p.SiteId, ServerID: p.ServerId, GroupID: p.GroupId, Query: p.Q})
	if err != nil {
		return nil, err
	}
	out := gen.ListCameras200JSONResponse{Items: make([]gen.Camera, 0, len(cams))}
	for _, c := range cams {
		out.Items = append(out.Items, toCamera(c))
	}
	return out, nil
}

func (h *Handlers) GetCamera(ctx context.Context, r gen.GetCameraRequestObject) (gen.GetCameraResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	c, err := h.Inv.GetCamera(ctx, a, r.CameraId)
	if err != nil {
		return nil, err
	}
	return gen.GetCamera200JSONResponse(toCamera(c)), nil
}

func cameraUpdate(b *gen.CameraUpdate) inventory.CameraUpdate {
	out := inventory.CameraUpdate{
		DisplayName: b.DisplayName, Enabled: b.Enabled, Description: b.Description, Location: b.Location, Tags: b.Tags,
	}
	if b.DefaultLiveQuality != nil {
		q := string(*b.DefaultLiveQuality)
		out.DefaultLiveQuality = &q
	}
	return out
}

func (h *Handlers) UpdateCamera(ctx context.Context, r gen.UpdateCameraRequestObject) (gen.UpdateCameraResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	c, err := h.Inv.UpdateCamera(ctx, a, r.CameraId, cameraUpdate(r.Body))
	if err != nil {
		return nil, err
	}
	return gen.UpdateCamera200JSONResponse(toCamera(c)), nil
}

func toGroup(g inventory.CameraGroupView) gen.CameraGroup {
	ids := g.CameraIDs
	if ids == nil {
		ids = []uuid.UUID{}
	}
	return gen.CameraGroup{Id: g.ID, TenantId: g.TenantID, Name: g.Name, Description: g.Description, CameraIds: ids, CreatedAt: g.CreatedAt, UpdatedAt: g.UpdatedAt}
}

func (h *Handlers) ListCameraGroups(ctx context.Context, _ gen.ListCameraGroupsRequestObject) (gen.ListCameraGroupsResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	gs, err := h.Inv.ListCameraGroups(ctx, a)
	if err != nil {
		return nil, err
	}
	out := gen.ListCameraGroups200JSONResponse{Items: make([]gen.CameraGroup, 0, len(gs))}
	for _, g := range gs {
		out.Items = append(out.Items, toGroup(g))
	}
	return out, nil
}

func cameraGroupInput(b gen.CameraGroupInput) inventory.CameraGroupInput {
	return inventory.CameraGroupInput{TenantID: b.TenantId, Name: b.Name, Description: deref(b.Description), CameraIDs: b.CameraIds}
}

func (h *Handlers) CreateCameraGroup(ctx context.Context, r gen.CreateCameraGroupRequestObject) (gen.CreateCameraGroupResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	g, err := h.Inv.CreateCameraGroup(ctx, a, cameraGroupInput(*r.Body))
	if err != nil {
		return nil, err
	}
	return gen.CreateCameraGroup201JSONResponse(toGroup(g)), nil
}

func (h *Handlers) GetCameraGroup(ctx context.Context, r gen.GetCameraGroupRequestObject) (gen.GetCameraGroupResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	g, err := h.Inv.GetCameraGroup(ctx, a, r.GroupId)
	if err != nil {
		return nil, err
	}
	return gen.GetCameraGroup200JSONResponse(toGroup(g)), nil
}

func (h *Handlers) ReplaceCameraGroup(ctx context.Context, r gen.ReplaceCameraGroupRequestObject) (gen.ReplaceCameraGroupResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	g, err := h.Inv.ReplaceCameraGroup(ctx, a, r.GroupId, cameraGroupInput(*r.Body))
	if err != nil {
		return nil, err
	}
	return gen.ReplaceCameraGroup200JSONResponse(toGroup(g)), nil
}

func (h *Handlers) DeleteCameraGroup(ctx context.Context, r gen.DeleteCameraGroupRequestObject) (gen.DeleteCameraGroupResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if err := h.Inv.DeleteCameraGroup(ctx, a, r.GroupId); err != nil {
		return nil, err
	}
	return gen.DeleteCameraGroup204Response{}, nil
}

func deref[T any](p *T) T {
	var zero T
	if p == nil {
		return zero
	}
	return *p
}

func authMode(m *gen.AuthMode) *frigate.AuthMode {
	if m == nil {
		return nil
	}
	v := frigate.AuthMode(*m)
	return &v
}

func connInput(baseURL string, mode *gen.AuthMode, username, password *string, skip *bool) inventory.ConnInput {
	in := inventory.ConnInput{BaseURL: baseURL, Username: deref(username), Password: deref(password), TLSSkipVerify: deref(skip)}
	if m := authMode(mode); m != nil {
		in.AuthMode = *m
	}
	return in
}

func toCameraFrigateConfig(v inventory.CameraFrigateConfigView) gen.CameraFrigateConfig {
	tracked := v.TrackedObjects
	if tracked == nil {
		tracked = []string{}
	}
	zones := v.Zones
	if zones == nil {
		zones = []string{}
	}
	return gen.CameraFrigateConfig{
		CameraId:       v.CameraID,
		CameraName:     v.CameraName,
		ServerId:       v.ServerID,
		DetectEnabled:  v.DetectEnabled,
		TrackedObjects: tracked,
		LprEnabled:     v.LPREnabled,
		Zones:          zones,
	}
}

func (h *Handlers) GetCameraFrigateConfig(ctx context.Context, r gen.GetCameraFrigateConfigRequestObject) (gen.GetCameraFrigateConfigResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	cfg, err := h.Inv.GetCameraFrigateConfig(ctx, a, r.CameraId)
	if err != nil {
		return nil, err
	}
	return gen.GetCameraFrigateConfig200JSONResponse(toCameraFrigateConfig(cfg)), nil
}

func (h *Handlers) UpdateCameraFrigateConfig(ctx context.Context, r gen.UpdateCameraFrigateConfigRequestObject) (gen.UpdateCameraFrigateConfigResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	b := r.Body
	var update frigate.CameraFrigateConfigUpdate
	if b != nil {
		update.DetectEnabled = b.DetectEnabled
		update.LPREnabled = b.LprEnabled
		if b.TrackedObjects != nil {
			update.TrackedObjects = *b.TrackedObjects
		}
	}
	cfg, err := h.Inv.UpdateCameraFrigateConfig(ctx, a, r.CameraId, update)
	if err != nil {
		return nil, err
	}
	return gen.UpdateCameraFrigateConfig200JSONResponse(toCameraFrigateConfig(cfg)), nil
}

func (h *Handlers) RestartServer(ctx context.Context, r gen.RestartServerRequestObject) (gen.RestartServerResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if err := h.Inv.RestartServer(ctx, a, r.ServerId); err != nil {
		return nil, err
	}
	return gen.RestartServer200JSONResponse{Success: true}, nil
}

