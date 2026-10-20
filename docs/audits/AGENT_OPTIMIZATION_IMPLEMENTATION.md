# Implementación de Optimización de Agentes, Engram y MCP (Etapas 0 a 7)

**Fecha de ejecución:** 2026-10-20  
**Repositorio:** `/root/openvms` (`main` @ `6d39b75`, versión `0.4.0`)  
**Backup de Seguridad:** `/root/.agent-optimization-backups/20261020T181320Z` (`SHA256SUMS` verificado)

---

## 1. Resumen de cambios implementados por etapa

### Etapa 0 — Preparación y Baseline
- Se verificó el estado limpio de Git en `/root/openvms` (`main...origin/main`, conservando intacto `/root/openvms/data/`).
- Se creó el directorio protegido `/root/.agent-optimization-backups/20261020T181320Z` (`chmod 700`) con copias íntegras y manifiesto `SHA256SUMS` verificado de:
  - `/root/.codex/` (`AGENTS.md`, `config.toml`, `engram-instructions.md`, `engram-compact-prompt.md`, `hooks.json`, `skills/`, `rules/`, `plugins/`)
  - `/root/.claude/` (`CLAUDE.md`, `settings.json`, `.claude.json`, `agents/`, `skills/`, `commands/`, `output-styles/`)
  - `/root/.gemini/` (`GEMINI.md`, `settings.json`, `system.md`, `antigravity/`, `config/`)
  - `/root/.engram/` (`engram.db`, `engram.db-wal`, `engram.db-shm`, `protocol-mode.json`, `.instance-id` + snapshot consistente `engram-consistent-snapshot.db` con `PRAGMA integrity_check = ok` y `1,435` observaciones).
