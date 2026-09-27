# PRD Maestro — Federated Frigate VMS

**Versión:** 1.0  
**Estado:** Ready for Development  
**Tipo de producto:** VMS federado multi-tenant / multi-site para Frigate  
**Objetivo:** crear una plataforma central de operación, búsqueda, seguridad y administración para múltiples servidores Frigate autónomos.

---

# 1. Visión del producto

Construir una plataforma VMS empresarial capaz de federar múltiples instalaciones Frigate independientes sin modificar ni forkear Frigate.

Cada servidor Frigate deberá continuar siendo completamente autónomo:

- Graba localmente.
- Mantiene su propia retención.
- Detecta objetos localmente.
- Ejecuta IA localmente.
- Ejecuta LPR/Face Recognition localmente cuando corresponda.
- Mantiene su interfaz Frigate original.
- Continúa operando ante pérdida de Internet, VPN o servidor central.
- Puede actualizarse independientemente.
- No depende del VMS central para realizar sus funciones esenciales.

El VMS central será una capa superior de:

- Multi-tenancy.
- Multi-site.
- Administración de servidores.
- Administración de cámaras.
- Usuarios.
- Administradores delegados.
- Roles.
- Grupos.
- Permisos granulares.
- Live View.
- Vistas multi-servidor.
- Videowall.
- Índice central de eventos.
- Búsquedas federadas.
- LPR global.
- Gestión de evidencia.
- Casos.
- Exportaciones.
- Auditoría.
- Health monitoring.
- Alertas.
- Estadísticas.
- Integraciones.

La arquitectura debe inspirarse en la experiencia operacional de sistemas como Avigilon, Nx Witness o Milestone, pero utilizando Frigate como motor NVR/AI distribuido.

---

# 2. Principio fundamental

El sistema tendrá dos planos claramente separados:

```text
CONTROL PLANE CENTRAL
──────────────────────────────────

Usuarios
Roles
Permisos
Sitios
Servidores
Cámaras
Vistas
Índice de eventos
Búsquedas
LPR global
Casos
Auditoría
Configuración VMS
Alertas
Health
Metadata


DATA PLANE DISTRIBUIDO
──────────────────────────────────

Frigate Server 1
    Grabaciones
    Streams
    IA
    Objetos
    LPR
    Snapshots
    Review

Frigate Server 2
    Grabaciones
    Streams
    IA
    Objetos
    LPR
    Snapshots
    Review

Frigate Server N
    ...
```

El servidor central **NO será un NVR secundario**.

Las grabaciones continuas permanecerán en cada Frigate.

---

# 3. Objetivos

## 3.1 Objetivos principales

El sistema debe permitir administrar desde una única interfaz:

- Decenas o cientos de servidores Frigate.
- Cientos o miles de cámaras.
- Diferentes organizaciones.
- Diferentes sitios.
- Diferentes operadores.
- Diferentes niveles de permisos.

Debe proporcionar una experiencia en la cual el usuario no necesite saber en qué servidor Frigate se encuentra una cámara.

El operador interactúa con:

```text
Organización
    ↓
Sitio
    ↓
Grupo de cámaras
    ↓
Cámara
```

La ubicación técnica de la cámara dentro de un determinado Frigate debe ser transparente.

---

# 4. No objetivos

En las primeras versiones NO se pretende:

- Reemplazar el motor de grabación de Frigate.
- Reemplazar la detección de objetos de Frigate.
- Modificar internamente Frigate.
- Mantener un fork propio de Frigate.
- Centralizar grabaciones continuas.
- Transcodificar permanentemente video en el servidor central.
- Duplicar toda la base de datos interna de Frigate.
- Hacer depender la operación local del servidor central.

---

# 5. Arquitectura organizacional

La jerarquía principal será:

```text
PLATFORM
│
├── TENANT
│   │
│   ├── SITE
│   │   │
│   │   ├── FRIGATE SERVER
│   │   │   │
│   │   │   ├── CAMERA
│   │   │   ├── CAMERA
│   │   │   └── CAMERA
│   │   │
│   │   └── FRIGATE SERVER
│   │
│   └── SITE
│
└── TENANT
```

Ejemplo:

```text
DOLAN CCTV
│
├── Municipalidad Helvecia
│   │
│   ├── Centro Monitoreo
│   │   ├── Frigate-H01
│   │   └── Frigate-H02
│   │
│   └── Cementerio
│       └── Frigate-H03
│
└── Municipalidad Cayastá
    │
    └── Centro Monitoreo
        ├── Frigate-C01
        └── Frigate-C02
```

---

# 6. Identidad global de recursos

Todos los objetos deben recibir UUID internos.

Nunca utilizar como clave primaria central:

- Nombre de cámara Frigate.
- Hostname.
- Dirección IP.
- Nombre del sitio.

Ejemplo:

```text
tenant_uuid
site_uuid
server_uuid
camera_uuid
event_uuid
user_uuid
group_uuid
view_uuid
case_uuid
```

Una cámara denominada:

```text
entrada
```

puede existir simultáneamente en veinte Frigate diferentes.

Internamente deberá identificarse como:

```text
camera_uuid = 19da...
server_uuid = a07f...
remote_camera_id = entrada
```

---

# 7. Stack tecnológico

## 7.1 Frontend

### Lenguaje

TypeScript.

### Framework

React.

### Build

Vite.

### UI

Tailwind CSS + shadcn/ui/Radix primitives.

### Estado remoto

TanStack Query.

### Estado local

Zustand.

### Routing

TanStack Router o React Router.

### Tablas

TanStack Table.

### Formularios

React Hook Form + Zod.

### Gráficos

Apache ECharts.

### Tiempo real

WebSocket.

Fallback:

Server-Sent Events.

### Características

- SPA.
- Responsive.
- Desktop first para centro de monitoreo.
- Usable desde tablet.
- Usable desde móvil para búsquedas/eventos.
- PWA opcional.
- Dark mode prioritario.
- Light mode disponible.

No se requiere SSR.

---

# 8. Backend central

## Tecnología principal

**Go**.

Motivos:

- Excelente concurrencia.
- Bajo consumo de RAM.
- Muy adecuado para conexiones persistentes.
- WebSocket.
- Streaming proxy.
- Agentes.
- MQTT.
- Procesamiento concurrente.
- Fácil distribución mediante binarios.
- Excelente para servicios de infraestructura.
- Permite compartir librerías entre Central y Edge Agent.

Arquitectura inicial:

**Modular Monolith**.

