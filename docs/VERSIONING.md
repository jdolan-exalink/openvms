# Versionado

OpenVMS usa [Versionado Semántico](https://semver.org/lang/es/) (`MAJOR.MINOR.PATCH`) y
lleva el historial en [`CHANGELOG.md`](../CHANGELOG.md) con el formato
[Keep a Changelog](https://keepachangelog.com/es-ES/1.1.0/).

## Fuentes únicas de verdad

| Componente | Archivo | Valor actual |
| --- | --- | --- |
| OpenVMS (API, relay, worker, web) | `VERSION` | `0.4.0` |
| Agente de borde (`edge-agent`) | `deploy/agent/VERSION` | `0.4.5` |

Cada archivo contiene solo la versión y un salto de línea. `make`, `deploy/deploy.sh`,
`deploy/install.sh` y los Dockerfiles leen estos archivos; nadie debe escribir una versión
a mano en otro lugar. Si `VERSION` o `AGENT_VERSION` se definen como variable de entorno, esa
definición tiene prioridad (útil para pruebas puntuales). El commit se registra aparte
(`COMMIT`) y no forma parte del número de versión.

La prueba `internal/platform/buildinfo/release_consistency_test.go` falla si los archivos no
son `MAJOR.MINOR.PATCH`, si la última release de `CHANGELOG.md` no coincide con `VERSION`
o si el Makefile, el compose o los Dockerfiles vuelven a fijar una versión.

La numeración de OpenVMS se reinició en `0.4.0` (2026-10-19). La release de GitHub `v0.3.0`
queda como historia.

## Cuándo subir cada número

Mientras `MAJOR` sea 0 la API todavía puede cambiar, pero cada cambio incompatible se
anota en el changelog.

### OpenVMS

- **MAJOR**: cambio incompatible para quien opera o integra el sistema: endpoints o
  contrato de la API eliminados o con otro significado, migración que no admite volver
  atrás, cambio de requisitos que obliga a intervenir (por ejemplo, una versión mínima de
  Frigate o de Docker Compose nueva).
- **MINOR**: funcionalidad nueva compatible o cambio significativo visible: una pantalla o
  módulo nuevo, endpoints nuevos, migraciones aditivas, cambios de despliegue relevantes.
- **PATCH**: correcciones sin funcionalidad nueva, ajustes de documentación que se
  publican con una release, actualizaciones de dependencias por seguridad.

Mientras `MAJOR` sea 0, un cambio incompatible sube `MINOR`.

### Agente de borde

El agente tiene su propia versión. Se sube cuando cambia el binario del agente o su
contrato de instalación (`deploy/agent/`: instalador, plantillas de compose, servicio
systemd, formato de configuración o protocolo con la API).

- **MAJOR**: el agente ya instalado deja de funcionar sin intervención o cambia el
  protocolo de forma incompatible.
- **MINOR**: capacidad nueva del agente o del instalador, compatible.
- **PATCH**: corrección en el agente o el instalador.

Un cambio de OpenVMS que no toca el agente no cambia `deploy/agent/VERSION`. Un cambio del
agente se anota en la sección de la release de OpenVMS que lo incluye (`Agent X.Y.Z`).

#### La versión del agente sube sola

Una vez publicada una versión del agente (existe el tag `agent-vX.Y.Z`), cualquier cambio en lo
que el agente incluye exige una versión nueva. "Lo que el agente incluye" es todo archivo que no
sea de prueba ni documentación en un paquete del que se compila `apps/edge-agent`
(`go list -deps`) y todo lo que hay en `deploy/agent/` salvo `VERSION`. Mientras la versión
vigente no tenga tag, los cambios se suman a ella.

- `tools/agent_version.sh check [BASE]` aplica la regla y falla si falta subir la versión;
  `make agent-version-check` lo corre junto con su prueba.
- `make hooks` instala un hook de pre-commit que sube el patch y agrega la línea al changelog en
  el mismo commit cuando hace falta (`check --fix`).
- `make agent-bump LEVEL=minor NOTE="qué cambió"` sube la versión a mano.
- El job de CI «Agent version» corre la regla en cada PR contra `main`, también en PRs apilados.
- Publicar el agente: tag `agent-vX.Y.Z` sobre un commit de `main`. El workflow «Release agent»
  verifica que el tag coincida con `deploy/agent/VERSION`, compila amd64 y arm64 y publica los
  binarios, `SHA256SUMS` e `install-agent.sh` (ver `docs/agent/install.md`). Desde el tag la versión
  cuenta como publicada.
- La versión sale de un solo archivo: `deploy/agent/VERSION` se incrusta en el binario de OpenVMS
  (la que espera la API) y se inyecta en el agente al compilar (la que reporta).

## Cómo publicar una release

1. En un PR, actualizar `VERSION` (y `deploy/agent/VERSION` si cambió el agente) y mover el
   contenido de `[Unreleased]` en `CHANGELOG.md` a una sección nueva
   `## [X.Y.Z] - AAAA-MM-DD`, agrupada en Added, Changed, Fixed (y Removed o Security si
   aplica). Dejar `[Unreleased]` vacío arriba.
2. Verificar con `go test ./internal/platform/buildinfo/`.
3. Mergear el PR a `main`.
4. Crear el tag anotado sobre ese commit y subirlo:
   `git tag -a vX.Y.Z -m "OpenVMS X.Y.Z" && git push origin vX.Y.Z`.
5. Crear la release de GitHub desde el tag usando la sección del changelog:
   `make release-notes V=X.Y.Z > /tmp/notes.md` y
   `gh release create vX.Y.Z --title "OpenVMS X.Y.Z" --notes-file /tmp/notes.md`.

Durante el desarrollo, cada PR con un cambio visible agrega una línea en `[Unreleased]`.
La versión solo sube al preparar una release, no en cada PR.
