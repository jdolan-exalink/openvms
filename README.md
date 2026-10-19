# OpenVMS · Federated Frigate VMS

**Versión actual: 0.4.0** ([changelog](CHANGELOG.md))

OpenVMS es un VMS central, multi-organización y multi-sitio, que federa varios servidores [Frigate](https://frigate.video) autónomos: usuarios y permisos propios, búsqueda global de eventos y patentes, vista en vivo multi-servidor, alarmas, mapas, auditoría y monitoreo. Cada Frigate sigue grabando y detectando por su cuenta; el central nunca es una dependencia para eso.

- Requisitos del producto: [`docs/PRD.md`](docs/PRD.md)
- Decisiones de stack: [`docs/architecture/decisions.md`](docs/architecture/decisions.md)
- Guía de instalación detallada (servidor nuevo, LXC, backup): [`Readme.txt`](Readme.txt)

Frigate soportado: **0.17 o posterior**. OpenVMS rechaza, al registrar o conectar un servidor, un Frigate 0.16 o anterior e informa la versión detectada.

## Funcionalidades

| Módulo | Qué ofrece |
|---|---|
| En vivo | Grillas y presentaciones por cámara, vistas guardadas, exportar e importar presentaciones, vista multi-servidor a través del media gateway y PWA instalable en teléfonos (horizontal, pantalla completa, zoom con pellizco, cuadrícula de snapshots). |
| Eventos y reproducción | Índice central de eventos de todos los Frigate con recuperación (backfill) tras caídas, búsqueda global, línea de tiempo, reproducción y exportaciones desde el Frigate de origen. |
| Patentes (LPR) | Lecturas de patentes por sitio, servidor y cámara. Se sincronizan los objetos rastreados de Frigate por páginas, de modo que un backlog grande tras una caída se recupera de forma progresiva. |
| Rostros | Registro central de personas con sus fotos, sincronizado a las bibliotecas de Frigate; entrenamiento desde detecciones; reconocimientos por sitio, servidor y cámara (como LPR); listas de vigilancia (watchlists); condiciones de persona y watchlist en reglas de alarma; configuración de reconocimiento facial por servidor. |
| Alarmas y reglas | Correlación de eventos en una alarma por situación, cierre automático por inactividad, bandeja con prioridad, enlaces de reproducción, reglas con plantillas, simulación, historial de versiones, grupos de cámaras, horarios en la zona horaria del sitio y cooldown. |
| Mapas | Edición de posiciones, tipos y FOV de cámaras, zonas, centro de monitoreo y navegación por sitio. Guía: [`docs/maps/OPERATIONS.md`](docs/maps/OPERATIONS.md). |
| Notificaciones | Canales con entregas y bandeja interna, incluido WhatsApp mediante WAHA ([`docs/notification-channels.md`](docs/notification-channels.md)). |
| Servidores y agentes | Inventario de sitios, servidores y cámaras; zonas persistidas con sincronización a Frigate; agente de borde (`edge-agent`) instalable por SSH; detección de aceleradores de IA y presets de detectores. |
| Seguridad y operación | Login con usuario y contraseña, sesiones, MFA TOTP, usuarios, grupos, permisos por alcance (organización, sitio, servidor, grupo de cámaras o cámara; denegar gana) y auditoría. |

Implementado no equivale a validado en producción: el estado y la evidencia de cada área están en [`odd/tasks/`](odd/tasks/) (por ejemplo [`maps.md`](odd/tasks/maps.md)).

## Requisitos

- Linux x86_64, Debian 12/13 o Ubuntu 22.04+. Con 2 CPU y 4 GB de RAM alcanza para decenas de cámaras: el VMS no graba video, lo hace cada Frigate.
- **Docker Engine con Compose v2.** El comando es `docker compose` (con espacio). El paquete `docker-compose` (con guion, Compose v1) no sirve: falla con `Invalid interpolation format` o con errores como `'name' does not match any of the regexes`. En Ubuntu 24.04: `apt-get install -y docker.io docker-compose-v2 docker-buildx git`; en Debian use el repositorio de Docker (pasos en [`Readme.txt`](Readme.txt)).
- En un LXC de Proxmox active `nesting` y `keyctl`; si no, Docker no arranca.
- Red desde el VMS a cada Frigate: puerto 8971 (con login) o 5000 (sin login, solo redes de confianza). El VMS usa `/api`, `/live/mse`, `/vod` y `/exports`.
- Puertos de entrada para los usuarios: 8000 (HTTP) y 8443 (HTTPS), configurables.

## Instalación rápida

```bash
git clone https://github.com/jdolan-exalink/openvms.git /root/openvms
cd /root/openvms
git checkout v0.4.0        # una versión etiquetada, no main
make install
```

`make install` ([`deploy/install.sh`](deploy/install.sh)) hace cinco pasos y nunca pisa un `.env` existente:

1. Verifica Docker y Compose v2 (con Compose v1 falla con un mensaje claro).
2. Crea `.env` (modo 0600) desde `.env.example` con `OPENVMS_MASTER_KEY` y `POSTGRES_PASSWORD` aleatorios.
3. Ejecuta `docker compose up -d --build` (la primera compilación tarda varios minutos; no hacen falta Go ni Node en el servidor).
4. Espera `/health/ready`.
5. Imprime las URL y los pasos siguientes.

**Guarde `OPENVMS_MASTER_KEY` fuera del servidor**: sin ella no se pueden leer las credenciales cifradas de los Frigate.

### Primer acceso

Al arrancar, la API crea sola el administrador de plataforma si la instalación no tiene ningún usuario de plataforma, y una organización con un sitio si no existe ninguna.

- Usuario: `admin` (`OPENVMS_ADMIN_USERNAME`).
- Contraseña: la de `OPENVMS_ADMIN_PASSWORD` si la definió en `.env` **antes** de levantar (mínimo 10 caracteres, letras y números). Si está vacía se genera una al azar, se registra una sola vez en el log de la API y el sistema obliga a cambiarla en el primer ingreso:

```bash
docker compose logs api | grep "initial admin"
```

Nunca pisa ni reinicia una instalación existente. Si la contraseña configurada no cumple la política, la API lo registra como error y no crea el usuario: use `vmsctl`.

| URL | Qué es |
|---|---|
| http://localhost:8000 | Web (Caddy sirve la SPA y enruta la API); muestra `OpenVMS vX.Y.Z` en el login |
| https://localhost:8443 | La misma web por HTTPS (certificado autofirmado, ver [Seguridad y red](#seguridad-y-red)) |
| http://localhost:8000/docs | Referencia de la API |
| http://localhost:8000/health/ready | Estado de dependencias |
| http://localhost:8000/version.json | `version`, `commit` y `buildTime` de la build web |

Después: active la verificación en dos pasos en "Mi cuenta", registre sus sitios y servidores (menú Servidores > Registrar servidor, con login `https://IP:8971` o sin login `http://IP:5000`) y cree usuarios, grupos y permisos. El recorrido completo está en la sección 7 de [`Readme.txt`](Readme.txt).

Comandos de administración (`vmsctl`, dentro del contenedor `api`):

```bash
docker compose exec api /vmsctl bootstrap -password 'UnaClaveLarga-2026'   # crea el admin con esa contraseña e imprime un token de API
docker compose exec api /vmsctl passwd -user admin -password 'Otra-Clave-2026'
docker compose exec api /vmsctl token -user admin                           # token para scripts
docker compose exec api /vmsctl migrate
docker compose exec api /vmsctl seed-demo                                   # organización demo (requiere los Frigate simulados)
```

### Instalación manual

Equivalente a `make install`, paso a paso (siempre `docker compose`, con espacio):

```bash
cp .env.example .env && chmod 600 .env
openssl rand -base64 32     # copiar el resultado a OPENVMS_MASTER_KEY en .env
# definir también POSTGRES_PASSWORD y, si desea, OPENVMS_ADMIN_PASSWORD
docker compose up -d --build        # o: make up
docker compose ps                   # todos "running" / "healthy"
curl -sk https://localhost:8443/health/ready
```

## Configuración (`.env`)

Todas las variables que lee `docker-compose.yml` están documentadas en `.env.example`; un valor vacío usa el predeterminado indicado allí. Para regenerar el ejemplo o agregar a un `.env` existente las variables que falten, sin tocar sus valores: `bash scripts/update-env-example.sh [--env]`.

| Grupo | Variables | Notas |
|---|---|---|
| Secretos (obligatorios) | `OPENVMS_MASTER_KEY`, `POSTGRES_PASSWORD` | La clave maestra son 32 bytes aleatorios en base64 (`openssl rand -base64 32`); no la cambie en una instalación existente. `POSTGRES_PASSWORD` solo se aplica al crear el volumen de la base. |
| Primer administrador | `OPENVMS_ADMIN_USERNAME`, `OPENVMS_ADMIN_PASSWORD` | Ver [Primer acceso](#primer-acceso). |
| Organización y sitio iniciales | `OPENVMS_DEFAULT_TENANT`, `OPENVMS_DEFAULT_SITE`, `OPENVMS_DEFAULT_SITE_TZ` | Solo se usan si no existe ninguna organización. Zona horaria predeterminada: `America/Argentina/Cordoba`. |
| Web y HTTPS | `WEB_PORT` (8000), `WEB_HTTPS_PORT` (8443), `SITE_ADDRESS`, `OPENVMS_HTTPS_SITE_IPV4`, `OPENVMS_HTTPS_SANS`, `OPENVMS_HTTPS_DEFAULT_SNI`, `OPENVMS_WEB_TLS_CERT_HOST_FILE`, `OPENVMS_WEB_TLS_KEY_HOST_FILE`, `HTTPS_INGRESS_PROXY_CIDRS`, `CREDENTIAL_TRUSTED_PROXY_CIDRS` | Con un dominio público, `SITE_ADDRESS=vms.ejemplo.com` y puertos 80/443: Caddy obtiene el certificado solo. |
| Sesiones y registro | `SESSION_TTL` (12h), `SESSION_IDLE` (2h), `LOG_LEVEL`, `OPENVMS_FEATURES` | `OPENVMS_FEATURES` es una lista separada por comas de feature flags ([`docs/live-view-architecture.md`](docs/live-view-architecture.md)). |
| Notificaciones | `WAHA_API_KEY` | Clave del gateway interno de WhatsApp; cámbiela en producción. |
| ONVIF y agente local | `ONVIF_ALLOWED_CIDRS`, `ONVIF_DISCOVERY_INTERFACES`, `OPENVMS_AGENT_*` | Solo para el perfil opcional `onvif-agent` y la red interna del agente. |
| Metadatos de build | `VERSION`, `COMMIT`, `BUILD_TIME`, `AGENT_VERSION` | Déjelos vacíos: `make` y los scripts los completan. |

El compose define además perfiles opcionales: `--profile mock` (Frigate simulados), `--profile observability` (Prometheus en `:9090` y Grafana en `:3000`, ambos solo en `127.0.0.1`) y `--profile onvif-agent` (agente de borde local).

## Actualizar y volver atrás

Para actualizar use `make deploy` ([`deploy/deploy.sh`](deploy/deploy.sh)); se detiene ante cualquier fallo. En 8 pasos:

1. Preflight: árbol limpio y en `main` (`DEPLOY_BRANCH`), `git pull --ff-only`.
2. Metadatos: commit, versión y migración actual.
3. Volcado protegido de Postgres en `.atl/deploy/<timestamp>/postgres.dump`, verificado con `pg_restore --list`.
4. Etiquetas de rollback `rollback-<timestamp>` en las imágenes `api`, `relay`, `worker` y `web`.
5. Build, 6. `docker compose up -d`, 7. espera de `/health/ready`, 8. resumen (commit, migración antes y después, volcado y comando de rollback).

Variables opcionales: `DEPLOY_SKIP_PULL=1`, `DEPLOY_SKIP_BACKUP=1`, `DEPLOY_DRY_RUN=1` (solo imprime), `DEPLOY_WAIT` (segundos de espera, 120) y `DEPLOY_KEEP=<n>` (etiquetas `rollback-*` por servicio y volcados que se conservan; 5 por defecto, nunca se borra lo recién creado). En el primer despliegue no hay imágenes que etiquetar y no hay rollback de imágenes.

Las migraciones de base se aplican solas al arrancar la API. Para volver atrás:

```bash
make rollback TAG=<timestamp>
```

`make rollback` ([`deploy/rollback.sh`](deploy/rollback.sh)) valida primero que existan las etiquetas, restaura **solo las imágenes** y, si un `docker tag` falla a mitad de camino, devuelve las anteriores sin reiniciar nada. La base **no** se restaura sola: si se aplicaron migraciones nuevas, hágalo a mano con el comando que imprime el despliegue (destructivo, revíselo antes):

```bash
docker compose exec -T postgres pg_restore -U openvms -d openvms --clean --if-exists < .atl/deploy/<timestamp>/postgres.dump
```

Los servicios principales (Postgres, Valkey, NATS, SeaweedFS, Mosquitto, API, relay, worker y web) tienen `restart: unless-stopped`: vuelven solos tras reiniciar el host. Backup manual de la base y del `.env` (contiene la clave maestra): sección 10 de [`Readme.txt`](Readme.txt). Las grabaciones no se respaldan aquí: están en cada Frigate.

## Versiones

- La versión de OpenVMS está en [`VERSION`](VERSION) y la del agente de borde en [`deploy/agent/VERSION`](deploy/agent/VERSION). Imágenes, `make` y los scripts leen de ahí, no de `git describe`.
- El historial está en [`CHANGELOG.md`](CHANGELOG.md); las reglas para subir versión y publicar una release, en [`docs/VERSIONING.md`](docs/VERSIONING.md). `make release-notes [V=0.4.0]` imprime la sección del changelog de una versión.
- La versión se ve en el login (`OpenVMS vX.Y.Z`) y en `/version.json`.

## Agentes y aceleradores

El agente de borde (`edge-agent`) se instala en el host de cada Frigate y entrega métricas, descubrimiento ONVIF y acciones remotas. Se instala desde la web por SSH, con tres variantes:

- **Servidor nuevo** (`POST /api/v1/servers/provision`): instala Frigate, NTP y el agente. Requiere Debian/Ubuntu (x86_64 o arm64, con systemd), SSH como `root` y que el host no tenga ya Frigate. Frigate queda en `/opt/frigate` y las grabaciones en `/mnt/cctv`, que debe ser un sistema de archivos montado; usar el disco del sistema es solo una opción explícita pensada para demos.
- **Solo el agente** en un servidor registrado (`POST /api/v1/servers/{id}/agent/install`): requiere `servers.manage` y `servers.config.secrets`.
- **Actualizar** un agente registrado (`.../agent/update-ssh`, "Actualizar agente" en la web).

La contraseña SSH es transitoria: no se guarda ni se devuelve. La primera huella de clave del host se guarda por servidor, IP y puerto, y un cambio posterior se rechaza; también se puede indicar la huella `SHA256` esperada. Es confianza en el primer contacto, no verificación independiente de la identidad del servidor.

Si el host **ya tiene un agente**, el instalador no lo toca: informa su versión, la compara con la que trae este OpenVMS ("al día" o "desactualizado") y, si está registrado en este OpenVMS, indica usar "Actualizar agente"; si no, avisa que probablemente lo administra otro OpenVMS. Otros fallos informan la causa real (no es root, SO o arquitectura no soportados, archivos de agente existentes, falta systemd, transferencia o activación).

Aceleradores: el agente detecta **NVIDIA**, **Intel OpenVINO**, **Google Coral** (USB y PCIe) y **Hailo-8**, y la configuración Frigate del servidor (modal de detectores) muestra lo detectado y ofrece presets de un clic: TensorRT, Coral USB, Coral PCIe, OpenVINO, Hailo-8 y CPU. El instalador de Frigate usa las variantes `coral`, `tensorrt`, `openvino` y `cpu`.

## Seguridad y red

- **HTTPS web**: en el primer inicio se genera un certificado autofirmado, que se conserva en el volumen `openvms-web-tls`. Defina en `OPENVMS_HTTPS_SANS` todos los nombres e IP con que accederá (separados por comas, sin espacios), por ejemplo `localhost,192.168.1.20,openvms.lan`; `OPENVMS_HTTPS_DEFAULT_SNI` debe ser uno de ellos. El certificado se regenera al cambiar la lista o con menos de 30 días de validez. El navegador no confía solo: hay que instalar el certificado o usar uno propio con `OPENVMS_WEB_TLS_CERT_HOST_FILE` y `OPENVMS_WEB_TLS_KEY_HOST_FILE` (rutas del host; se validan y nunca se reemplazan). Si queda un bloqueo `.generation-lock` en el volumen tras un inicio interrumpido, detenga el contenedor `web`, verifique que nada más use el volumen y quítelo.
- **Canal de control gRPC** (`GRPC_ADDR`, `:9090`): va en texto plano mientras no se definan **ambas** `GRPC_TLS_CERT_FILE` y `GRPC_TLS_KEY_FILE`; la API lo avisa al arrancar (`gRPC control channel is plaintext`). El agente puede usar TLS mutuo. Detalles y orden de migración: [`docs/grpc-tls.md`](docs/grpc-tls.md).
- **Secretos**: las contraseñas de Frigate se guardan cifradas (AES-256-GCM) con `OPENVMS_MASTER_KEY` (también puede pasarse como archivo con `OPENVMS_MASTER_KEY_FILE`). Las contraseñas de usuario usan Argon2id. `.env` debe tener modo 0600.
- Postgres, Valkey, NATS y SeaweedFS solo escuchan en `127.0.0.1`. La auditoría (menú Auditoría) registra ingresos, cambios, vistas en vivo, reproducciones y descargas, y no se puede modificar desde la aplicación.

## Solución de problemas

| Síntoma | Causa y solución |
|---|---|
| `Invalid interpolation format` o `'name' does not match any of the regexes` al levantar | Se usó Compose v1 (`docker-compose`). Instale el plugin v2 y use `docker compose`. |
| `web` no arranca con certificado propio | `OPENVMS_WEB_TLS_CERT_HOST_FILE` y `OPENVMS_WEB_TLS_KEY_HOST_FILE` deben definirse ambas, con archivos legibles, válidos y que coincidan entre sí; si no, el contenedor lo informa y se detiene. |
| `api` o `worker` reiniciándose | Casi siempre Postgres, NATS o Valkey no están sanos. Revise `docker compose ps` y `docker compose logs postgres`. Con las políticas de reinicio se recuperan solos cuando la dependencia vuelve. |
| La API arranca pero no se puede entrar con la contraseña de `.env` | Solo se aplica en la primera creación del admin. Para cambiarla: `vmsctl passwd`. |
| Patentes sin lecturas nuevas tras una caída | La sincronización de objetos continúa por páginas desde el cursor guardado; un backlog grande se drena en varias pasadas. Verifique en Servidores la columna "Eventos". |
| Falla la instalación del agente | El mensaje en la web indica la causa (ver [Agentes y aceleradores](#agentes-y-aceleradores)); el log de la API la registra sin secretos. |
| Verificar estado | `curl -sk https://localhost:8443/health/ready`, `docker compose ps` y `docker compose logs -f api worker`. |

## Desarrollo

Requisitos: Go 1.26 y Node 22 con pnpm (`corepack enable`), además de Docker con Compose v2.

```bash
make dev
```

Levanta Postgres, Valkey, NATS, SeaweedFS, Mosquitto y los Frigate simulados en Docker, y corre la API (`:8080`) y la web con Vite (`:5173`) en la máquina local. `make help` lista el resto: `test`, `test-integration`, `lint`, `generate`, `build`.

Para tener los Frigate simulados con el stack completo: `docker compose --profile mock up -d`. Escuchan en `127.0.0.1:18971` (Helvecia, `admin` / `helvecia-dev`) y `127.0.0.1:28971` (Cayastá, `admin` / `cayasta-dev`); `vmsctl seed-demo` crea la organización de demostración que los usa.

### Estructura

```text
apps/
  api/            API HTTP del control plane (Go)
  worker/         consumidores del bus NATS (Go)
  relay/          media relay para NAT/firewall
  edge-agent/     agente de borde que se instala junto a Frigate
  vmsctl/         CLI de administración
  frigate-mock/   Frigate simulado: API estilo puerto 8971 + reviews por MQTT
  web/            SPA React + Vite + TanStack (PWA)
internal/         dominio, API (api/gen es código generado), store, plataforma
migrations/       migraciones SQL versionadas (goose), embebidas en el binario
packages/
  api-contract/   openapi.yaml: fuente única de verdad de la API
proto/            contratos gRPC
deploy/           Dockerfiles, Caddyfile, instaladores y scripts de despliegue
docs/             PRD, arquitectura y guías
```

### Contrato primero

Ningún endpoint existe sin estar en [`packages/api-contract/openapi.yaml`](packages/api-contract/openapi.yaml) (PRD §75). Después de editarlo, `make generate` regenera el servidor Go (oapi-codegen), el cliente TypeScript (openapi-typescript) y los stubs de Protobuf. CI falla si el código generado no coincide con el contrato.

### Frigate simulado

`frigate-mock` imita lo que OpenVMS usa de Frigate 0.17: login con cookie o Bearer como el puerto autenticado 8971, `/api/version`, `/api/config`, `/api/stats`, `/api/review` con filtros, thumbnails y el ciclo `new → update → end` en `{prefijo}/reviews` por MQTT, con patentes argentinas en las cámaras LPR. Arranca con 24 h de historial para probar reconciliación. Se configura por variables de entorno documentadas en [`apps/frigate-mock/main.go`](apps/frigate-mock/main.go).

## Documentación

[`docs/`](docs/): [PRD](docs/PRD.md), [arquitectura](docs/architecture/decisions.md), [vista en vivo](docs/live-view-architecture.md), [mapas](docs/maps/OPERATIONS.md), [canales de notificación](docs/notification-channels.md), [gRPC TLS](docs/grpc-tls.md), [i18n](docs/i18n.md) y [versionado](docs/VERSIONING.md).