NO comenzar con 20 microservicios.

El backend se dividirá internamente en módulos claramente aislados.

```text
core
auth
rbac
tenants
sites
servers
cameras
events
search
lpr
views
media
cases
exports
audit
notifications
health
integrations
```

Estos módulos podrán posteriormente extraerse como servicios independientes.

---

# 9. Librerías backend recomendadas

HTTP:

```text
net/http
+
chi
```

Base de datos:

```text
pgx
sqlc
```

Validación:

```text
go-playground/validator
```

Migraciones:

```text
Atlas
```

o alternativamente:

```text
goose
```

Logging:

```text
slog
```

Tracing:

```text
OpenTelemetry
```

API:

```text
REST + OpenAPI 3
```

Tiempo real:

```text
WebSocket
```

No usar GraphQL inicialmente.

---

# 10. Base de datos

## Principal

PostgreSQL.

Debe almacenar:

- Tenants.
- Sitios.
- Servidores.
- Cámaras.
- Usuarios.
- Grupos.
- Permisos.
- Vistas.
- Eventos normalizados.
- LPR.
- Casos.
- Auditoría.
- Configuración.
- Health.
- Notificaciones.

Extensiones recomendadas:

```text
pg_trgm
uuid
```

Opcional:

```text
pgvector
```

para futuras búsquedas semánticas.

---

# 11. Cache y estado temporal

Utilizar:

**Valkey**

para:

- Cache.
- Rate limits.
- Tokens temporales.
- Estado online.
- Sesiones distribuidas cuando corresponda.
- Locks.
- Datos efímeros.
- Presence.
- Invalidación de permisos.

---

# 12. Event Bus

Utilizar:

**NATS JetStream**

como bus interno.

Ejemplo:

```text
frigate.event.received
frigate.event.updated
frigate.event.completed

camera.online
camera.offline

server.online
server.offline

lpr.detected

user.login

export.requested
export.completed
```

Ventajas:

- Durable consumers.
- Replay.
- Backpressure.
- Bajo consumo.
- Excelente integración con Go.
- Facilita separar workers posteriormente.

---

# 13. Object Storage

Utilizar interfaz compatible con S3.

Implementación on-premise recomendada:

```text
MinIO
```

o cualquier almacenamiento S3 compatible.

Almacenar centralmente:

- Thumbnails.
- Previews opcionales.
- Snapshots seleccionados.
- Evidencia exportada.
- Archivos asociados a casos.

No almacenar por defecto:

- Grabación continua.

---

# 14. Frigate Edge Agent

Crear un componente propio:

```text
frigate-edge-agent
```

Lenguaje:

```text
Go
```

El Agent será recomendado pero Frigate podrá registrarse inicialmente sin él.

Cada Agent estará asociado exactamente a un Frigate Server.

Arquitectura:

```text
┌─────────────────────────┐
│        FRIGATE          │
│                         │
│ API                     │
│ MQTT                    │
│ go2rtc                  │
│ Recordings              │
└────────────┬────────────┘
             │ localhost/LAN
             ▼
┌─────────────────────────┐
│   FRIGATE EDGE AGENT    │
│                         │
│ MQTT Client             │
│ Frigate API Client      │
│ Local Queue             │
│ Health                  │
│ Media Access            │
│ Sync                    │
└────────────┬────────────┘
             │
             │ outbound TLS
             ▼
       CENTRAL VMS
```

---

# 15. Responsabilidades del Edge Agent

El Agent deberá:

- Detectar versión de Frigate.
- Registrar capacidades.
- Suscribirse a MQTT.
- Escuchar Review events.
- Detectar cambios.
- Recuperar metadata.
- Recuperar thumbnails.
- Enviar eventos al Central.
- Mantener cola offline.
- Realizar reintentos.
- Realizar backfill.
- Reportar health.
- Reportar uso de disco.
- Reportar cámaras.
- Reportar estado de cámaras.
- Reportar versión.
- Facilitar acceso seguro al media plane.
- Ejecutar acciones administrativas autorizadas.

---

# 16. Cola offline Edge

Utilizar:

```text
SQLite
```

en el Agent.

Si se pierde conectividad:

```text
Frigate
   ↓
Agent
   ↓
SQLite spool
```

Ejemplo:

```text
10:13 evento 100
10:14 evento 101
10:16 evento 102

INTERNET DOWN

10:20 evento 103
10:24 evento 104

INTERNET RESTORED

sync 100 ✓
sync 101 ✓
sync 102 ✓
sync 103 ✓
sync 104 ✓
```

La cola debe ser idempotente.

---

# 17. Sincronización con Frigate

El principal origen de eventos será MQTT.

Frigate publica cambios de Review en `frigate/reviews`. Los mensajes pueden representar creación, actualización o finalización del Review Item y pueden actualizarse cuando aparecen nuevos objetos, sublabels, LPR o reconocimientos posteriores.

Esto será utilizado para replicación en tiempo real.

Flujo:

```text
Frigate
   ↓
frigate/reviews
   ↓
Edge Agent
   ↓
Normalizer
   ↓
Central ingestion
   ↓
NATS
   ↓
Event Worker
   ↓
PostgreSQL
   ↓
WebSocket
   ↓
UI
```

---

# 18. Reconciliación / Backfill

MQTT por sí solo no debe considerarse garantía absoluta de sincronización.

El sistema deberá implementar reconciliación.

Periódicamente:

```text
Agent
  ↓
GET /api/review
  ↓
comparar remote IDs
  ↓
subir faltantes
```

Frigate expone Review mediante su API autenticada y permite filtrarlo por cámaras, etiquetas, zonas, severidad y rango temporal.

Se deberá mantener:

```text
last_successful_sync
last_remote_event_time
last_reconciliation
```

---

# 19. Compatibilidad con versiones Frigate

Crear:

```text
Frigate Adapter Layer
```

No utilizar llamadas directas a Frigate dispersas por todo el código.

Interfaces:

```text
FrigateAdapter
    GetVersion()
    GetCapabilities()
    ListCameras()
    GetReview()
    GetReviewPreview()
    GetSnapshot()
    GetRecording()
    CreateExport()
    GetHealth()
```

Implementaciones:

```text
adapter/v018
adapter/future
```

Esto permitirá adaptarse a cambios futuros sin reescribir el VMS.

---

# 20. Modelo central de Event

Nuestro concepto `Event` será una representación normalizada.

No deberá confundirse obligatoriamente con el histórico concepto interno de Event de Frigate.