- Se registró el baseline en [AGENT_OPTIMIZATION_BASELINE.md](file:///root/openvms/docs/audits/AGENT_OPTIMIZATION_BASELINE.md).

### Etapa 1 — Reducción de instrucciones globales (`< 8 KB`) y extracción a skills bajo demanda
- Se extrajeron los manuales completos de `sdd-orchestrator`, `native-compact-review` y `agent-routing` sin pérdida de reglas al skill indexado bajo demanda `orchestrator-reference`:
  - `/root/.claude/skills/orchestrator-reference/SKILL.md` (+ `references/{claude-sdd-orchestrator,codex-sdd-orchestrator,gemini-sdd-orchestrator,odd-routing-full,odd-routing-pre-optimization}.md`)
  - `/root/.codex/skills/orchestrator-reference/SKILL.md` (+ `references/*`)
  - `/root/.gemini/config/skills/orchestrator-reference/SKILL.md` y `/root/.gemini/skills/orchestrator-reference/SKILL.md` (+ `references/*`)
  - Se sincronizó `_shared/sdd-orchestrator-workflow.md` (`21,991 B`) en `/root/.codex/skills/_shared/` y `/root/.gemini/config/skills/_shared/`.
  - Se actualizó `.atl/skill-registry.md` mediante `gentle-ai skill-registry refresh --force --cwd /root/openvms`, registrando `orchestrator-reference` como skill descubrible por los delegadores.
- Se refactorizaron los tres archivos globales conservando todos los marcadores `<!-- gentle-ai:... -->`:
  - `/root/.codex/AGENTS.md`: **`100,234 B` → `7,992 B`** (`-92.0%`).
  - `/root/.claude/CLAUDE.md`: **`69,471 B` → `7,992 B`** (`-88.5%`).
  - `/root/.gemini/GEMINI.md`: **`87,957 B` → `7,992 B`** (`-90.9%`).
- Se eliminó `model_instructions_file = "/root/.codex/engram-instructions.md"` de `/root/.codex/config.toml` y se redujo `/root/.codex/engram-instructions.md` (`9,384 B` → `283 B`) para suprimir la doble inyección del protocolo Engram en Codex.

### Etapa 2 — Optimización de Subagentes (`Parent-Owned Memory` y `Context Pack` obligatorio)
- Se incorporó el contrato **`Parent-Owned Memory`** tanto en las instrucciones globales (`CLAUDE.md`, `AGENTS.md`, `GEMINI.md`) como en `_shared/persistence-contract.md` de los tres entornos:
  - El agente principal (padre) es el único responsable de `mem_current_project`, `mem_context`, `mem_review`, `mem_save`, `mem_judge` y `mem_session_summary`.
  - Los subagentes ejecutores y verificadores operan en modo **Memory-Free por defecto** (`0` llamadas automáticas a Engram), salvo instrucción explícita del padre o fases SDD con `artifact_store.mode` en `engram`/`hybrid`.
- Se estandarizó el bloque obligatorio **`Context Pack`** (`TASK`, `SCOPE`, `CONTEXT`, `SYMBOLS`, `CONSTRAINTS`, `ACCEPTANCE`, `OUTPUT`) para toda delegación a subagentes.

### Etapa 3 — Optimización de Engram y Persistencia ODD
- **Separación Git vs. Engram**:
  - **Git (`odd/tasks/<feature-name>.md`)** queda establecido como la única fuente de verdad para listas de tareas y progreso ODD/SDD, eliminando el espejado de documentos `odd/<feature-name>/tasks` de 30–49 KB en Engram.
  - **Engram** se reserva para decisiones arquitectónicas, descubrimientos no obvios, restricciones técnicas, bugfixes relevantes y continuidad entre sesiones (`<= 1,500 B` por observación).
- **Recuperación selectiva verificada empíricamente**:
  - Uso de `mem_context(project="openvms", compact=true)` (`2,144 B` frente a `8,423 B` del modo por defecto: **`-74.5%`**).
  - Uso de `mem_search(query="...", project="openvms", match_mode="all", limit=5)` omitiendo `response_format="compact"`, dado que en Engram `3.4.0` el formato `response_format="compact"` devuelve un sobre JSON de `15,910 B` frente a `7,467 B` del formato texto legado.
- **Disciplina de `mem_judge`**:
  - Se restringe la invocación de `mem_judge` exclusivamente a contradicciones semánticas reales en registros `architecture` o `decision` (`conflicts_with` / `supersedes`), evitando las rondas automáticas sobre candidatos no conflictivos.
- **Limitación documentada de Engram `3.4.0` (sin modificar el binario ni la base)**:
  - El binario `/usr/local/bin/engram` (`3.4.0`) ejecuta el detector de candidatos de conflicto internamente en cada `mem_save` sin un flag de configuración para desactivar `judgment_required` en el servidor, y `mem_session_summary` no expone el parámetro `topic_key` en su esquema JSON (`mem_session_summary.json`). La mitigación compatible aplicada opera en la capa de protocolo del agente (solo el padre invoca `mem_session_summary` al cierre real de sesión y omite `mem_judge` cuando no hay contradicción real).

### Etapa 4 — Optimización de Navegación del Monorepo
- Se creó [/root/openvms/.rgignore](file:///root/openvms/.rgignore) (`152 B`) excluyendo de búsquedas generales:
  - `internal/api/gen/`, `internal/store/db/*.sql.go`, `apps/web/src/api/schema.d.ts`, `gen/`, `packages/api-contract/openapi.yaml`, `odd/tasks/`, `models/*.onnx`, `*.mp4`, `*.docx`.
- Se verificó que las búsquedas explícitas sobre esos archivos (`rg -n ServerFrigateConfig packages/api-contract/openapi.yaml`, `rg -n ServerFrigateConfig internal/api/gen/api.gen.go`, `rg -n floor odd/tasks/maps.md`) siguen funcionando sin restricción.
- Se crearon archivos regulares (sin symlinks) [/root/openvms/AGENTS.md](file:///root/openvms/AGENTS.md) (`4,678 B`) y [/root/openvms/CLAUDE.md](file:///root/openvms/CLAUDE.md) (`4,678 B`) con el mapa de las 7 aplicaciones en `apps/`, los 30 paquetes en `internal/`, las fuentes de verdad de contratos, comandos de build/test y reglas de navegación por módulo.

### Etapa 5 — Integración de CodeGraph en Codex
- Se verificó el servidor MCP `codegraph serve --mcp` (`codegraph 1.6.0`, índice `.codegraph/` actualizado con `1,129` archivos, `19,816` nodos y `66,221` aristas).
- Se registró `[mcp_servers.codegraph]` (`command = "codegraph"`, `args = ["serve", "--mcp"]`) en `/root/.codex/config.toml` preservando todas las configuraciones existentes.
- Se incluyó la guía compacta `codegraph-guidance` en `/root/.codex/AGENTS.md` y en `/root/openvms/AGENTS.md`.

### Etapa 6 — Optimización de Hooks, Gentle AI y Antigravity MCP
- **Claude Code (`/root/.claude/settings.json`)**:
  - Se movió `gentle-ai skill-registry refresh --quiet --cwd "${CLAUDE_PROJECT_DIR:-$PWD}" || true` (eliminando el flag `--no-gitignore`) desde `UserPromptSubmit` hacia `SessionStart` (`startup|resume|clear|compact`), evitando regeneraciones en cada prompt del usuario y evitando escanear directorios ignorados por Git.
  - Se eliminó el hook roto `PreToolUse` (`gentle-ai sdd-preflight-hook --agent claude-code`), ya que el subcomando `sdd-preflight-hook` no existe en `gentle-ai 4.2.0` y fallaba con código de error en cada lanzamiento de subagente (`129–714 ms`).
  - Se preservaron intactos `gentle-ai review stop-hook --agent claude-code` (`SessionStart` y `Stop`), `codegraph prompt-hook` (`UserPromptSubmit`) y la telemetría asíncrona (`Stop` y `SubagentStop`).
- **Codex (`/root/.codex/hooks.json`)**:
  - Se preservó intacto `/root/.codex/hooks.json` para no invalidar el `trusted_hash` (`sha256:ee482040d324265de9bff0ee883aab2dad7b181a4a7e07c0250cfebe0dc32dd6`) registrado en `/root/.codex/config.toml`.
- **Antigravity (`/root/.gemini/config/`)**:
  - Tras confirmar que `/root/.gemini/config/mcp_config.json` y `/root/.gemini/config/plugins/gentle-ai-engram/mcp_config.json` se cargaban simultáneamente (generando el duplicado `gentle-ai-engram_engram`), se consolidó `/root/.gemini/config/mcp_config.json` con `context7`, `engram` (`--tools=agent`) y `codegraph`, y se dejó `"mcpServers": {}` en `/root/.gemini/config/plugins/gentle-ai-engram/mcp_config.json`.
  - Se optimizó el mensaje `PreInvocation` en `/root/.gemini/config/plugins/gentle-ai-engram/hooks.json` para indicar carga bajo demanda de esquemas MCP en lugar de forzar la lectura de los 14 archivos JSON en cada turno.
- **Estrategia documentada para reubicar `.atl/*deploy*` (`942 MB`) sin borrar ni mover archivos automáticamente**:
  - Actualmente `.atl/` contiene `530 MB` en `.atl/deploy`, `157 MB` en `.atl/onvif-deploy`, `129 MB` en `.atl/demo-cleanup` y `128 MB` en `.atl/face-deploy`.
  - Para reubicarlos de forma segura cuando el operador lo autorice:
    1. Crear `/var/backups/openvms/atl-dumps` con `chmod 700`.
    2. Mover los tres directorios históricos de sesiones pasadas (`mv /root/openvms/.atl/{demo-cleanup,face-deploy,onvif-deploy} /var/backups/openvms/atl-dumps/`), liberando `414 MB` de inmediato.
    3. En `deploy/deploy.sh`, parametrizar `DEPLOY_BACKUP_DIR=${DEPLOY_BACKUP_DIR:-/var/backups/openvms/deploy}` para que los futuros dumps de `pg_dump` queden fuera del árbol del repositorio.

---

## 2. Etapa 7 — Plan de Mantenimiento de SQLite (Pendiente de autorización explícita; probado en copia aislada)

> **IMPORTANTE:** No se ejecutó ninguna escritura, cambio de esquema, `VACUUM`, `optimize`, checkpoint ni `PRAGMA` sobre la base productiva `/root/.engram/engram.db` (`1,435` observaciones activas intactas, `PRAGMA integrity_check = ok`).

### 2.1 Resultados medidos sobre copia aislada (`/tmp/engram-isolated-bench.db`, eliminada tras la prueba)

| Paso probado en copia aislada | `mem_context` SQL (Mediana) | `mem_search` FTS5 (Mediana) | Segmentos `observations_fts_data` | Plan de consulta (`EXPLAIN QUERY PLAN`) |
|---|---:|---:|---:|---|
| **0. Estado actual (sin cambios)** | `2.278 ms` | `27.349 ms` | `2,394` (`9,652,675 B`) | `SEARCH observations USING INDEX idx_obs_project` + **`USE TEMP B-TREE FOR ORDER BY`** |
| **1. Crear índice parcial `idx_obs_project_active_updated`** | **`0.055 ms`** (`41.4x` más rápido) | `5.390 ms` | `2,394` (`9,652,675 B`) | `SEARCH observations USING INDEX idx_obs_project_active_updated (project=?)` (**sin temp B-tree**) |
| **2. Archivado lógico (`deleted_at`) de `740` `session_summary` antiguos (conservando los últimos 5 por proyecto) + `FTS5 optimize`** | **`0.028 ms`** (`81.4x` más rápido) | **`0.578 ms`** (`47.3x` más rápido) | **`968` (`3,910,951 B`, `-59.5%`)** | `SEARCH observations USING INDEX idx_obs_project_active_updated (project=?)` |

### 2.2 Procedimiento propuesto para ejecución futura (cuando se autorice)

1. **Crear índice parcial compuesto (No destructivo, riesgo nulo):**
   ```sql
   CREATE INDEX IF NOT EXISTS idx_obs_project_active_updated
   ON observations(project, updated_at DESC)
   WHERE deleted_at IS NULL;
   ```
2. **Archivado lógico (`deleted_at`, reversible al 100%) de `session_summary` redundantes:**
   - Conservar activos los últimos 5 `session_summary` de `openvms` y los últimos 5 de `root`, marcando `deleted_at = datetime('now')` en los `740` resúmenes históricos anteriores (la mayoría generados por subagentes). Ningún registro se borra físicamente.
3. **Revisión asistida de las 35 relaciones `supersedes` y trackers `odd/*/tasks` cerrados:**
   - No borrar ni archivar automáticamente todas las relaciones `supersedes`: primero listar los 35 pares (`source_id` → `target_id`) y verificar que la observación nueva contenga todo el contexto técnico relevante antes de marcar `deleted_at` en la observación reemplazada.
   - Para las observaciones `odd/<slug>/tasks` cuyos archivos `odd/tasks/<slug>.md` ya están commiteados y cerrados en `main`, marcar `deleted_at = datetime('now')` una vez verificado el archivo en Git.
4. **Compactación del índice FTS5 y Checkpoint WAL:**
   ```sql
   INSERT INTO observations_fts(observations_fts) VALUES('optimize');
   PRAGMA wal_checkpoint(TRUNCATE);
   ```
5. **Evaluación de `PRAGMA synchronous = NORMAL`:**
   - En modo `WAL`, `synchronous = NORMAL (1)` garantiza atomicidad y consistencia ante cualquier fallo del proceso (`engram` o el agente), y solo podría perder el último commit ante un corte eléctrico súbito del kernel/host. Dado que `engram` abre conexiones desde su propio binario Go, mantener el valor por defecto del binario salvo que se configure en la inicialización de Engram.
