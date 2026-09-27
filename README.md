# OpenVMS · Federated Frigate VMS

Plataforma central multi-tenant y multi-site que federa múltiples servidores [Frigate](https://frigate.video) autónomos: usuarios y permisos propios, búsqueda global de eventos y patentes, live view multi-servidor, casos, auditoría y monitoreo. Cada Frigate sigue grabando y detectando por su cuenta; el central nunca es dependencia para eso.

- Requisitos del producto: [`docs/PRD.md`](docs/PRD.md)
- Decisiones de stack: [`docs/architecture/decisions.md`](docs/architecture/decisions.md)

## Estado

**MVP (M0 a M6).** Sobre la base de M0 y el inventario con permisos de M1: índice central de eventos y lecturas de patentes de todos los Frigate con backfill tras caídas (M2), búsqueda global de eventos y patentes (M3), login con usuario y contraseña, sesiones, MFA TOTP, usuarios, grupos, permisos y auditoría desde la web (M4), en vivo multi-servidor con vistas guardadas a través del media gateway (M5) y línea de tiempo, reproducción y exportaciones desde el Frigate de origen (M6). Cada Frigate se registra con login (puerto 8971) o sin login (puerto 5000). Guía de instalación: [`Readme.txt`](Readme.txt).

## Arranque rápido

Requisitos: Docker con Compose v2. Para desarrollo local también Go 1.26 y Node 22 con pnpm (`corepack enable`).

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