Frigate desde 0.14 organiza actividad principalmente mediante Review Items, que agrupan períodos de actividad y pueden contener múltiples objetos detectados.

Modelo:

```text
Event

id
tenant_id
site_id
server_id
camera_id

remote_review_id

start_time
end_time

severity

objects[]
zones[]
sub_labels[]
audio_labels[]

has_lpr
license_plates[]

thumbnail_key
preview_key

remote_thumb_path

reviewed

created_at
updated_at
```

---

# 21. Idempotencia

Crear constraint:

```text
UNIQUE(server_id, remote_review_id)
```

Un evento recibido diez veces debe actualizar la misma fila.

Nunca crear duplicados por reintentos.

---

# 22. Almacenamiento de thumbnails

Por defecto:

```text
Metadata     CENTRAL ✓
Thumbnail    CENTRAL ✓
Preview      configurable
Snapshot     configurable
Video        LOCAL
Recording    LOCAL
```

El thumbnail central permitirá consultar eventos aunque el sitio esté temporalmente offline.

---

# 23. Previews

Frigate permite obtener previews asociados a Review Items y solicitar formatos GIF o MP4 mediante API autenticada.

Configurable por Tenant/Site:

```text
Store previews centrally:

Off
Alerts only
Alerts + detections
All
```

Retención independiente:

```text
30 días
90 días
180 días
365 días
custom
```

---

# 24. Usuarios

El VMS tendrá su propio sistema de identidad.

Un usuario NO deberá existir necesariamente en cada Frigate.

Modelo:

```text
users

id
tenant_id
username
email
display_name

password_hash

status

mfa_enabled

last_login_at
created_at
updated_at
```

Estados:

```text
active
disabled
locked
pending
```

---

# 25. Tipos de administrador

El sistema debe admitir administración delegada.

## Platform Super Admin

Acceso completo a la plataforma.

## Tenant Admin

Administra exclusivamente su Tenant.

Puede:

- Crear usuarios.
- Crear grupos.
- Asignar roles.
- Crear sitios.
- Registrar servidores según permisos.
- Administrar cámaras.
- Consultar auditoría.

## Site Admin

Administra uno o varios Sites.

## Security Supervisor

Operación y supervisión.

## Operator

Operación diaria.

## Viewer

Sólo visualización autorizada.

Los roles son plantillas.

La autorización real debe derivarse de permisos.

---

# 26. Grupos

Los permisos se administrarán prioritariamente mediante grupos.

Ejemplos:

```text
Administradores
Supervisores
Operadores Helvecia
Operadores Cayastá
Policía
Tránsito
Investigaciones
Auditoría
Monitoreo Nocturno
```

Un usuario podrá pertenecer a múltiples grupos.

---

# 27. Modelo RBAC + Scope

Permiso:

```text
permission
+
scope
```

Ejemplo:

```text
live.view
scope=SITE:Helvecia
```

Significa:

Puede ver Live de todas las cámaras pertenecientes a Helvecia.

---

# 28. Scopes soportados

```text
PLATFORM
TENANT
SITE
SERVER
CAMERA_GROUP
CAMERA
```

La herencia será descendente.

Ejemplo:

```text
SITE Helvecia
      ↓
SERVER H01
      ↓
CAMERA Plaza
```

Un permiso otorgado a SITE aplica a sus servidores y cámaras salvo excepción explícita.

---

# 29. Permisos

Namespace inicial:

```text
live.view
live.audio
live.talk
live.ptz

recordings.view
recordings.seek

events.view
events.search
events.review

snapshots.view
snapshots.download

lpr.view
lpr.search

exports.create
exports.download
exports.delete

views.create_private
views.create_shared
views.manage_shared

cases.view
cases.create
cases.update
cases.export

cameras.view
cameras.manage

servers.view
servers.manage
servers.restart
servers.config

users.view
users.manage

groups.view
groups.manage

permissions.manage

audit.view

health.view

notifications.manage

tenant.manage
```

---

# 30. Reglas de autorización

Aplicar:

```text
DENY > ALLOW
```

Ejemplo:

```text
ALLOW live.view SITE Helvecia

DENY live.view CAMERA Tesoreria
```

Resultado:

Puede visualizar todas las cámaras de Helvecia excepto Tesorería.

---

# 31. Seguridad server-side

TODOS los permisos deben verificarse en backend.

Nunca confiar en:

- Ocultar botones.
- Filtrado React.
- JavaScript.
- Rutas frontend.

Ejemplo búsqueda:

Usuario puede acceder a:

```text
CAM1
CAM2
CAM3
```

Consulta central equivalente:

```sql
WHERE camera_id IN (
  cameras_authorized_for_user
)
```

Si existen diez eventos en total pero sólo tres pertenecen a cámaras autorizadas:

```text
Resultado visible: 3
```

Nunca:

```text
"10 encontrados, mostrando 3"
```

La existencia de datos no autorizados también es información protegida.

---

# 32. Cache de permisos

Mantener en Valkey:

```text
user_permissions:{user_uuid}
```

Invalidar inmediatamente cuando:

- Usuario cambia de grupo.
- Grupo cambia.
- ACL cambia.
- Cámara cambia de Site.
- Permiso es revocado.

No depender exclusivamente del cache.

PostgreSQL sigue siendo source of truth.

---

# 33. Camera Groups

Permitir agrupación lógica independiente de Frigate.

Ejemplo:

```text
Accesos
Rutas
LPR
PTZ
Plazas
Hospital
Escuelas
Cementerios
Edificios públicos
```

Una cámara podrá pertenecer a múltiples Camera Groups.

Esto permitirá:

```text
Policía
    ALLOW live.view
    SCOPE camera_group:Accesos
```

---

# 34. Autenticación

Inicial:

```text
username/email + password
```

Passwords:

```text
Argon2id
```

No almacenar password reversible.

Agregar:

```text
TOTP MFA
```

Opcional por usuario.

Configurable obligatorio por Tenant/Admin.

---

# 35. SSO

Diseñar desde V1 la interfaz para:

```text
Local
OIDC
LDAP
Active Directory
Authentik
Keycloak
Microsoft Entra ID
Google Workspace
```

Implementar OIDC como primera autenticación externa.

---

# 36. Sesiones

Utilizar:

- Access token corto.
- Refresh token rotativo.
- Device/session tracking.
- Revocación server-side.

Registrar:

```text
IP
User Agent
created_at
last_seen
expires_at
```

Permitir:

```text
Cerrar esta sesión
Cerrar todas mis sesiones
Administrador → revocar sesiones usuario
```

