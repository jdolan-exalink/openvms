# OpenVMS · Federated Frigate VMS

Plataforma central multi-tenant y multi-site que federa múltiples servidores [Frigate](https://frigate.video) autónomos: usuarios y permisos propios, búsqueda global de eventos y patentes, live view multi-servidor, casos, auditoría y monitoreo. Cada Frigate sigue grabando y detectando por su cuenta; el central nunca es dependencia para eso.

- Requisitos del producto: [`docs/PRD.md`](docs/PRD.md)
- Decisiones de stack: [`docs/architecture/decisions.md`](docs/architecture/decisions.md)

## Estado

**MVP (M0 a M6).** Sobre la base de M0 y el inventario con permisos de M1: índice central de eventos y lecturas de patentes de todos los Frigate con backfill tras caídas (M2), búsqueda global de eventos y patentes (M3), login con usuario y contraseña, sesiones, MFA TOTP, usuarios, grupos, permisos y auditoría desde la web (M4), en vivo multi-servidor con vistas guardadas a través del media gateway (M5) y línea de tiempo, reproducción y exportaciones desde el Frigate de origen (M6). Cada Frigate se registra con login (puerto 8971) o sin login (puerto 5000). Guía de instalación: [`Readme.txt`](Readme.txt).

Maps incorpora edición de posiciones, tipos/FOV, zonas, centro de monitoreo y navegación por
sitio. El cierre operativo agrega estados de inventario/ubicación, recuperación de errores y
flujos diferenciados de Live, investigación y resumen actual. **Implementado no equivale a
validado en producción:** los checks locales de web, Go (race/integración) y rendimiento
sintético pasaron; la aceptación con la cuenta instalada y el despliegue completo siguen pendientes. Guía: [`docs/maps/OPERATIONS.md`](docs/maps/OPERATIONS.md). Estado y evidencia
histórica: [`odd/tasks/maps.md`](odd/tasks/maps.md). El editor avanzado de Frigate y las
capacidades fuera del MVP mantienen sus propios pendientes; no se declaran cerrados aquí.

## Arranque rápido

Requisitos: Docker con Compose v2. Para desarrollo local también Go 1.26 y Node 22 con pnpm (`corepack enable`).

Versión de Frigate soportada: **0.17 o posterior**. OpenVMS rechaza al registrar o conectar un servidor con Frigate 0.16 o anterior e informa la versión detectada; actualiza ese Frigate antes de registrarlo.

```bash
cp .env.example .env      # opcional
docker compose up -d      # o: make up
```

| URL | Qué es |
|---|---|
| http://localhost:8000 | Web (Caddy sirve la SPA y enruta la API) |
| http://localhost:8000/docs | Referencia de la API |
| http://localhost:8000/health/ready | Estado de dependencias |
| http://localhost:18971 | Frigate simulado Helvecia (`admin` / `helvecia-dev`) |
| http://localhost:28971 | Frigate simulado Cayastá (`admin` / `cayasta-dev`) |

Creá el administrador de plataforma (con contraseña para entrar a la web) y, si querés, el tenant de demostración (sitios Helvecia, Cayastá y Santa Rosa, los dos Frigate simulados, el grupo Operator-A del test de aceptación §129 y un supervisor con una cámara denegada):

```bash
docker compose exec api /vmsctl bootstrap -password "Clave-Larga-2026"   # admin / esa contraseña; imprime también un token de API
docker compose exec api /vmsctl seed-demo    # imprime los tokens de "operador" y "supervisor"
docker compose exec api /vmsctl token -user operador   # otro token para un usuario existente
```

La contraseña de cada Frigate se guarda cifrada con `OPENVMS_MASTER_KEY` (32 bytes en base64). El valor de `docker-compose.yml` es solo para desarrollo: en producción generalo con `openssl rand -base64 32` o pasalo como archivo con `OPENVMS_MASTER_KEY_FILE`.

Observabilidad opcional: `docker compose --profile observability up -d` agrega Prometheus (`:9090`) y Grafana (`:3000`).

### Desarrollo con recarga

```bash
make dev
```

Levanta Postgres, Valkey, NATS, SeaweedFS, Mosquitto y los Frigate simulados en Docker, y corre la API (`:8080`) y la web con Vite (`:5173`) en tu máquina.

`make help` lista el resto: `test`, `test-integration`, `lint`, `generate`, `build`.

### HTTPS directo en la LAN

`docker compose up` genera en el primer inicio un certificado TLS autofirmado y lo conserva
en el volumen Docker `openvms-web-tls`. Configurá explícitamente todas las direcciones que
usarás en el navegador; el contenedor no puede descubrir de forma fiable las IP de la interfaz
del host:

```dotenv
OPENVMS_HTTPS_SANS=localhost,192.168.1.20,openvms.lan,fd00::20
```

Separá los SAN por comas, sin espacios. Al cambiar la lista o cuando queden menos de 30 días
de validez, el próximo inicio genera un nuevo certificado. El valor predeterminado solo cubre
`localhost`, `127.0.0.1` y `::1`. HTTPS sigue disponible en el puerto 8443; HTTP en 8000 no
cambia. El certificado autofirmado no hace que el navegador confíe automáticamente en él:
hay que instalar/confiar el certificado y acceder con un nombre/IP incluido en sus SAN.
`OPENVMS_HTTPS_DEFAULT_SNI` debe ser uno de los nombres incluidos en `OPENVMS_HTTPS_SANS`;
por defecto es `OPENVMS_HTTPS_SITE_IPV4` si está definida (que también es el SAN de respaldo)
o `localhost` en caso contrario.

El generador espera hasta 30 segundos si otra instancia está creando el certificado y luego
falla con un mensaje de recuperación; nunca elimina automáticamente un bloqueo porque podría
pertenecer a un inicio todavía activo. Si el bloqueo persiste, detené el contenedor web y
verificá que no haya otro proceso de inicio usando ese volumen antes de quitar
`.generation-lock` del volumen `openvms-web-tls` y volver a iniciar.
Con el bloqueo adquirido, el siguiente inicio también limpia directorios temporales de
generaciones interrumpidas y conserva intacta la generación activa.

Para conservar un certificado y una clave externos, definí ambos
`OPENVMS_WEB_TLS_CERT_HOST_FILE` y `OPENVMS_WEB_TLS_KEY_HOST_FILE` con rutas del host. El
contenedor valida que sean legibles, válidos y que coincidan; no reemplaza esos archivos.

## Despliegue

`make deploy` actualiza el stack local en un solo paso y se detiene ante cualquier fallo: verifica que el árbol esté limpio y en `main`, hace `git pull --ff-only`, genera un volcado protegido de Postgres en `.atl/deploy/<timestamp>/` (verificado con `pg_restore --list`), etiqueta las imágenes actuales como `rollback-<timestamp>`, construye con `COMMIT`, `BUILD_TIME` y `VERSION`, ejecuta `docker compose up -d` y espera `/health/ready`. Al terminar imprime el commit, la migración antes y después, el volcado y el comando de rollback.

Variables opcionales: `DEPLOY_BRANCH`, `DEPLOY_SKIP_PULL=1`, `DEPLOY_SKIP_BACKUP=1`, `DEPLOY_DRY_RUN=1` (solo imprime los comandos). Para volver atrás: `make rollback TAG=<timestamp>` restaura las imágenes sin tocar la base de datos; si se aplicaron migraciones nuevas, restaura también el volcado con el comando que imprime el despliegue.

## Estructura

```text
apps/
  api/            API HTTP del control plane (Go)
  worker/         consumidores del bus NATS (Go)
  frigate-mock/   Frigate simulado: API estilo puerto 8971 + reviews por MQTT
  web/            SPA React + Vite + TanStack
internal/
  api/            router y handlers; api/gen es código generado del contrato
  frigatemock/    lógica del simulador
  health/         checks de dependencias
  platform/       config, logging, HTTP, Postgres, Valkey, NATS, S3, OpenTelemetry
packages/
  api-contract/   openapi.yaml: fuente única de verdad de la API
migrations/       migraciones SQL versionadas (goose), embebidas en el binario
deploy/           Dockerfiles, Caddyfile y configs de servicios
docs/             PRD y decisiones de arquitectura
```

## Contrato primero

Ningún endpoint existe sin estar en [`packages/api-contract/openapi.yaml`](packages/api-contract/openapi.yaml) (PRD §75). Después de editarlo:

```bash
make generate   # regenera el servidor Go (oapi-codegen) y el cliente TS (openapi-typescript)
```

CI falla si el código generado no coincide con el contrato.

## Frigate simulado

`frigate-mock` imita lo que OpenVMS usa de Frigate 0.17: login con cookie o Bearer como el puerto autenticado 8971, `/api/version`, `/api/config`, `/api/stats`, `/api/review` con filtros, thumbnails y el ciclo `new → update → end` en `{prefijo}/reviews` por MQTT, con patentes argentinas en las cámaras LPR. Arranca con 24 h de historial para probar reconciliación. Se configura por variables de entorno documentadas en [`apps/frigate-mock/main.go`](apps/frigate-mock/main.go).
