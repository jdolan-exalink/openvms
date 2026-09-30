package authz

// Permission namespace from PRD §29, plus sites.view / sites.manage, which the
// PRD leaves implicit and inventory screens need.
const (
	LiveView  Permission = "live.view"
	LiveAudio Permission = "live.audio"
	LiveTalk  Permission = "live.talk"
	LivePTZ   Permission = "live.ptz"

	RecordingsView Permission = "recordings.view"
	RecordingsSeek Permission = "recordings.seek"

	EventsView   Permission = "events.view"
	EventsSearch Permission = "events.search"
	EventsReview Permission = "events.review"

	AlarmsView   Permission = "alarms.view"
	AlarmsManage Permission = "alarms.manage"

	SnapshotsView     Permission = "snapshots.view"
	SnapshotsDownload Permission = "snapshots.download"

	LPRView   Permission = "lpr.view"
	LPRSearch Permission = "lpr.search"

	ExportsCreate   Permission = "exports.create"
	ExportsDownload Permission = "exports.download"
	ExportsDelete   Permission = "exports.delete"

	ViewsCreatePrivate Permission = "views.create_private"
	ViewsCreateShared  Permission = "views.create_shared"
	ViewsManageShared  Permission = "views.manage_shared"

	CasesView   Permission = "cases.view"
	CasesCreate Permission = "cases.create"
	CasesUpdate Permission = "cases.update"
	CasesExport Permission = "cases.export"

	SitesView   Permission = "sites.view"
	SitesManage Permission = "sites.manage"

	CamerasView   Permission = "cameras.view"
	CamerasManage Permission = "cameras.manage"

	ServersView    Permission = "servers.view"
	ServersManage  Permission = "servers.manage"
	ServersRestart Permission = "servers.restart"
	ServersConfig  Permission = "servers.config"
	// ServersConfigSecrets reads and edits Frigate credentials (stream URLs, ONVIF user and
	// password), the raw config.yml and the stored revisions that contain them.
	ServersConfigSecrets Permission = "servers.config.secrets" //nolint:gosec // permission name, not a credential

	UsersView   Permission = "users.view"
	UsersManage Permission = "users.manage"

	GroupsView   Permission = "groups.view"
	GroupsManage Permission = "groups.manage"

	PermissionsManage Permission = "permissions.manage"

	AuditView Permission = "audit.view"

	HealthView Permission = "health.view"

	NotificationsManage Permission = "notifications.manage"

	MapsView       Permission = "maps.view"
	MapsEdit       Permission = "maps.edit"
	MapsCreateZone Permission = "maps.create_zone"
	MapsEditDevice Permission = "maps.edit_device"

	TenantManage Permission = "tenant.manage"
)

// Definition describes a permission and the narrowest scope it can be granted at.
type Definition struct {
	Permission  Permission
	Description string
	// Narrowest is the finest-grained scope that makes sense for this permission.
	// Grants below it are rejected (e.g. users.manage on a single camera).
	Narrowest ScopeType
}

var Catalog = []Definition{
	{LiveView, "Ver video en vivo", ScopeCamera},
	{LiveAudio, "Escuchar audio en vivo", ScopeCamera},
	{LiveTalk, "Hablar por la cámara (two-way audio)", ScopeCamera},
	{LivePTZ, "Mover cámaras PTZ", ScopeCamera},
	{RecordingsView, "Ver grabaciones", ScopeCamera},
	{RecordingsSeek, "Navegar la línea de tiempo de grabaciones", ScopeCamera},
	{EventsView, "Ver eventos", ScopeCamera},
	{EventsSearch, "Buscar eventos", ScopeCamera},
	{EventsReview, "Marcar eventos como revisados", ScopeCamera},
	{AlarmsView, "Ver alarmas", ScopeCamera},
	{AlarmsManage, "Reconocer, asignar y resolver alarmas", ScopeCamera},
	{SnapshotsView, "Ver snapshots", ScopeCamera},
	{SnapshotsDownload, "Descargar snapshots", ScopeCamera},
	{LPRView, "Ver lecturas de patentes", ScopeCamera},
	{LPRSearch, "Buscar patentes", ScopeCamera},
	{ExportsCreate, "Crear exportaciones de video", ScopeCamera},
	{ExportsDownload, "Descargar exportaciones", ScopeCamera},
	{ExportsDelete, "Borrar exportaciones", ScopeCamera},
	{ViewsCreatePrivate, "Crear vistas privadas", ScopeTenant},
	{ViewsCreateShared, "Crear vistas compartidas", ScopeTenant},
	{ViewsManageShared, "Administrar vistas compartidas", ScopeTenant},
	{CasesView, "Ver casos", ScopeTenant},
	{CasesCreate, "Crear casos", ScopeTenant},
	{CasesUpdate, "Editar casos", ScopeTenant},
	{CasesExport, "Exportar casos", ScopeTenant},
	{SitesView, "Ver sitios", ScopeSite},
	{SitesManage, "Administrar sitios", ScopeSite},
	{CamerasView, "Ver inventario de cámaras", ScopeCamera},
	{CamerasManage, "Administrar cámaras y grupos de cámaras", ScopeCamera},
	{ServersView, "Ver servidores Frigate", ScopeServer},
	{ServersManage, "Registrar y administrar servidores Frigate", ScopeServer},
	{ServersRestart, "Reiniciar servidores Frigate", ScopeServer},
	{ServersConfig, "Editar configuración de Frigate", ScopeServer},
	{ServersConfigSecrets, "Ver y editar credenciales, URLs de streams y el YAML completo de Frigate", ScopeServer},
	{UsersView, "Ver usuarios", ScopeTenant},
	{UsersManage, "Administrar usuarios", ScopeTenant},
	{GroupsView, "Ver grupos de usuarios", ScopeTenant},
	{GroupsManage, "Administrar grupos de usuarios", ScopeTenant},
	{PermissionsManage, "Otorgar y revocar permisos", ScopeCamera},
	{AuditView, "Ver auditoría", ScopeTenant},
	{HealthView, "Ver estado de salud", ScopeCamera},
	{NotificationsManage, "Administrar notificaciones", ScopeTenant},
	{MapsView, "Ver mapas", ScopeSite},
	{MapsEdit, "Administrar estructura del mapa (sitios, edificios, pisos)", ScopeSite},
	{MapsCreateZone, "Crear y editar zonas en mapas", ScopeSite},
	{MapsEditDevice, "Ubicar y configurar dispositivos en mapas", ScopeSite},
	{TenantManage, "Administrar el tenant", ScopeTenant},
}

var scopeDepth = map[ScopeType]int{
	ScopePlatform:    0,
	ScopeTenant:      1,
	ScopeSite:        2,
	ScopeCameraGroup: 2,
	ScopeServer:      3,
	ScopeCamera:      4,
}

// Lookup returns the definition of p, or false if p is not a known permission.
func Lookup(p Permission) (Definition, bool) {
	for _, d := range Catalog {
		if d.Permission == p {
			return d, true
		}
	}
	return Definition{}, false
}

// Grantable reports whether p may be granted at scope type t.
func Grantable(p Permission, t ScopeType) bool {
	d, ok := Lookup(p)
	if !ok {
		return false
	}
	depth, ok := scopeDepth[t]
	if !ok {
		return false
	}
	if t == ScopeCameraGroup {
		// Camera groups only make sense for permissions that reach individual cameras.
		return d.Narrowest == ScopeCamera
	}
	return depth <= scopeDepth[d.Narrowest]
}