---

# 37. Live View

El usuario podrá crear vistas mezclando cámaras provenientes de cualquier Frigate autorizado.

Ejemplo:

```text
VISTA: ACCESOS REGIONALES

┌────────────┬────────────┬────────────┐
│ Helvecia   │ Cayastá    │ Helvecia   │
│ Acceso N   │ Ruta 1     │ Cementerio │
├────────────┼────────────┼────────────┤
│ Cayastá    │ Santa Rosa │ Santa Rosa │
│ Plaza      │ Ruta       │ Acceso     │
└────────────┴────────────┴────────────┘
```

---

# 38. Vistas

Tipos:

```text
Private
Shared
System
```

Private:

Sólo propietario.

Shared:

Compartida con usuarios/grupos.

System:

Administrada por administradores.

Configuraciones:

```text
1
2x2
3x3
4x4
5x5
6x6
8x8
custom
```

Target inicial:

```text
32 cámaras simultáneas
```

Target posterior:

```text
64+
```

---

# 39. Optimización Live

Nunca abrir automáticamente el stream principal de todas las cámaras.

Cada cámara deberá definir:

```text
live_stream
high_quality_stream
```

Grid:

```text
substream
```

Single Camera:

```text
main stream
```

Cambio dinámico:

```text
Grid
    ↓ click
Single View
    ↓
HQ stream
```

---

# 40. Data Plane de Video

Objetivo:

Evitar que todo el video necesariamente atraviese el servidor central.

Preferir:

```text
Browser
   ↓ signed media session
Edge/Site
   ↓
Frigate
```

Cuando exista conectividad directa/VPN.

Fallback:

```text
Browser
   ↓
Central Media Gateway
   ↓
Site
   ↓
Frigate
```

El sistema deberá decidir topología disponible.

---

# 41. Media Session

El frontend nunca recibirá credenciales permanentes de Frigate.

Solicita:

```text
POST /api/media/session
```

Payload:

```json
{
  "camera_id": "...",
  "purpose": "live"
}
```

Backend verifica:

```text
live.view
```

y emite token corto:

```text
camera
user
purpose
expires
nonce
```

TTL sugerido:

```text
30-120 segundos
```

---

# 42. Protocolos de video

Prioridad:

```text
WebRTC
MSE
HLS fallback
```

Nunca forzar transcoding cuando pueda evitarse.

Aprovechar streams compatibles ya disponibles en Frigate/go2rtc.

---

# 43. Búsqueda Global

Debe realizarse contra el índice central.

NO hacer fan-out a todos los servidores para cada búsqueda normal.

Incorrecto:

```text
Search
 ├→ Frigate 1
 ├→ Frigate 2
 ├→ Frigate 3
 ├→ Frigate 4
 └→ Frigate 50
```

Correcto:

```text
Search
   ↓
Central PostgreSQL
   ↓
results
```

---

# 44. Filtros de búsqueda

Mínimo:

```text
Date range
Time range
Tenant
Site
Server
Camera
Camera Group

Severity

Object
Zone
Sub-label

Person
Vehicle type

License plate

Reviewed / Unreviewed

Has snapshot
Has preview
```

Futuro:

```text
Color
Direction
Speed
Face
Semantic text search
Similarity
```

---

# 45. Búsqueda LPR

Crear tabla especializada.

```text
lpr_detection

id
event_id

tenant_id
site_id
server_id
camera_id

plate_raw
plate_normalized

confidence

timestamp

snapshot_key
```

Normalización Argentina:

```text
AB 123 CD
AB-123-CD
ab123cd
```

deberán indexarse como:

```text
AB123CD
```

---

# 46. Índices PostgreSQL

Crear índices específicos para:

```text
event.start_time
event.camera_id
event.site_id
event.server_id
event.severity

GIN objects
GIN zones
GIN sub_labels

lpr.plate_normalized
lpr.timestamp
```

Para búsquedas parciales:

```text
pg_trgm
```

---

# 47. Escala inicial

Diseñar inicialmente para:

```text
100 Frigate Servers

2.000 cámaras

100 millones de eventos indexados

500 usuarios

100 operadores concurrentes
```

Sin necesidad de que una instalación real inicial tenga ese volumen.

---

# 48. Escalabilidad de búsqueda

PostgreSQL será suficiente inicialmente con:

- Particionado temporal.
- Buenos índices.
- Retención.
- Vacuum adecuado.
- Connection pooling.

Introducir OpenSearch solamente cuando métricas reales lo justifiquen.

No introducirlo en V1 por complejidad innecesaria.

---

# 49. Particionado de Events

Particionar por:

```text
start_time
```

mensualmente.

Ejemplo:

```text
events_2026_09
events_2026_10
events_2026_11
```

---

# 50. Retención central

Separada de Frigate.

Ejemplo:

```text
Event metadata       2 años
Thumbnail            1 año
Preview              90 días
Audit                 5 años
Health metrics        90 días
```

Configurable por Tenant.

---

# 51. Event Detail

Pantalla:

```text
EVENT

Site:
Helvecia

Server:
Frigate-H01

Camera:
Cementerio

Date:
26/09/2026

Time:
14:31:52

Objects:
Car

Plate:
AG123BC

Zones:
entrada

Severity:
Alert

[ THUMBNAIL / PREVIEW ]

[ LIVE CAMERA ]

[ VIEW RECORDING ]

[ EXPORT ]

[ ADD TO CASE ]
```

---

# 52. Playback

Al solicitar video:

```text
event
 ↓
camera_uuid
 ↓
server_uuid
 ↓
remote camera
 ↓
start/end
 ↓
Frigate recording
```

Las grabaciones permanecen en Frigate.

Frigate mantiene sus recordings localmente y su API permite trabajar con rangos temporales y exports.

---

# 53. Export

El usuario solicita:

```text
EXPORT

Camera
From
To
```

El backend verifica:

```text
exports.create
```

Después solicita export al Frigate correspondiente.

Frigate soporta exportar rangos de grabación y conservar esos exports separadamente de la retención normal.

---

# 54. Evidencia central

Una vez generado el export se podrá:

```text
dejar remoto
```

o:

```text
copiar a Object Storage central
```

Para evidencia/caso se recomienda copiarlo al central.

Registrar:

```text
SHA-256
size
created_at
created_by
original_site
original_camera
start
end
```

---

# 55. Cases

Módulo de investigación.

Modelo:

```text
Case

id
tenant_id

number
title
description

status

created_by
assigned_to

created_at
updated_at
closed_at
```

Estados:

```text
OPEN
INVESTIGATING
CLOSED
ARCHIVED
```

---

# 56. Evidencia dentro de Cases

Un Case puede contener:

```text
Events
Snapshots
Video exports
License plates
Notes
Attachments
```

Registrar cadena de auditoría.

---

# 57. Integridad de evidencia

Para archivos almacenados centralmente:

```text
SHA-256
```

Guardar hash en DB.

Opcional futuro:

```text
digital signature
timestamp authority
```

---

# 58. Dashboard principal

Mostrar:

```text
Sites online
Sites offline

Servers online
Servers degraded
Servers offline

Cameras online
Cameras offline

Storage warnings

Alerts today
Detections today

LPR today

Events last hour

Active operators
```

---

# 59. Health Monitor

Por Server:

```text
Online state
Frigate version
Uptime

CPU
RAM

GPU
Detector

Disk total
Disk used
Disk free

Recording health

MQTT state

Camera count

Camera offline count

Last event

Last agent contact
```

---

# 60. Camera Health

Estado:

```text
ONLINE
DEGRADED
OFFLINE
UNKNOWN
```

Datos:

```text
last_frame
last_event
fps
stream health
detect state
record state
audio state
```

---

# 61. Alertas de infraestructura

Ejemplos:

```text
Server offline > 60 sec
Camera offline > 60 sec

Disk > 80%
Disk > 90%

No events unusually long

Agent disconnected

MQTT disconnected

Frigate restarted

Version mismatch
```

---

# 62. Notification Engine

Canales inicialmente:

```text
In-app
Email
Webhook
```

Posteriormente:

```text
WhatsApp
Telegram
Push
SMS
```

Regla:

```text
IF
camera offline > 5 minutes

THEN
notify group Supervisores
```

---

# 63. Administración de Frigate Servers

Pantalla:

```text
Servers

Name
Site
IP/URL
Frigate Version
Agent Version
Status
Cameras
Storage
Last Seen
```

---

# 64. Registrar Server

Wizard:

### Step 1

```text
Tenant
Site
Name
```

### Step 2

```text
Connection

URL
Authenticated port
Credentials
```

Preferir el puerto autenticado `8971`. Frigate aplica allí sus roles y autorización. El puerto interno `5000` no aplica RBAC y equivale funcionalmente a acceso administrador anónimo, por lo que nunca deberá quedar expuesto como interfaz pública del VMS.

### Step 3

Detectar:

```text
Version
Cameras
Capabilities
MQTT
```

### Step 4

```text
Install Edge Agent
```

### Step 5

```text
Import cameras
```

---

# 65. Gestión central de configuración

No requerida para MVP inicial.

Fase posterior:

```text
Read configuration
Edit configuration
Validate
Create backup
Apply
Restart Frigate
Rollback
```

Toda modificación debe generar Audit Entry.

Nunca sobrescribir configuración sin backup.

---

# 66. Auditoría

Audit log será append-only desde aplicación.

Registrar:

```text
LOGIN
LOGIN_FAILED
LOGOUT

LIVE_VIEW

EVENT_SEARCH
LPR_SEARCH

PLAYBACK

SNAPSHOT_DOWNLOAD

EXPORT_CREATED
EXPORT_DOWNLOADED

CASE_CREATED
CASE_UPDATED

USER_CREATED
USER_DISABLED

GROUP_CHANGED
PERMISSION_CHANGED

SERVER_ADDED
SERVER_REMOVED

CONFIG_CHANGED
SERVER_RESTARTED
```

---

# 67. Ejemplo Audit

```text
2026-09-26T18:14:03Z

actor:
juan.perez

action:
LPR_SEARCH

query:
AG123BC

scope:
Helvecia

ip:
10.20.10.33
```

---

# 68. Protección Audit

Operadores:

```text
NO DELETE
NO MODIFY
```

Tenant Admin:

```text
VIEW
```

Platform Admin:

Tampoco deberá editar entradas históricas desde UI.

---

# 69. Multi-tenancy

Toda entidad deberá tener relación explícita con Tenant cuando corresponda.

La aplicación nunca dependerá solamente de filtros frontend.

Aplicar Tenant isolation en:

- Queries.
- Repositories.
- Authorization.
- Cache keys.
- Object Storage paths.
- WebSocket topics.

---

# 70. Object Storage layout

Ejemplo:

```text
tenant/{tenant_uuid}/
    events/
    previews/
    snapshots/
    cases/
    exports/
```

---

# 71. Seguridad entre Agent y Central

Utilizar:

```text
TLS
```

Preferentemente:

```text
mTLS
```

Cada Agent recibe identidad única.

Ejemplo:

```text
agent_uuid
certificate
private key
tenant_id
server_id
```

Permitir revocar Agent.

---

# 72. Provisioning Agent

Central crea:

```text
one-time enrollment token
```

Ejemplo:

```bash
frigate-edge-agent enroll \
  --server https://vms.example.com \
  --token XXXXX
```

Token:

- One-time.
- TTL corto.
- Vinculado a server_uuid.

Agent recibe certificado.

---

# 73. Secrets

Nunca guardar credenciales Frigate en texto plano.

Utilizar:

```text
AES-256-GCM
```

con master key externa.

Ideal:

```text
Docker Secret
Kubernetes Secret
Vault
```

dependiendo del despliegue.

---

# 74. API principal

Prefijo:

```text
/api/v1
```

Recursos:

```text
/auth

/tenants
/sites
/servers
/cameras

/users
/groups
/roles
/permissions

/events
/search
/lpr

/views

/cases
/exports

/health
/audit

/media
```

---

# 75. OpenAPI

Toda API deberá documentarse mediante:

```text
OpenAPI 3.x
```

Generar:

```text
/openapi.json
/docs
```

No desarrollar endpoints sin contrato documentado.

---

# 76. WebSocket

Endpoint:

```text
/ws
```

Eventos:

```text
event.created
event.updated

camera.online
camera.offline

server.online
server.offline

notification.created

export.progress

permission.changed
```

---

# 77. Pagination

Utilizar cursor pagination para eventos.

No offset pagination para conjuntos grandes.

Ejemplo:

```text
?limit=100&cursor=...
```

---

# 78. Timestamps

Toda DB:

```text
UTC
```

Cada Site:

```text
timezone
```

Frontend transforma a timezone correspondiente.

Ejemplo:

```text
America/Argentina/Cordoba
```

---

