# Decisiones de stack

Registro de las decisiones que el PRD dejaba abiertas o que cambian respecto de él. Revisión del 2026-09-26.

| Tema | Decisión | Motivo |
|---|---|---|
| Object storage | **SeaweedFS** en desarrollo; cualquier S3 en producción | MinIO community entró en modo mantenimiento en diciembre de 2025. El código usa solo la API S3 (`aws-sdk-go-v2`), así que Garage o AWS S3 funcionan sin cambios. |
| Contrato API | **OpenAPI primero**: `oapi-codegen` (strict server sobre chi) y `openapi-typescript` + `openapi-fetch` | El contrato es la fuente de verdad (PRD §75) y CI verifica que el código generado esté al día. |
| Migraciones | **goose**, SQL plano, embebidas en el binario | Compatible con sqlc y sin funciones bajo licencia comercial. La API aplica las pendientes al arrancar (`MIGRATE_ON_START`). |
| Routing web | **TanStack Router** | Rutas y search params tipados; los filtros de búsqueda van a vivir en la URL. |
| Idioma de la UI | Español | Operadores en Argentina. Se puede internacionalizar más adelante. |
| Transporte Agent ↔ Central | **ConnectRPC** con mTLS (a implementar en M7) | Stream bidireccional iniciado por el Agent, atraviesa NAT, protobuf compartido. |
| Video en vivo | **MSE sobre WebSocket vía Media Gateway** primero, WebRTC después (M5) | MSE no necesita TURN ni transcodificar y es el fallback que ya usa Frigate. |
| Sincronización con Frigate | Un único paquete *connector* que corre en el worker (sin Agent) o en el Edge Agent | Permite el MVP sin instalar nada en los sitios y el Agent reutiliza código probado. |
| Aislamiento de tenant | Filtro en repositorios **y** Row-Level Security en Postgres (`FORCE`) | Segunda barrera si una query olvida el `tenant_id`. La API corre como el rol `openvms_app` (sin `BYPASSRLS`) y cada transacción fija `app.tenant_id`; las migraciones corren con el rol de login. |
| Autorización | Permiso + alcance (plataforma, tenant, sitio, servidor, grupo de cámaras, cámara), herencia descendente, **DENY gana**. Los listados filtran en SQL | Un operador nunca ve ni cuenta cámaras sin permiso (PRD §31). Recursos de otro tenant responden 404, del mismo tenant sin permiso 403. Nadie puede otorgar un ALLOW que no tiene. |
| Tokens de API | Se guarda solo su SHA-256; se emiten con `vmsctl token` | Para scripts e integraciones. Las personas entran con usuario, contraseña y MFA (ver Sesiones web). |
| Credenciales de Frigate | AES-256-GCM con la clave maestra; el id del servidor va como dato asociado | Un valor cifrado copiado a otra fila no descifra. Nunca vuelven por la API. |
| Búsqueda dinámica | sqlc para CRUD, queries armadas sobre pgx para filtros de búsqueda (M3) | sqlc no maneja bien 15+ filtros opcionales. |
| Versiones de Frigate | Adapter para **0.17** (estable) y **0.18** (RC al momento de escribir) | Los sitios van a convivir con ambas versiones. |
| Acceso a Frigate | Dos modos por servidor: **credenciales** (puerto 8971, login con cookie) o **sin login** (puerto 5000 o auth desactivada) | Instalaciones existentes usan 5000 en redes internas. El modo sin login solo se acepta explícitamente y la UI advierte que es para redes de confianza. |
| Ingesta de eventos | **Pull por HTTP** desde el worker cada 5 s, con cursor por servidor en la base | No requiere acceso al broker MQTT de cada sitio, y el mismo cursor hace el backfill tras una caída del VMS o del Frigate. MQTT queda como optimización futura para el Edge Agent. |
| Índice de eventos | Review items de Frigate como eventos; tracked objects solo para lecturas de patentes (`lpr_reads`) | Los review items son lo que el operador ve en Frigate; las patentes de 0.16+ viven en `data.recognized_license_plate` del objeto. |
| Miniaturas | Copia en object storage (`tenant/{id}/thumbs/...`) al terminar cada evento | Los eventos siguen siendo navegables si el Frigate está caído o ya borró el clip. |
| Sesiones web | Cookie `openvms_session` HttpOnly + SameSite=Strict, token opaco (se guarda su SHA-256), expiración absoluta 12 h e inactividad 2 h. Escrituras exigen el header `X-OpenVMS-Request` | Evita guardar tokens en el navegador y bloquea CSRF sin tokens adicionales. Los tokens de API siguen para scripts. |
| Contraseñas y MFA | Argon2id (m=64 MiB, t=3, p=2); TOTP RFC 6238 con el secreto cifrado con la clave maestra; bloqueo tras 5 fallos | Recomendación OWASP; TOTP funciona con cualquier app sin depender de terceros. |
| Media gateway | `/media/v1` en la API: relay del websocket MSE de go2rtc (en vivo), HLS de `/vod` (grabaciones), snapshots y descargas de exportaciones | El navegador nunca habla con Frigate ni conoce sus credenciales; cada pedido se autoriza por cámara (caché de 15 s). |
| Exportaciones | Frigate genera el archivo; el VMS guarda metadatos, sigue el job y reenvía la descarga | Sin copiar video al central (el central no es un NVR). |

## Orden de hitos

M0 fundación · M1 inventario multi-site + motor de permisos · M2 eventos federados · M3 búsqueda + LPR · M4 usuarios, sesiones y auditoría · M5 live federado · M6 playback + export (criterio MVP §128) · M7 Edge Agent productivo · M8+ casos, operaciones enterprise, IA.

El motor de permisos se adelanta a M1 (el PRD lo ubica en la fase 4) para no tener que agregar autorización después a endpoints ya existentes.