# 79. Soft Delete

Utilizar en:

```text
users
sites
servers
cameras
groups
views
```

No borrar datos históricos inmediatamente.

Campos:

```text
deleted_at
deleted_by
```

---

# 80. UI general

Sidebar:

```text
Dashboard

Live

Events
Search
LPR

Cases

Sites
Servers
Cameras

Views

Health
Notifications

Users
Groups
Permissions

Audit

Settings
```

Secciones visibles según permisos.

---

# 81. Live Workspace

Modo estilo VMS.

```text
┌─────────────────────────────────────────────┐
│ Search cameras                             │
├─────────────┬───────────────────────────────┤
│ Sites       │                               │
│             │                               │
│ Helvecia    │           GRID                │
│   H01       │                               │
│   H02       │                               │
│             │                               │
│ Cayastá     │                               │
│             │                               │
├─────────────┴───────────────────────────────┤
│ Timeline / alarms                          │
└─────────────────────────────────────────────┘
```

Drag & Drop cámaras al grid.

---

# 82. Global Search UX

Barra principal:

```text
Search events...
```

Filtros laterales.

Resultados:

```text
Thumbnail

Timestamp
Site
Camera
Objects
Plate
Zone
Severity
```

Infinite scroll.

---

# 83. Búsqueda semántica futura

Fase posterior.

Ejemplo:

```text
"camioneta blanca"
"persona con mochila"
"auto detenido frente al portón"
```

Dos opciones futuras:

1. Índice central propio con embeddings.
2. Adapter federado hacia semantic search local de Frigate.

La implementación deberá ser modular y no bloquear V1.

---

# 84. Video Wall

Crear modo:

```text
VIDEO WALL
```

Características:

- Fullscreen.
- Sin sidebar.
- Layout fijo.
- Rotación automática.
- Vistas programadas.
- 16/25/32/64 streams según hardware.
- Indicador offline.
- Overlay mínimo.

Futuro:

```text
Dynamic wall
```

que cambie cámaras automáticamente ante eventos.

---

# 85. PTZ

Cuando cámara soporte PTZ:

```text
live.ptz
```

Mostrar:

- Pan.
- Tilt.
- Zoom.
- Presets.

Sólo usuarios autorizados.

Registrar operaciones PTZ relevantes en audit.

---

# 86. Performance frontend

Requisitos:

- Virtualizar listas largas.
- Lazy load imágenes.
- Cancelar requests obsoletos.
- No cargar thumbnails fuera de viewport.
- WebSocket único por sesión.
- No crear conexión por cámara.
- Limitar streams simultáneos según layout.
- Suspender streams invisibles.

---

# 87. Observabilidad

Stack recomendado:

```text
Prometheus
Grafana
Loki
OpenTelemetry
```

---

# 88. Métricas VMS

Ejemplos:

```text
vms_connected_agents
vms_connected_users

vms_servers_online
vms_cameras_online

vms_events_ingested_total

vms_event_ingestion_latency

vms_search_latency

vms_media_sessions

vms_agent_queue_depth

vms_frigate_api_errors
```

---

# 89. Correlation ID

Cada request:

```text
X-Request-ID
```

Propagar:

```text
Frontend
→ Backend
→ NATS
→ Worker
→ Agent
```

---

# 90. Logs

Formato:

```text
JSON
```

Campos:

```text
timestamp
level
service
request_id
tenant_id
user_id
server_id
message
```

No loggear passwords/tokens.

---

# 91. Health endpoints

```text
/health/live
/health/ready
```

Separados.

---

# 92. Deployment Central

Primera opción:

Docker Compose.

Servicios:

```text
vms-api
vms-worker
vms-web

postgres
nats
valkey
minio

prometheus
grafana
loki
```

Reverse proxy:

```text
Caddy
```

o:

```text
Traefik
```

---

# 93. Producción HA

Posteriormente:

```text
2+ API replicas
2+ Worker replicas

PostgreSQL HA
NATS cluster
Valkey HA
S3/MinIO distributed
```

API y Worker deberán ser stateless.

---

# 94. Kubernetes

Soportar posteriormente pero NO hacerlo requisito de desarrollo inicial.

El producto debe funcionar perfectamente mediante Docker Compose.

---

# 95. Backup

Central:

```text
PostgreSQL
Object Storage
Secrets/config
```

No es necesario que Central respalde automáticamente las grabaciones de Frigate.

---

# 96. Alta disponibilidad Frigate

Fuera del alcance del VMS.

Cada Site administra su disponibilidad.

El VMS solamente monitorea.

---

# 97. Estado ante caída Central

Si Central cae:

```text
Frigate recording        ✓
Frigate detection        ✓
Frigate LPR              ✓
Frigate UI               ✓
Frigate retention        ✓
```

Temporalmente no disponible:

```text
Global search
Central users
Federated Live UI
Central cases
Central audit
```

Cuando Central vuelve:

```text
Agents reconnect
queues synchronize
events backfill
```

---

# 98. Estado ante caída Site

Central mantiene:

```text
Metadata events
Thumbnail almacenado
Audit
Cases
Historical health
```

Muestra:

```text
SITE OFFLINE
```

Playback remoto:

```text
Unavailable until site reconnects
```

---

# 99. Estado de sincronización

Por Server:

```text
LIVE
SYNCING
DEGRADED
OFFLINE
```

Mostrar:

```text
Last Event Sync
Last Health
Queue Pending
Last Reconciliation
```

---

# 100. Administración de compatibilidad

Cada server mantiene:

```text
frigate_version
agent_version
api_capabilities
```

No asumir igualdad entre servidores.

Ejemplo:

```text
Helvecia     Frigate 0.18
Cayastá      Frigate 0.18
Cliente X    versión posterior
```

---

# 101. Feature Capabilities

Tabla:

```text
server_capabilities

preview
lpr
face_recognition
semantic_search
ptz
audio
exports
cases
```

La UI habilita acciones según capacidad.

---

# 102. Seguridad Frigate

El VMS debe usar preferentemente la interfaz autenticada de Frigate.

Frigate admite roles `admin`, `viewer` y roles personalizados de sólo lectura restringidos a cámaras específicas, con enforcement del lado backend cuando se usa el puerto autenticado.

Nuestro VMS implementará un RBAC mucho más granular y no dependerá de crear todos los usuarios centrales dentro de cada Frigate.

---

# 103. Credencial técnica Frigate

Cada conexión Server tendrá Service Account.

Ideal:

```text
VMS connector account
```

Nivel requerido según funciones activadas.

Modo read-only:

```text
viewer/custom
```

Modo administración:

```text
admin
```

No usar admin si no es necesario.

---

# 104. Rate Limiting

Aplicar por:

```text
IP
user
tenant
endpoint
```

Más estricto para:

```text
login
password reset
exports
search
```

---

# 105. Seguridad HTTP

Implementar:

```text
TLS only

Secure cookies
HttpOnly
SameSite

CSP
HSTS
X-Content-Type-Options

CSRF protection cuando corresponda
```

---

# 106. Account Security

Implementar:

- Password policy.
- Login throttling.
- Lockout progresivo.
- MFA.
- Revocación de sesiones.
- Password reset seguro.
- Audit de login.
- Alertas de login sospechoso opcionales.

---

# 107. Testing

## Unit Tests

Backend:

```text
RBAC
normalizers
search
LPR normalization
sync logic
```

## Integration Tests

Con:

```text
PostgreSQL
NATS
Valkey
MinIO
```

mediante containers.

## Frigate Adapter Tests

Mock Frigate API.

Test contract para cada versión soportada.

## E2E

Playwright.

Casos:

```text
login
create user
assign group
live access
denied camera
search event
LPR search
export
case
```

---

# 108. Prueba crítica de seguridad

Crear siempre tests negativos.

Ejemplo:

Usuario sólo puede ver:

```text
CAM-A
```

Debe recibir:

```text
200 CAM-A
403 CAM-B
```

Buscar CAM-B:

```text
0 results
```

No basta con ocultarla en UI.

---

# 109. CI/CD

GitHub Actions.

Pipeline:

```text
lint
unit tests
integration tests
frontend tests
build

docker build
security scan
```

Generar:

```text
vms-api image
vms-web image
vms-worker image
frigate-edge-agent binary/image
```

---

# 110. Versionado

Semantic Versioning.

Ejemplo:

```text
v0.1.0
v0.2.0
v1.0.0
```

---

# 111. Database migrations

Cada release debe contener migrations versionadas.

Nunca modificar DB manualmente en producción.

---

# 112. Monorepo

Estructura propuesta:

```text
/
├── apps/
│   ├── web/
│   ├── api/
│   ├── worker/
│   └── edge-agent/
│
├── internal/
│   ├── auth/
│   ├── rbac/
│   ├── tenants/
│   ├── sites/
│   ├── frigate/
│   ├── cameras/
│   ├── events/
│   ├── search/
│   ├── media/
│   ├── lpr/
│   ├── cases/
│   ├── audit/
│   └── health/
│
├── packages/
│   ├── api-contract/
│   └── ui/
│
├── migrations/
├── deploy/
│   ├── docker/
│   └── kubernetes/
│
├── docs/
└── scripts/
```

---

# 113. Development environment

Comando único:

```bash
docker compose up -d
```

y:

```bash
make dev
```

Debe levantar entorno funcional.

---

# 114. Mock Frigate

Crear servicio:

```text
frigate-mock
```

Para desarrollo sin cámaras reales.

Debe simular:

```text
Cameras
Review
Events
MQTT
Snapshots
Previews
Health
Exports
```

Fundamental para CI.

---

# 115. Seed data

Crear:

```text
Demo Tenant

Helvecia
Cayastá
Santa Rosa
```

con múltiples servers/cameras/eventos simulados.

---

# 116. Fases de desarrollo

## PHASE 0 — Foundation

Crear:

```text
Monorepo
Docker Compose
PostgreSQL
Valkey
NATS
MinIO

API
Web

CI
Migrations
Logging
OpenTelemetry
```

Resultado:

Plataforma base ejecutable.

---

# 117. PHASE 1 — Multi-Site Core

Implementar:

```text
Tenant
Site
Frigate Server
Camera
Camera Group
```

Registrar Frigate.

Discover cameras.

Detectar versión.

Health básico.

Resultado:

Administrar múltiples Frigate desde el central.

---

# 118. PHASE 2 — Federated Events

Implementar:

```text
MQTT ingestion
Review normalization
Event index
Thumbnail storage
Reconciliation
Backfill
```

UI:

```text
Global Events
```

Resultado:

Ver eventos de múltiples servidores en una sola pantalla.

---

# 119. PHASE 3 — Global Search + LPR

Implementar filtros.

LPR global.

Indexes.

Pagination.

Resultado:

Buscar una patente/evento entre todos los Frigate autorizados.

---

# 120. PHASE 4 — Identity + RBAC

Implementar:

```text
Users
Groups
Roles
Scopes
Permissions
Delegated admins
MFA
Audit
```

Integrar autorización en TODOS los endpoints existentes.

Resultado:

Sistema multiusuario enterprise.

---

# 121. PHASE 5 — Federated Live

Implementar:

```text
Live
Camera tree
Layouts
Views
Shared views
Media tokens
Media gateway
```

Resultado:

Visualizar cámaras provenientes de múltiples Frigate simultáneamente.

---

# 122. PHASE 6 — Playback + Export

Implementar:

```text
Timeline
Playback
Exports
Snapshots
Download permissions
```

Resultado:

Investigación completa desde VMS.

---

# 123. PHASE 7 — Edge Agent

Aunque se puede prototipar previamente, convertir Agent en componente productivo.

Implementar:

```text
Enrollment
mTLS
SQLite queue
MQTT local
Backfill
Health
Media edge
Remote actions
Auto update
```

---

# 124. PHASE 8 — Cases

Implementar:

```text
Cases
Evidence
Notes
Exports
Hashes
Case package
```

---

# 125. PHASE 9 — Enterprise Operations

Implementar:

```text
Notifications
Advanced audit
OIDC
LDAP
AD
PTZ

Server configuration
Backups
Remote restart
Version monitoring
```

---

# 126. PHASE 10 — Advanced AI

Opcional:

```text
Semantic global search
Face federation
Vehicle attributes
Cross-camera tracking
Advanced LPR
Speed events
AI summaries
```

---

# 127. Prioridad MVP

Para tener rápidamente algo demostrable:

```text
1 Multi-Site
2 Server discovery
3 Camera inventory
4 Federated events
5 Global search
6 LPR
7 Users/RBAC
8 Multi-server Live Views
9 Health
10 Audit
```

---

# 128. Criterio MVP funcional

El MVP se considera exitoso cuando podamos tener:

```text
Frigate Helvecia
+
Frigate Cayastá
```

y desde un único VMS:

- Ver ambas instalaciones.
- Ver todas las cámaras autorizadas.
- Crear una vista mezclando cámaras.
- Consultar eventos de ambas.
- Buscar una patente en ambas.
- Abrir un evento.
- Reproducir grabación desde el Frigate origen.
- Crear dos usuarios con cámaras diferentes.
- Verificar server-side que uno no pueda acceder a cámaras del otro.
- Mantener eventos centrales si un Frigate queda offline.
- Recuperar automáticamente eventos perdidos cuando vuelve.
- Registrar todas las operaciones relevantes en auditoría.

---

# 129. Acceptance Test principal

Infraestructura:

```text
Frigate-A
    camera-a1
    camera-a2

Frigate-B
    camera-b1
    camera-b2
```

Grupo:

```text
Operator-A
```

Permisos:

```text
camera-a1
camera-b2
```

Debe poder:

```text
Live camera-a1 ✓
Live camera-b2 ✓

Search camera-a1 ✓
Search camera-b2 ✓

Playback camera-a1 ✓
Playback camera-b2 ✓
```

Debe recibir:

```text
camera-a2 → 403
camera-b1 → 403
```

Global Search tampoco debe revelar eventos de esas cámaras.

---

# 130. Requisito de autonomía

Esta condición será considerada **invariante arquitectónica**:

> Ninguna actualización futura del VMS debe convertir al servidor central en dependencia obligatoria para que Frigate pueda grabar, detectar o conservar video local.

---

# 131. Requisito anti-fork

Segunda invariante:

> No modificar el código fuente principal de Frigate salvo que exista una razón técnica extraordinaria.

La integración deberá hacerse mediante:

```text
API
MQTT
supported streaming interfaces
```

De esta forma Frigate puede actualizarse de manera independiente.

---

# 132. Filosofía del producto

El producto no deberá presentarse técnicamente como:

```text
Modified Frigate
```

sino como:

```text
Federated VMS Platform for Frigate
```

Frigate será el:

```text
Edge NVR / AI Engine
```

Nuestro producto será:

```text
Enterprise Control Plane
+
Federated Video Management System
```

---

# 133. Arquitectura final objetivo

```text
                             INTERNET / PRIVATE NETWORK
                                      │
                                      │
                          ┌───────────▼───────────┐
                          │    FEDERATED VMS      │
                          │                       │
                          │ Authentication        │
                          │ RBAC                  │
                          │ Multi-Tenant          │
                          │ Multi-Site            │
                          │ Global Search         │
                          │ LPR                   │
                          │ Live Views            │
                          │ Cases                 │
                          │ Audit                 │
                          │ Health                │
                          └───────────┬───────────┘
                                      │
                            Control / Metadata
                                      │
             ┌────────────────────────┼────────────────────────┐
             │                        │                        │
             ▼                        ▼                        ▼

     ┌─────────────────┐      ┌─────────────────┐      ┌─────────────────┐
     │ EDGE AGENT      │      │ EDGE AGENT      │      │ EDGE AGENT      │
     │ Helvecia        │      │ Cayastá         │      │ Cliente X       │
     └────────┬────────┘      └────────┬────────┘      └────────┬────────┘
              │                        │                        │
              ▼                        ▼                        ▼
     ┌─────────────────┐      ┌─────────────────┐      ┌─────────────────┐
     │ FRIGATE         │      │ FRIGATE         │      │ FRIGATE         │
     │                 │      │                 │      │                 │
     │ Cameras         │      │ Cameras         │      │ Cameras         │
     │ Recording       │      │ Recording       │      │ Recording       │
     │ Detection       │      │ Detection       │      │ Detection       │
     │ LPR             │      │ LPR             │      │ LPR             │
     │ AI              │      │ AI              │      │ AI              │
     └────────┬────────┘      └────────┬────────┘      └────────┬────────┘
              │                        │                        │
              ▼                        ▼                        ▼
        LOCAL STORAGE              LOCAL STORAGE            LOCAL STORAGE
```

---

# 134. Decisiones arquitectónicas obligatorias

El equipo de desarrollo deberá respetar:

1. Frigate continúa siendo autónomo.
2. Las grabaciones permanecen locales.
3. Los eventos se indexan centralmente.
4. Los thumbnails se almacenan centralmente.
5. Los previews son configurables.
6. Las búsquedas normales se ejecutan centralmente.
7. El VMS posee su propio RBAC.
8. Toda autorización se valida server-side.
9. Una cámara posee UUID global.
10. Site y Server son entidades diferentes.
11. Un Site puede contener múltiples Frigate.
12. Una vista puede mezclar cámaras de cualquier Server autorizado.
13. Un grupo de cámaras puede atravesar múltiples Sites y Servers.
14. El backend no dependerá de nombres de cámaras como IDs globales.
15. Frigate se integra mediante Adapter.
16. MQTT no será el único mecanismo de consistencia.
17. Se implementará reconciliación.
18. El sistema debe tolerar desconexiones.
19. Las operaciones relevantes quedan auditadas.
20. Nunca se expondrán credenciales permanentes Frigate al navegador.
21. El media plane debe poder escalar independientemente del control plane.
22. No introducir microservicios innecesarios durante las primeras fases.

---

# 135. Resultado esperado

Al completar las fases principales, un operador deberá iniciar sesión una sola vez y poder trabajar con cientos de cámaras distribuidas como si pertenecieran a un único VMS.

Para el operador:

```text
no existen 20 Frigates

existe:

CCTV Helvecia
CCTV Cayastá
Accesos
Rutas
Plazas
LPR
Investigaciones
```

La complejidad de qué servidor almacena cada grabación deberá quedar resuelta por la plataforma.

El sistema deberá combinar:

```text
Autonomía de Frigate
+
Arquitectura distribuida
+
Administración central
+
RBAC enterprise
+
Búsqueda federada
+
Live multi-site
+
Auditoría
+
Gestión de evidencia
```

sin transformar Frigate en una dependencia propietaria ni en un fork difícil de mantener.

---

# 136. Definición final del producto

**Federated Frigate VMS** es una plataforma multi-tenant y multi-site que convierte múltiples instalaciones Frigate autónomas en una infraestructura de videovigilancia federada, proporcionando administración centralizada, usuarios y permisos granulares, búsquedas globales, LPR, live view multi-servidor, reproducción, evidencia, casos, auditoría y monitoreo, manteniendo el procesamiento, grabación y almacenamiento primario distribuidos en cada instalación Frigate.

Ese principio deberá mantenerse durante toda la vida del producto.