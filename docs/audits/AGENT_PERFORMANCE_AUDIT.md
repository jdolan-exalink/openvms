# Auditoría de Rendimiento de Agentes, MCP y Engram en OpenVMS (OpenBMS)

**Fecha de auditoría:** 2026-10-20  
**Repositorio auditado:** `/root/openvms` (`HEAD` en `main` @ `6d39b75`, versión `0.4.0`)  
**Alcance:** Diagnóstico técnico no destructivo de extremo a extremo sobre Claude Code (`2.1.303`), OpenAI Codex (`0.163.0`), Google Antigravity, Engram (`3.4.0` + SQLite WAL) y la estructura del monorepo OpenVMS.

---

## 1. Resumen ejecutivo

> **Conclusión principal (basada en evidencia):**  
> **SQLite y el binario de Engram NO son lentos a nivel de base de datos ni de transporte MCP** (las consultas SQLite tardan entre **0.003 ms y 31.4 ms**, y una llamada completa MCP sobre `stdio` tarda **21 ms – 46 ms**).  
> La degradación severa en los tiempos de ejecución y el consumo masivo de tokens (**6.53 mil millones de tokens de entrada acumulados** entre Claude Code y Codex) provienen de **cinco cuellos de botella de protocolo, contexto y orquestación**:

1. **Sobrecarga de instrucciones estáticas globales en cada turno y subagente (`36.4k – 74.3k tokens` en el Turno 1 antes de leer un solo archivo):**
   - `/root/.codex/AGENTS.md` pesa **100,234 bytes (~25,058 tokens)** y se carga junto con `/root/.codex/engram-instructions.md` (**9,384 bytes / ~2,346 tokens**), **duplicando** el protocolo de Engram en cada sesión y en cada subagente de Codex.
   - `/root/.claude/CLAUDE.md` pesa **69,471 bytes (~17,367 tokens)** y `/root/.gemini/GEMINI.md` pesa **87,957 bytes (~21,989 tokens)**. Entre el **85% y el 93%** de esos archivos corresponde a prosa de orquestación (`sdd-orchestrator` + `agent-routing`), con **0 bytes** de mapa arquitectónico específico de OpenVMS.
2. **Multiplicador de llamadas MCP por el protocolo ODD/Engram (`mem_judge`, `mem_session_summary` y trackers `odd/*/tasks` de hasta 49 KB):**
   - En Codex se registraron **5,595 llamadas MCP a Engram** (devolviendo **21.30 MB / ~5.3M tokens** de salida) y en Claude Code **558 llamadas**.
   - Cada `mem_save` dispara hasta 3 candidatos de conflicto y obliga al agente a ejecutar rondas adicionales de `mem_judge`. Se ejecutaron **2,760 llamadas a `mem_judge`** en la base (`372` en Claude Code —la 2.ª herramienta más invocada después de `Bash`— y `1,071` en Codex) con **0 conflictos reales detectados (`0 / 2,760` `conflicts_with`)**.
   - El protocolo ODD almacena listas de tareas completas (`odd/<slug>/tasks`) como observaciones gigantes en Engram (hasta **49,216 bytes** y **64 revisiones** por registro), duplicando los archivos que ya existen en `odd/tasks/*.md` del repositorio. Solo 5 observaciones (`#1086`, `#458`, `#120`, `#1326`, `#917`) fueron descargadas **187 veces** vía `mem_get_observation` entre agentes padres y subagentes (~**5.1 MB** de texto reinyectado).
3. **Tormenta de ceremonias y re-exploración en subagentes (hasta `84.7%` de lecturas redundantes):**
   - En Codex, los **173 subagentes** heredan las instrucciones globales completas y ejecutan por su cuenta la ceremonia de inicio y cierre de sesión de Engram: realizaron **3,445 llamadas a Engram** (`680 mem_save`, `655 mem_judge`, `557 mem_session_summary`, `368 mem_current_project`, `333 mem_search`, `325 mem_get_observation`, `284 mem_review`, `227 mem_context`). En un solo árbol de ejecución (`Parent 01a109fc` con 23 subagentes) se dispararon **760 llamadas a Engram** y el **84.7% de los accesos a archivos (300/354)** fueron redundantes.
   - En Claude Code (`176` subagentes), la mediana de exploración antes del primer `Edit`/`Write` es de **165.4 segundos** (**23.5 llamadas a herramientas**), con un **34.5% a 44.8% de lecturas de archivos duplicadas** entre subagentes de la misma sesión.
4. **Asimetría de herramientas de navegación (`CodeGraph` ausente en Codex y subutilizado en Claude Code) + contaminación por archivos generados y `odd/tasks/*.md`:**
   - `/root/.codex/config.toml` **no tiene configurado `codegraph` como servidor MCP** y `/root/.codex/AGENTS.md` **no incluye la sección `codegraph-guidance`**. En consecuencia, Codex hizo **0 llamadas a CodeGraph** y ejecutó **10,314 comandos de shell** (`4,460 sed`, `2,218 cat`, `1,950 rg`, `1,164 grep`, `522 find`).
   - El repositorio carece de `.rgignore`, `.ignore`, `.claudeignore` y `.codexignore`. El **25% de las líneas de código trackeadas** (`67,375` líneas / `2.12 MB` en `internal/api/gen/api.gen.go`, `internal/store/db/*.sql.go`, `apps/web/src/api/schema.d.ts`, `gen/`) más `60` archivos `odd/tasks/*.md` (`958 KB`) contaminan las búsquedas por texto, inflando los resultados de `rg` hasta en un **85.3% de bytes innecesarios** (y hasta **332 KB / ~83k tokens** si un agente ejecuta `grep -rn` sobre `api.gen.go`).
5. **Contaminación del índice FTS5 de Engram por `session_summary` (52.3% de la tabla) y fragmentación de versiones:**
   - **750 de las 1,435 observaciones activas (52.3% de las filas, 55.8% de los bytes)** son resúmenes `session_summary` sin `topic_key` (`687` con el título idéntico `"Session summary: openvms"`, alimentados en gran parte por los `557` cierres de subagentes en Codex).
   - Las **533 actualizaciones** de documentos `odd/*/tasks` inflaron la tabla virtual `observations_fts_data` a **9.65 MB** (`2,394` bloques para solo `2.29 MB` de texto activo) y `observation_versions` a **1.13 MB**. Además, las **35 observaciones marcadas como `supersedes`** siguen activas (`deleted_at IS NULL`) y continúan apareciendo en `observations_fts`.

---

## 2. Inventario del entorno (Fase 1)

### 2.1 Versiones de herramientas y binarios instalados

| Componente | Ruta del binario | Versión detectada | Rol en el entorno |
|---|---|---|---|
| **Claude Code** | `/root/.local/bin/claude` | `2.1.303` | Agente CLI principal y subagentes (`Agent`) |
| **OpenAI Codex CLI** | `/root/.local/bin/codex` | `0.163.0` (modelo `gpt-6-luna`, `reasoning_effort = "high"`) | Agente CLI principal y subagentes (`spawn_agent`, `max_depth=2`, `max_threads=4`) |
| **Engram** | `/usr/local/bin/engram` | `3.4.0` | Servidor MCP de memoria persistente respaldado por SQLite |
| **Gentle AI** | `/usr/local/bin/gentle-ai` | `4.2.0` | Orquestador de hooks (`sdd-preflight-hook`, `review stop-hook`, `skill-registry`, `telemetry`) |
| **CodeGraph** | `/usr/bin/codegraph` | `1.6.0` | Grafo estructural de código (SQLite WAL en `.codegraph/`, 85.45 MB) |
| **Go** | `/usr/local/bin/go` | `go1.26.9 linux/amd64` | Toolchain backend de OpenVMS |
| **Node.js / npm** | `/usr/bin/node` / `/usr/bin/npm` | `v22.23.3` / `10.9.9` | Runtime frontend y ejecución de CodeGraph |
| **ripgrep / fd** | `/root/.pi/agent/bin/rg` / `fd` | `15.2.0` / `10.5.0` | Búsqueda rápida en filesystem |
| **sqlite3 CLI** | *(no instalado)* | N/A (se utiliza `python3 sqlite3` / `node:sqlite`) | Inspección de bases SQLite |

### 2.2 Configuración de servidores MCP por cliente

| Cliente | Archivo de configuración | Servidores MCP configurados | Discrepancias / Hallazgos críticos |
|---|---|---|---|
| **Claude Code** | `/root/.claude.json` y `/root/.claude/settings.json` | 1. `engram` (`/usr/local/bin/engram mcp --tools=agent`)<br>2. `codegraph` (`codegraph serve --mcp`)<br>3. `context7` (`npx -y --package=@upstash/context7-mcp@2.2.5 -- context7-mcp`) | Permisos `allow: ["mcp__codegraph__*"]` activos. `context7` se lanza vía `npx` bajo demanda. |
| **OpenAI Codex** | `/root/.codex/config.toml` | 1. `engram` (`/usr/local/bin/engram mcp --tools=agent`)<br>2. `context7` (`https://mcp.context7.com/mcp`) | **Falta `codegraph` en `config.toml`.** Codex no tiene acceso MCP a CodeGraph. |
| **Gemini / Antigravity** | `/root/.gemini/settings.json`, `/root/.gemini/config/mcp_config.json`, `/root/.gemini/config/plugins/gentle-ai-engram/mcp_config.json`, `/root/.gemini/antigravity/mcp_config.json` | 1. `engram` (`--tools=agent` o `mcp`)<br>2. `gentle-ai-engram_engram` (**duplicado** desde el plugin)<br>3. `codegraph` (`codegraph serve --mcp`)<br>4. `context7` (`https://mcp.context7.com/mcp`) | **Servidor MCP `engram` duplicado** (`engram` + `gentle-ai-engram_engram`), exponiendo 22 herramientas diferidas por duplicado (44 esquemas en total). |

### 2.3 Archivos de instrucciones globales vs. locales y lo que se carga automáticamente en cada sesión

No existe ningún archivo `CLAUDE.md`, `AGENTS.md` ni `GEMINI.md` dentro de `/root/openvms`. Toda la carga proviene de archivos globales en `/root/`:

| Archivo cargado automáticamente | Tamaño (Bytes / Líneas) | Tokens estimados (~4 B/tok) | Desglose por sección (`gentle-ai:*`) | Problema detectado |
|---|---:|---:|---|---|
| `/root/.codex/AGENTS.md` | `100,234 B` (899 líneas) | `~25,058 tok` | `sdd-orchestrator`: `66,796 B`<br>`agent-routing`: `19,019 B`<br>`engram-protocol`: `8,892 B`<br>`strict-tdd-mode`: `26 B` | **1.** No incluye `codegraph-guidance`.<br>**2.** El 85.6% del archivo es protocolo SDD/ODD genérico.<br>**3.** Se inyecta íntegro en cada subagente. |
| `/root/.codex/engram-instructions.md` | `9,384 B` (143 líneas) | `~2,346 tok` | Cargado vía `model_instructions_file` en `/root/.codex/config.toml` | **100% duplicado** con el bloque `<!-- gentle-ai:engram-protocol -->` (`8,892 B`) que ya está dentro de `/root/.codex/AGENTS.md`. |
| `/root/.claude/CLAUDE.md` | `69,471 B` (477 líneas) | `~17,367 tok` | `sdd-orchestrator`: `43,289 B`<br>`agent-routing`: `19,019 B`<br>`codegraph-guidance`: `3,016 B`<br>`persona`: `2,020 B`<br>`engram-protocol`: `1,688 B` | El **89.8%** (`62,308 B`) son reglas de orquestación SDD/ODD que se cargan incluso en consultas simples o subagentes ejecutores. |
| `/root/.gemini/GEMINI.md` | `87,957 B` (814 líneas) | `~21,989 tok` | `sdd-orchestrator`: `51,371 B`<br>`agent-routing`: `19,019 B`<br>`engram-protocol`: `8,892 B`<br>`persona`: `5,220 B`<br>`codegraph-guidance`: `3,016 B` | Mismo fenómeno de sobrecarga por instrucciones de orquestación. |
| Esquemas MCP `engram` (`--tools=agent`) | `23,990 B` | `~5,997 tok` | 14 herramientas expuestas al agente (`tools/list`) | Se inyecta en el contexto base de cada sesión y subagente. |

### 2.4 Hooks configurados al iniciar sesiones, enviar prompts o invocar herramientas

| Cliente | Evento de Hook | Comando ejecutado | Latencia medida (Mediana / Máx) | Impacto |
|---|---|---|---|---|
| **Claude Code** | `UserPromptSubmit` | `codegraph prompt-hook` | `69.75 ms` / `72.72 ms` | Bajo en tiempo, verifica estado del índice. |
| **Claude Code** | `UserPromptSubmit` | `gentle-ai skill-registry refresh --quiet --no-gitignore --cwd "${CLAUDE_PROJECT_DIR:-$PWD}" \|\| true` | `7.01 ms` / `27.31 ms` | Corre en **cada prompt del usuario** con el flag `--no-gitignore` para regenerar `.atl/skill-registry.md` (`4.5 KB`). |
| **Claude Code** | `PreToolUse` (`matcher: "Agent"`) | `gentle-ai sdd-preflight-hook --agent claude-code` | `129.04 ms` / `714.53 ms` | Se ejecuta antes de lanzar cualquier subagente `Agent`. |
| **Claude Code** | `SessionStart` (`startup\|resume\|clear\|compact`) y `Stop` | `gentle-ai review stop-hook --agent claude-code` | `3.84 ms` / `10.46 ms` | Bajo costo computacional. |
| **Claude Code** | `Stop` y `SubagentStop` | `gentle-ai telemetry runtime claude --json` (async) | `< 10 ms` (async) | Registra telemetría al cerrar turno/subagente. |
| **Codex** | `SessionStart` (`startup\|resume\|clear\|compact`) | `gentle-ai skill-registry refresh --quiet --no-gitignore --cwd "$PWD" \|\| true` | `7.01 ms` / `27.31 ms` | Se ejecuta al iniciar cada sesión y cada compactación (99 compactaciones registradas). |
| **Codex** | `Stop` y `SubagentStop` | `gentle-ai telemetry runtime codex --json` (async) | `< 10 ms` (async) | Registra telemetría al finalizar subagente/sesión. |
| **Antigravity** | `PreInvocation` (`/root/.gemini/config/plugins/gentle-ai-engram/hooks.json`) | Inyecta `ephemeralMessage`: `"CRITICAL FIRST ACTION — Ensure these Engram MCP tools are available before responding: mem_save, mem_search, ..."` | `~2 ms` (proceso), pero **alto costo en turnos LLM** | Fuerza al agente a leer 13–14 archivos JSON de esquemas MCP diferidos (`/root/.gemini/antigravity-cli/mcp/engram/*.json`) al iniciar o tras compactar. |

### 2.5 Subagentes y Skills instalados

- **Subagentes estáticos en Claude Code (`/root/.claude/agents/*.md` — 19 archivos, `145 KB` totales):**
  - **Ciclo SDD (`11` agentes):** `sdd-init.md`, `sdd-onboard.md`, `sdd-explore.md`, `sdd-research.md`, `sdd-propose.md`, `sdd-spec.md`, `sdd-design.md`, `sdd-tasks.md`, `sdd-apply.md`, `sdd-verify.md`, `sdd-archive.md` (entre `6.6 KB` y `8.7 KB` cada uno).
  - **Revisión nativa y Judgment Day (`8` agentes):** `review-readability.md`, `review-reliability.md`, `review-resilience.md`, `review-risk.md`, `review-refuter.md`, `jd-judge-a.md`, `jd-judge-b.md`, `jd-fix-agent.md`.
- **Skills instalados (`21` directorios en `/root/.claude/skills/` y `/root/.codex/skills/`, `18` en `/root/.gemini/config/skills/`):**
  - Incluyen `_shared/` (`engram-convention.md`, `persistence-contract.md`, `review-ledger-contract.md`, `sdd-orchestrator-sections.md`, `strict-tdd-profile.md`), los 9 skills `sdd-*`, `judgment-day`, `go-testing`, `chained-pr`, `work-unit-commits`, `cognitive-doc-design`, `skill-creator`, `skill-improver`, `skill-registry`.
- **Bases SQLite internas de Codex en `/root/.codex/`:**
  - `thread_history_1.sqlite`: **142.29 MB**
  - `logs_2.sqlite`: **93.82 MB** (+ `609 KB` WAL)
  - `state_5.sqlite`: **14.72 MB**

---

## 3. Métricas observadas (Consolidado de Fases 1 a 5)

| Dimensión | Métrica observada | Valor medido | Fuente de evidencia |
|---|---|---|---|
| **Tokens (Claude Code)** | 18 sesiones padre (`cache_read` / `cache_create` / `output`) | `2,704,636,122` / `45,145,283` / `6,172,840` tokens | `/root/.claude/projects/-root-openvms/*.jsonl` |
| **Tokens (Claude Subagentes)** | 176 sesiones subagente (`cache_read` / `cache_create` / `output`) | `1,892,309,434` / `59,540,319` / `3,897,251` tokens | `/root/.claude/projects/-root-openvms/*/subagents/*.jsonl` |
| **Tokens (Codex)** | 239 sesiones (`98` padres + `173` subagentes; `input` / `cached` / `output` / `reasoning`) | `1,937,041,203` / `1,885,134,080` / `6,513,275` / `3,121,445` tokens | `/root/.codex/sessions/**/*.jsonl` |
| **Carga de Turno 1 (Contexto base)** | Mediana de tokens de entrada en el primer turno de una sesión | Claude Padre: **`74,356 tok`**<br>Claude Subagente: **`57,175 tok`**<br>Codex Padre: **`42,550 tok`**<br>Codex Subagente: **`36,451 tok`** | Telemetría `usage` / `token_count` del primer turno en logs JSONL |
| **Base Engram SQLite** | Tamaño en disco (`engram.db` + `engram.db-wal` + `engram.db-shm`) | `17,170,432 B` (`16.38 MB`) + `5,706,232 B` (`5.44 MB` WAL) + `32,768 B` (`0.03 MB` SHM) = **`22.91 MB`** | `/root/.engram/engram.db*` (solo lectura) |
| **Registros en Engram** | Filas por tabla principal | `observations`: **`1,435`** (`1,313` `openvms`, `122` `root`, `0` borradas)<br>`memory_relations`: **`3,031`**<br>`observation_versions`: **`146`**<br>`user_prompts`: **`72`**<br>`sessions`: **`16`** | Consulta SQL de solo lectura sobre `/root/.engram/engram.db` |
| **Volumen devuelto por Engram en Codex** | Salida acumulada de herramientas Engram (`3,731` respuestas) | **`21,299,810 bytes` (`21.30 MB`)**, media `5,708.9 B`, máx `43,751 B` | `/root/.codex/sessions/**/*.jsonl` (`custom_tool_call_output`) |
| **Tiempo de exploración antes de editar** | Mediana de tiempo y llamadas antes del primer `Edit`/`Write` en Claude | Padre: **`1,570.4 s`** (`42.0` llamadas)<br>Subagente: **`165.4 s`** (`23.5` llamadas, media `207.1 s` / `28.5` llamadas) | Trazas con timestamp ISO-8601 en `/root/.claude/projects/-root-openvms/` |

---

## 4. Cuellos de botella confirmados

### CB-1: Inflación de contexto inicial por instrucciones globales masivas y duplicadas (`36.4k – 74.3k tokens` en Turno 1)
- **Evidencia:** Cada sesión o subagente arranca con **36,451 a 74,356 tokens** en el Turno 1 antes de inspeccionar el código. En Codex, `/root/.codex/AGENTS.md` (`100.2 KB`) y `/root/.codex/engram-instructions.md` (`9.4 KB`) inyectan el protocolo de Engram **dos veces** y cargan `85.8 KB` de reglas del orquestador SDD/ODD en todos los subagentes.
- **Efecto:** Reduce la ventana efectiva de trabajo, acelera las compactaciones (**99 compactaciones** registradas en Codex, cada una disparando `mem_session_summary` + `mem_context`), y encarece cada llamada subsecuente a herramientas porque esos ~36k–74k tokens viajan en cada turno.

### CB-2: Ceremonia Engram ejecutada por cada subagente en Codex (`3,445` llamadas MCP desde subagentes)
- **Evidencia:** Los `173` subagentes de Codex ejecutaron una mediana de **14.0 llamadas a Engram por subagente** (media `22.8`, total `3,445` llamadas: `680 mem_save`, `655 mem_judge`, `557 mem_session_summary`, `368 mem_current_project`, `333 mem_search`, `325 mem_get_observation`, `284 mem_review`, `227 mem_context`).
- **Efecto:** Cada subagente repite la detección de proyecto, revisión de ciclo de vida, búsqueda de contexto y resumen de cierre, agregando entre **4 y 8 turnos adicionales de modelo** por subagente y contaminando la base con cientos de filas `Session summary: openvms`.

### CB-3: Impuesto de `mem_judge` (`2,760` veredictos ejecutados para `0` conflictos reales)
- **Evidencia:** De las `3,031` filas en `memory_relations`, los agentes juzgaron **2,760 relaciones** (`1,498 related`, `703 not_conflict`, `432 compatible`, `92 scoped`, `35 supersedes`, **`0 conflicts_with`**) y dejaron `271` pendientes. En Claude Code Padre, `mcp__engram__mem_judge` fue invocada **372 veces** (`3.29` veces por cada `mem_save`), siendo la segunda herramienta más usada de todo el sistema.
- **Efecto:** Cada `mem_save` (incluyendo simples actualizaciones de progreso de tareas ODD) obliga al agente a realizar 1 a 3 rondas adicionales de inferencia LLM (`~5–15 segundos` y decenas de miles de tokens leídos por ronda) sin haber detectado jamás un conflicto real. Furthermore, las 35 relaciones marcadas como `supersedes` **no desactivan ni excluyen** la observación antigua de `observations_fts`.

### CB-4: Uso de Engram como almacén de documentos de seguimiento ODD (`odd/*/tasks` de hasta 49.2 KB) y doble persistencia con Git
- **Evidencia:** Las observaciones más grandes de Engram son listas de tareas ODD (`#458` `odd/onvif-camera-management/tasks` con **49,216 B** y **64 revisiones**; `#1326` `odd/agent-reinstall-safety/tasks` con **30,417 B**, **33 revisiones** y **560,688 B** en `observation_versions`; `#252` con **30,130 B**; `#1086` con **26,539 B** y **31 revisiones**). Esos mismos documentos ya existen en el repositorio bajo `odd/tasks/*.md` (`60` archivos, `958.9 KB`).
- **Efecto:**
  1. En Codex, la observación `#1086` (`26.5 KB`) fue descargada **55 veces** (`17` padre + `38` subagentes) y la `#458` (`49.2 KB`) **50 veces** (`15` padre + `35` subagentes), inyectando **3.92 MB** de texto solo entre esos dos registros.
  2. Cada actualización reescribe el documento entero en FTS5, inflando `observations_fts_data` a **9.65 MB** (`4.2x` el tamaño del texto activo).

### CB-5: Falta de CodeGraph en Codex, ausencia de archivos de exclusión (`.rgignore`/`.claudeignore`) y falta de mapa de arquitectura en el repositorio
- **Evidencia:**
  1. Codex tiene **0 llamadas a CodeGraph** (no está en `/root/.codex/config.toml` ni en `AGENTS.md`) y compensa ejecutando **10,314 invocaciones a `sed`/`cat`/`rg`/`grep`/`find`**.
  2. En Claude Code, pese a tener CodeGraph configurado, se registraron **5,666 comandos `grep`/`rg`/`find`/`ls` en `Bash`** frente a solo **57 llamadas a `mcp__codegraph__codegraph_explore`**.
  3. Al no existir `.rgignore`, las búsquedas por defecto con `rg` escanean `67,375` líneas de código generado (`internal/api/gen/api.gen.go`, `internal/store/db/*.sql.go`, `apps/web/src/api/schema.d.ts`, `packages/api-contract/openapi.yaml`) y `60` archivos históricos en `odd/tasks/*.md`, devolviendo hasta **6.8x más bytes** de ruido que una búsqueda filtrada.

---

## 5. Hipótesis pendientes de verificar

1. **Impacto del tamaño de `/root/.codex/thread_history_1.sqlite` (142.3 MB) y `/root/.codex/logs_2.sqlite` (93.8 MB) en el arranque de `codex-cli`:**
   - *Estado:* Se observó un archivo `.sqlite-maintenance.lock` en `/root/.codex/` y más de `250 MB` acumulados en las bases internas de estado/logs de Codex. Falta instrumentación con `strace -T` durante un arranque interactivo en vivo de `codex` TUI para cuantificar cuántos milisegundos añade la apertura de `thread_history_1.sqlite` y `logs_2.sqlite`.
2. **Latencia de red / rate-limiting del proveedor de modelos en horarios pico vs. tiempo de serialización de contexto:**
   - *Estado:* En las trazas JSONL de Claude Code y Codex se observa el timestamp de inicio y fin de cada turno y el conteo exacto de tokens (`cache_read`, `cache_create`, `output`, `reasoning`), pero los logs locales no separan `TTFT` (Time-To-First-Token del servidor API remoto) de la velocidad de generación (`tokens/s`). Para medirlo se requiere capturar cabeceras HTTP/telemetría de red o activar `--debug` en una sesión controlada.
3. **Efecto de `AUTOINCREMENT` y fragmentación de `observations_fts` tras una compactación FTS (`INSERT INTO observations_fts(observations_fts) VALUES('optimize')`):**
   - *Estado:* Por restricción estricta de solo lectura no se ejecutó `optimize` sobre `observations_fts`. Queda pendiente verificar en una copia aislada cuánto baja la latencia de `A2_sql_fts_search_broad` (`31.15 ms` actual) al fusionar los `2,394` segmentos de `observations_fts_data`.

---

## 6. Análisis de Engram y SQLite (Fase 2)

### 6.1 Estado físico y configuración de `/root/.engram/engram.db`

| Parámetro SQLite / Archivo | Valor medido | Diagnóstico |
|---|---|---|
| `engram.db` / `engram.db-wal` / `engram.db-shm` | `17,170,432 B` (`16.38 MB`) / `5,706,232 B` (`5.44 MB`) / `32,768 B` | Total en disco: **`22.91 MB`**. El archivo WAL pesa el **33.2%** del archivo principal porque las conexiones de corta duración o procesos concurrentes dejan páginas sin consolidar hasta alcanzar el umbral de checkpoint. |
| `PRAGMA page_size` / `page_count` / `freelist_count` | `4096 B` / `4,212` páginas / `43` páginas libres (`176 KB`) | Baja fragmentación de páginas libres (`1.02%`), pero alto consumo en tablas auxiliares FTS5 y versiones. |
| `PRAGMA journal_mode` / `synchronous` / `wal_autocheckpoint` | `wal` / `2` (`FULL`) / `1000` páginas (`4 MB`) | `synchronous = FULL (2)` en modo `WAL` fuerza `fsync` adicionales en cada commit; en SQLite con `WAL`, el estándar recomendado para rendimiento sin perder durabilidad ante fallos de aplicación es `synchronous = NORMAL (1)`. |
| `PRAGMA auto_vacuum` | `0` (`NONE`) | No reclama espacio automáticamente al borrar versiones u observaciones antiguas. |

### 6.2 Distribución de registros por tabla y por categoría (`type`)

**Conteo de filas por tabla en `engram.db`:**
- `observations`: **1,435** filas activas (`0` con `deleted_at IS NOT NULL`: **nunca se ha borrado ni archivado ninguna observación**).
  - Proyecto `openvms`: **1,313 filas** (`2,152,874 bytes` de `content`).
  - Proyecto `root` (creado cuando el agente corre desde `/root` o sin detectar el repo): **122 filas** (`137,984 bytes`).
- `memory_relations`: **3,031** filas.
- `observation_versions`: **146** filas (`1,128,577 bytes` / `1.13 MB` en solo 38 observaciones distintas; media `7,730 B` por versión).
- `observations_fts_data`: **2,394** filas (**`9,652,675 bytes` / `9.65 MB`** — **56.2% de todo el archivo `engram.db`**).
- `user_prompts`: **72** filas (`prompts_fts`: 72 filas).
- `sessions`: **16** filas.

**Desglose de `observations` por `type`:**

| Categoría (`type`) | Cantidad (`c`) | % de filas | Tamaño medio (`avg_b`) | Tamaño máx (`max_b`) | Tamaño total (`sum_b`) | % del volumen total |
|---|---:|---:|---:|---:|---:|---:|
| `session_summary` | **750** | **52.26%** | `1,705.0 B` | `6,931 B` | `1,278,715 B` | **55.82%** |
| `discovery` | **263** | 18.33% | `1,374.0 B` | `30,417 B` | `361,371 B` | 15.77% |
| `pattern` | **11** | 0.77% | **`12,480.0 B`** | `30,130 B` | `137,280 B` | 5.99% |
| `architecture` | **64** | 4.46% | `2,130.3 B` | **`49,216 B`** | `136,341 B` | 5.95% |
| `bugfix` | **140** | 9.76% | `939.9 B` | `3,437 B` | `131,588 B` | 5.74% |
| `decision` | **113** | 7.87% | `1,067.7 B` | `20,139 B` | `120,651 B` | 5.27% |
| `config` | **32** | 2.23% | `2,048.7 B` | `23,076 B` | `65,557 B` | 2.86% |
| `feature` | **43** | 3.00% | `1,122.1 B` | `15,711 B` | `48,251 B` | 2.11% |
| `preference` | **18** | 1.25% | `589.3 B` | `1,011 B` | `10,608 B` | 0.46% |
| `reference` | **1** | 0.07% | `496.0 B` | `496 B` | `496 B` | 0.02% |
| **Total** | **1,435** | **100.0%** | **`1,596.4 B`** (mediana `1,147 B`, p95 `3,013 B`, p99 `13,601 B`) | **`49,216 B`** | **`2,290,858 B`** | **100.0%** |

### 6.3 Crecimiento temporal de `observations`

| Fecha (`UTC`) | Nuevas observaciones | Bytes de `content` actuales | Evento correlacionado |
|---|---:|---:|---|
| `2026-09-28` a `2026-10-03` | `266` | `510,598 B` | Inicio de uso intensivo de `odd/maps/tasks` y `ui-ux-refactor` |
| `2026-10-05` a `2026-10-08` | `551` | `976,970 B` | Pico de sesiones multi-subagente (`onvif-camera-management`, `agent-lifecycle`) |
| `2026-10-10` a `2026-10-16` | `298` | `423,465 B` | `alarms-phases3-6`, `fisheye`, `live-stream-quality-switching` |
| `2026-10-17` a `2026-10-20` | `320` | `379,825 B` | `face-recognition`, `agent-reinstall-safety`, cierre de PRs |

### 6.4 Duplicaciones, obsolescencia y anomalías de esquema/índices

1. **Redundancia masiva en `session_summary` (`750` registros sin `topic_key`):**
   - Existen **687 registros activos** con el título exacto `"Session summary: openvms"` (`1,191,851 B`) y **61 registros** con `"Session summary: root"` (`83,407 B`). Como `mem_session_summary` inserta siempre con `topic_key = NULL`, nunca hace upsert; cada subagente o compactación añade una fila más que queda indexada en `observations_fts`.
2. **Trackers `odd/*` almacenados en Engram en lugar de solo en el repositorio:**
   - Las **111 observaciones** con prefijo `topic_key LIKE 'odd/%'` acumulan **533 revisiones** y **500,185 bytes** de contenido activo (media `4,506.2 B`, máximo `49,216 B`), más **1.13 MB** en `observation_versions`.
   - Existen incluso colisiones de `topic_key` activo: `odd/resolve-build-typecheck-errors/tasks` tiene **2 filas activas simultáneas** (`id=3` e `id=23`, `18,137 B`).
3. **Observaciones `supersedes` y `needs_review` sin efecto en el filtrado:**
   - Las **35 observaciones** que fueron marcadas como reemplazadas (`relation = 'supersedes'`) en `memory_relations` siguen con `deleted_at IS NULL` y **las 35 permanecen indexadas en `observations_fts`**, devolviendo decisiones obsoletas o contradictorias en búsquedas futuras.
   - `1,296` de las `1,435` observaciones (`90.3%`) tienen `review_after IS NULL` (nunca expiran para revisión), y las `139` restantes aún no han expirado (`0` filas devueltas por `mem_review(action="list")`), haciendo que las **363 llamadas a `mem_review`** en Codex hayan devuelto siempre una lista vacía (`221 B`).
4. **Índices SQLite y planes de ejecución (`EXPLAIN QUERY PLAN`):**
   - `observations` tiene 11 índices individuales (`idx_obs_project`, `idx_obs_project_lower`, `idx_obs_created`, `idx_obs_deleted`, `idx_obs_scope`, `idx_obs_session`, `idx_obs_sync_id`, `idx_obs_topic`, `idx_obs_type`, `idx_obs_dedupe`), lo que penaliza cada `INSERT`/`UPDATE` con el mantenimiento de 11 B-trees + 4 triggers FTS5.
   - Sin embargo, **falta el índice compuesto que realmente usa `mem_context`**: `(project, deleted_at, updated_at DESC)`. Por eso `EXPLAIN QUERY PLAN` muestra:
     `SEARCH observations USING INDEX idx_obs_project (project=?)` seguido de **`USE TEMP B-TREE FOR ORDER BY`**.

---

## 7. Análisis de consultas MCP y subagentes (Fase 3)

### 7.1 Comparativa de uso de Engram MCP entre Claude Code y Codex

| Métrica | Claude Code Padre (`18` sesiones) | Claude Code Subagentes (`176` sesiones) | Codex Padre (`98` sesiones) | Codex Subagentes (`173` sesiones) |
|---|---:|---:|---:|---:|
| **Sesiones que llaman a Engram** | `17 / 18` (`94.4%`) | `3 / 176` (`1.7%`) | `~45 / 98` | `~141 / 173` (`81.5%`) |
| **Total llamadas a Engram** | **`536`** | **`22`** | **`2,150`** | **`3,445`** |
| **Media / Máximo por sesión** | `29.8` / `102` | `0.1` / `14` | `24.3` / `557` | **`22.8` (mediana `14.0`) / `625`** |
| `mem_save` | `113` | `6` | `423` | `680` |
| `mem_judge` | **`372`** | `12` | **`416`** | **`655`** |
| `mem_session_summary` | `10` | `2` | `323` | **`557`** |
| `mem_get_observation` | `7` | `0` | `320` | `325` |
| `mem_search` | `10` | `1` | `274` | `333` |
| `mem_context` | `3` | `1` | `197` | `227` |
| `mem_current_project` | `1` | `0` | `84` | **`368`** |
| `mem_review` | `0` | `0` | `79` | **`284`** |
| `mem_update` | `20` | `0` | `27` | `15` |

### 7.2 Respuestas a las 8 preguntas de diagnóstico de la Fase 3

1. **¿Cuántas consultas se realizan por tarea?**
   - En Claude Code, una sesión padre realiza en promedio **29.8 llamadas a Engram** (máximo **102**).
   - En Codex, un árbol de tarea complejo (padre + subagentes) realiza entre **187 y 760 llamadas a Engram** (`Parent 01a109fc`: `340` del padre + `420` de 23 subagentes = **760 llamadas**; `Parent 01a12b25`: `219` del padre + `489` de 26 subagentes = **708 llamadas**).
2. **¿Cuántas son repetidas o equivalentes?**
   - En Codex, **`mem_get_observation` se llamó 645 veces**, pero **307 de esas llamadas (47.6%)** pidieron reiteradamente los mismos 15 IDs de observaciones `odd/*/tasks`:
     - `#1086` (`odd/alarms-phases3-6/tasks`): **55 veces** (`17` padre, `38` subagentes).
     - `#458` (`odd/onvif-camera-management/tasks`): **50 veces** (`15` padre, `35` subagentes).
     - `#120` (`odd/maps/tasks`): **34 veces** (`20` padre, `14` subagentes).
     - `#1326` (`odd/agent-reinstall-safety/tasks`): **26 veces** (`18` padre, `8` subagentes).
     - `#917` (`odd/fisheye-grid-camera-indicators/tasks`): **22 veces** (`8` padre, `14` subagentes).
   - La búsqueda `mem_search(query="skill-registry")` se repitió **38 veces** en Codex.
3. **¿Los subagentes recuperan información que el agente principal ya conoce?**
   - **Sí, de forma sistemática.** En Codex, el agente padre lee `odd/<slug>/tasks` y los archivos del módulo antes de usar `spawn_agent`, y luego cada subagente vuelve a llamar a `mem_current_project`, `mem_context`, `mem_review`, `mem_search` y `mem_get_observation` sobre el mismo ID, además de releer los mismos archivos (entre **52.6% y 84.7% de redundancia** en archivos tocados por árbol en Codex, y entre **34.5% y 44.8%** en sesiones multi-subagente de Claude Code).
4. **¿Las consultas recuperan demasiado contexto?**
   - **Sí.** La media de bytes devueltos por llamada a Engram en Codex es de **5,708.9 B (~1,427 tokens)** con picos de **43,751 B (~10,937 tokens)**. Además, en Engram `3.4.0`, usar `mem_search(..., response_format="compact", limit=5)` envuelve los resultados en un sobre JSON estructurado con metadatos de versión/relación que midió **15,910 B (~3,977 tokens)** frente a **7,467 B** del formato texto legado con `limit=10`. Y si un agente invoca `mem_get_observation(id=1326, include_history=True)`, recibe **1,173,668 bytes (~293,417 tokens)** en una sola respuesta.
5. **¿Se ejecutan búsquedas globales cuando bastaría una búsqueda por módulo?**
   - **Sí.** `mem_search` en Engram no filtra por prefijo de `topic_key` (no tiene parámetro `topic_prefix`), sino que ejecuta `MATCH` en `observations_fts` sobre todas las observaciones del proyecto (incluyendo las 750 `session_summary` y las 111 `odd/*/tasks`), a menudo con `match_mode="any"` (`OR` implícito).
6. **¿Existe sobrecarga de inicialización de MCP?**
   - El binario `/usr/local/bin/engram mcp --tools=agent` inicia en **25.3 ms** (`stdio`), lo cual es muy rápido. Sin embargo, el esquema de sus 14 herramientas ocupa **23,990 bytes (~5,997 tokens)** en el contexto inicial de cada agente, y en Antigravity el hook `PreInvocation` fuerza al modelo a leer manualmente los archivos JSON de esquema diferido tras cada compactación.
7. **¿Hay consultas secuenciales que podrían evitarse o agruparse?**
   - **Sí.** Aunque Codex a veces agrupa `Promise.all([mem_context, mem_review, mem_search])` dentro de su wrapper JS `exec`, el flujo `mem_search` → `mem_get_observation` → `mem_save` → `3 × mem_judge` → `mem_session_summary` exige **mínimo 4 a 6 turnos secuenciales de modelo** por ciclo.
8. **¿El agente vuelve a investigar decisiones ya documentadas?**
   - **Sí**, por dos causas: (a) no existe un índice arquitectónico corto en el repositorio (`CLAUDE.md` / `AGENTS.md` local), y (b) los documentos de arquitectura que sí existen en `docs/` (`docs/ARCHITECTURE_AUDIT.md`, `docs/grpc-tls.md`, `docs/live-view-architecture.md`, `docs/maps/ARCHITECTURE.md`) no están referenciados desde las instrucciones de arranque.

---

## 8. Problemas de organización del monorepo (Fase 4)

### 8.1 Estructura y distribución de tamaño del repositorio (`/root/openvms`)

El repositorio tiene **1,366 archivos trackeados por Git**, distribuidos en **7 binarios/aplicaciones bajo `apps/`** (`api`, `edge-agent`, `frigate-mock`, `relay`, `vmsctl`, `web`, `worker`) y **30 paquetes Go bajo `internal/`** (`563` archivos).

| Categoría en el repositorio | Archivos trackeados | Líneas totales | Tamaño en disco | Observación |
|---|---:|---:|---:|---|
| **Código Frontend manual (`apps/web`)** | `541` | `93,699` | `3.79 MB` | React + TypeScript (`components`: 210, `lib`: 188, `i18n`: 58, `routes`: 54) |
| **Código Backend manual (`internal/`, `apps/`, `pkg/`)** | `537` | `106,736` | `3.47 MB` | 30 módulos en `internal/` (`api`: 84, `store`: 55, `provision`: 49, `agent`: 42) |
| **Código Generado (`*.gen.go`, `*.sql.go`, `schema.d.ts`, `gen/`)** | **`46`** | **`67,375`** | **`2.12 MB`** | **25.1% de todas las líneas de código** (`api.gen.go`: 37,310 líneas / 1.32 MB; `schema.d.ts`: 11,903 líneas / 385 KB) |
| **Trackers ODD en repo (`odd/tasks/*.md`)** | **`60`** | **`7,229`** | **`0.91 MB`** | Duplicados con Engram (`maps.md`: 112 KB, `plate-detail-watermark.md`: 70 KB, `ui-ux-refactor.md`: 61 KB) |
| **Especificaciones y Lockfiles (`openapi.yaml`, `pnpm-lock.yaml`, `go.sum`)** | `3` | `14,698` | `0.57 MB` | `packages/api-contract/openapi.yaml` tiene 7,238 líneas (`306 KB`) |
| **Activos binarios trackeados (`.onnx`, `.mp4`, `.docx`, `.png`)** | `7` | `104,021` | `29.92 MB` | `models/vehicle-body.onnx` (`30.8 MB`), `docs/Provision_ISR_LPR_Integracion_Plugin.docx` (`260 KB`) |
| **Documentación y Mockups (`docs/`, `README.md`, `PRD.txt`, HTML)** | `24` | `11,938` | `0.40 MB` | Incluye documentación duplicada (`PRD.txt` vs `docs/PRD.md`, `Readme.txt` vs `README.md`) |

### 8.2 Directorios pesados en el workspace (ignorados por Git pero presentes en disco)

| Directorio | Tamaño en disco (`du -sh`) | Riesgo durante exploración de agentes |
|---|---:|---|
| `/root/openvms/.atl` | **`942 MB`** | Contiene dumps de Postgres en `.atl/deploy` (`530 MB`), `.atl/onvif-deploy` (`157 MB`), `.atl/demo-cleanup` (`129 MB`), `.atl/face-deploy` (`128 MB`). |
| `/root/openvms/node_modules` | **`292 MB`** | Dependencias de `pnpm`. |
| `/root/openvms/.git` | **`76 MB`** | Historial Git. |
| `/root/openvms/bin` | **`44 MB`** | Binarios Go compilados localmente. |
| `/root/openvms/.codegraph` | **`32 MB`** (`85.45 MB` lógico) | Base SQLite de CodeGraph. |
| `/root/openvms/.mimocode` | **`30 MB`** | Caché de índice semántico previo (`.mimocode`). |
| `/root/openvms/models` | **`30 MB`** | Modelo ONNX versionado en Git. |
| `/root/openvms/data` | **`29 MB`** | Datos locales de Postgres/servicios. |

### 8.3 Hallazgos estructurales del monorepo

1. **Ausencia total de `.rgignore`, `.ignore`, `.claudeignore` y `.codexignore`:**
   - Aunque `.gitignore` excluye `node_modules/`, `.atl/`, `.codegraph/` y `bin/`, **NO excluye de las búsquedas de agentes** los archivos generados que sí están versionados en Git (`internal/api/gen/api.gen.go`, `internal/store/db/*.sql.go`, `apps/web/src/api/schema.d.ts`, `gen/`, `packages/api-contract/openapi.yaml`) ni los 60 archivos históricos de `odd/tasks/*.md`.
2. **Archivos monolíticos de gran tamaño que saturan lecturas completas:**
   - En frontend: `apps/web/src/routes/Servers.tsx` (`2,666` líneas / `124.7 KB`), `apps/web/src/routes/Live.test.tsx` (`2,065` líneas / `95.6 KB`), `apps/web/src/routes/FrigateCameraConfig.tsx` (`1,737` líneas / `81.1 KB`), `apps/web/src/routes/Live.tsx` (`1,644` líneas / `77.5 KB`), `apps/web/src/components/EvidencePlayerModal.tsx` (`1,542` líneas / `58.7 KB`).
   - En backend: `internal/media/gateway.go` (`1,485` líneas / `53.8 KB`), `deploy/agent/compose/compose_contract_test.go` (`1,281` líneas / `55.4 KB`).
3. **Documentación duplicada y ausencia de mapa por módulo:**
   - Existen en la raíz `PRD.txt` (`53.3 KB`) y `docs/PRD.md` (`49.5 KB`), además de `Readme.txt` (`8.5 KB`) y `README.md` (`22.4 KB`), y dos mockups HTML sueltos (`avigilon_alta_cloud_vms_mockup.html`, `vms_mapas_soc.html`).
   - Ninguno de los 30 paquetes en `internal/*` ni `apps/web` posee un `README.md` o mapa de contratos/símbolos, obligando a los subagentes a ejecutar decenas de comandos `rg`/`sed`/`cat` para reconstruir las fronteras de cada paquete en cada tarea.

---

## 9.Resultados del Benchmark Controlado (Fase 5)

### Prueba A — Latencia de SQLite vs. MCP `stdio` y tamaño de contexto recuperado

*Condiciones:* 10 iteraciones en modo `mode=ro` para consultas SQLite directas; 5 iteraciones de ciclo completo `initialize` + `tools/call` sobre `stdio` contra `/usr/local/bin/engram mcp --tools=agent`.

| ID Prueba | Operación medida | Filas / Bytes devueltos | Tokens est. | Min (ms) | Mediana (ms) | Media (ms) | Máx (ms) |
|---|---|---:|---:|---:|---:|---:|---:|
| `A1_sql` | SQLite directo: `mem_context` (últimas 20 obs de `openvms`) | `20` / `8,432 B` | `~2,108` | `3.05` | **`3.30`** | `4.14` | `10.87` |
| `A2_sql` | SQLite directo: `observations_fts MATCH 'agent OR tls OR mTLS OR enrollment'` | `10` / `3,823 B` | `~955` | `28.06` | **`31.15`** | `31.44` | `33.68` |
| `A3_sql` | SQLite directo: búsqueda por prefijo `topic_key LIKE 'agent%'` | `10` / `4,192 B` | `~1,048` | `1.42` | **`1.45`** | `1.55` | `2.09` |
| `A4_sql` | SQLite directo: `SELECT` por PK (`id=458`, tracker ODD de 49 KB) | `1` / `49,876 B` | `~12,469` | `0.03` | **`0.05`** | `0.06` | `0.12` |
| `A5_sql` | SQLite directo: `SELECT` por PK (`id=1431`, decisión normal de 1.2 KB) | `1` / `1,259 B` | `~314` | `0.003` | **`0.003`** | `0.005` | `0.023` |
| `MCP_0` | MCP `stdio`: `initialize` + `tools/list` (`--tools=agent`) | `14 tools` / `23,990 B` | `~5,997` | `18.44` | **`29.48`** | `28.25` | `35.11` |
| `MCP_1` | MCP `stdio`: `mem_current_project(cwd="/root/openvms")` | `1` / `221 B` | `~55` | `22.65` | **`25.27`** | `26.19` | `33.44` |
| `MCP_2` | MCP `stdio`: `mem_context(project="openvms")` (default) | `20` / `8,423 B` | `~2,105` | `37.38` | **`45.80`** | `43.89` | `47.22` |
| `MCP_3` | MCP `stdio`: `mem_context(project="openvms", compact=true)` | `20` / **`2,144 B`** | **`~536`** | `36.83` | **`43.62`** | `43.68` | `50.07` |
| `MCP_5` | MCP `stdio`: `mem_search(match_mode="any", limit=10)` (formato legado) | `10` / `7,467 B` | `~1,866` | `26.13` | **`34.62`** | `34.12` | `39.56` |
| `MCP_6` | MCP `stdio`: `mem_search(match_mode="all", response_format="compact", limit=5)` | `5` / **`15,910 B`** | **`~3,977`** | `20.05` | **`24.40`** | `24.50` | `30.66` |
| `MCP_7` | MCP `stdio`: `mem_get_observation(id=458)` (`odd/onvif-camera-management/tasks`) | `1` / **`50,205 B`** | **`~12,551`** | `18.05` | **`21.42`** | `22.35` | `26.67` |
| `MCP_8` | MCP `stdio`: `mem_get_observation(id=1326, include_history=true)` | `1+32 ver` / **`1,173,668 B`** | **`~293,417`** | `41.41` | **`45.15`** | `47.86` | `62.26` |

### Prueba B — Exploración del repositorio (5 iteraciones por método)

| ID Prueba | Método de búsqueda | Líneas devueltas | Bytes devueltos (Tokens est.) | Mediana (ms) | Media (ms) | Hallazgo clave |
|---|---|---:|---:|---:|---:|---|
| `B1` | `grep -rn` sin excluir generados (`FrigateCameraConfig`) | `39` | **`332,754 B` (`~83,188 tok`)** | `44.18` | `49.90` | `internal/api/gen/api.gen.go` incluye una línea embebida de ~325 KB que explota el contexto. |
| `B2` | `rg -n FrigateCameraConfig .` (default `.gitignore`) | `35` | `7,114 B` (`~1,778 tok`) | `13.67` | `27.01` | Incluye hits en `api.gen.go`, `schema.d.ts`, `openapi.yaml` y `odd/tasks/*.md`. |
| `B3` | `rg -n` excluyendo `*.gen.go`, `*.sql.go`, `schema.d.ts`, `odd/**`, `openapi.yaml` | `18` | **`2,296 B` (`~574 tok`)** | `13.39` | `13.75` | **Reducción del 67.7% en tokens** y eliminación del 100% de falsos positivos generados. |
| `B4` | `rg -n FrigateCameraConfig apps/web/src/routes` (acotado al módulo) | `4` | **`471 B` (`~117 tok`)** | **`2.87`** | `3.00` | **4.8x más rápido** y **93.4% menos tokens** que `rg` global. |
| `B5` | `codegraph query FrigateCameraConfig` | `50` | `1,314 B` (`~328 tok`) | `140.82` | `152.98` | Devuelve nodos tipados exactos sin ruido de markdown ni blobs generados. |
| `B6` | `codegraph callers FrigateCameraConfig` | `12` | **`270 B` (`~67 tok`)** | `129.22` | `134.61` | Resuelve el grafo de llamadas en 1 sola invocación (`67 tokens`). |
| `B9` vs `B10` | `rg -n ServerFrigateConfig .` (Default vs. Sin `odd/**` ni generados) | `212` → **`29`** | `27,089 B` → **`3,993 B`** | `13.79` → `11.36` | `13.82` → `11.51` | **Reducción del 85.3% en bytes** (`183` coincidencias espurias eliminadas). |

### Prueba C y D — Contexto mínimo vs. sobrecarga de subagentes (Medición empírica sobre 433 sesiones reales)

| Métrica medida en trazas reales | Claude Padre (`18` ses.) | Claude Subagente (`176` ses.) | Codex Padre (`98` ses.) | Codex Subagente (`141` ses. con tiempo) |
|---|---:|---:|---:|---:|
| **Tokens de entrada en Turno 1 (mediana)** | `74,356 tok` | `57,175 tok` | `42,550 tok` | `36,451 tok` |
| **Tokens de entrada en turno pico (mediana / máx)** | `329,476` / `751,515 tok` | `115,246` / `613,373 tok` | `71,462` / `235,489 tok` | `87,009` / `235,833 tok` |
| **Tiempo hasta la 1.ª llamada a herramienta (TTFA, mediana)** | `4.47 s` | `2.98 s` | `6.85 s` | `4.48 s` |
| **Tiempo hasta el 1.er `Edit`/`Write` (mediana / media)** | `1,570.4 s` / `12,650.1 s` | **`165.4 s` / `207.1 s`** | *(ejecuta vía `exec`)* | *(ejecuta vía `exec`)* |
| **Llamadas a herramientas antes del 1.er `Edit`/`Write` (mediana / máx)** | `42.0` / `657` | **`23.5` / `79`** | N/A | N/A |
| **Comandos shell de lectura/búsqueda (`sed`/`cat`/`rg`/`grep`) por sesión (mediana / media)** | N/A (`1,739` total) | N/A (`3,927` total) | `0.0` / `22.5` | **`23.0` / `53.8` (máx `1,964`)** |
| **Llamadas a Engram por sesión (mediana / media / máx)** | `~20` / `29.8` / `102` | `0.0` / `0.1` / `14` | `0.0` / `24.3` / `557` | **`14.0` / `22.8` / `625`** |
| **Duración total de sesión (mediana / p90)** | `25,130.9 s` / `233,127 s` | **`357.9 s` / `1,495.1 s`** | `23.5 s` / `38,790 s` | **`343.0 s` / `7,297.5 s`** |

---

## 10. Arquitectura propuesta y comparativa de herramientas (Fase 6)

### 10.1 Modelo de Memoria Jerárquica de 4 Niveles (Continuidad Claude Code ↔ Codex)

```
┌────────────────────────────────────────────────────────────────────────────┐
│ NIVEL 1 — Arquitectura y Mapa del Monorepo (En Git: CLAUDE.md / AGENTS.md) │
│ • Tamaño objetivo: <= 3.5 KB (~900 tokens).                                │
│ • Contiene: Mapa de los 30 paquetes internal/* y apps/*, rutas generadas   │
│   prohibidas para lectura/edición, comandos de test/build y convención de  │
│   topic_key de Engram. Compartido nativamente por Claude Code y Codex.     │
├────────────────────────────────────────────────────────────────────────────┤
│ NIVEL 2 — Conocimiento por Módulo (En Git: docs/<modulo>.md + Engram)      │
│ • Contratos estables en docs/ (referenciados bajo demanda, no en Turno 1). │
│ • Decisiones/gotchas vivos en Engram con topic_key="<modulo>/<tema>"       │
│   (máximo 1,500 bytes por observación; prohibido guardar listas de tareas).│
├────────────────────────────────────────────────────────────────────────────┤
│ NIVEL 3 — Estado de la Tarea Activa (En Git: odd/tasks/<slug>.md)          │
│ • NUNCA en Engram. El archivo odd/tasks/<slug>.md en el worktree es la     │
│   única fuente de verdad de checklists ODD/SDD.                            │
│ • El agente padre pasa a cada subagente un "Context Pack" en el prompt     │
│   (archivos exactos, líneas, resumen de decisión), prohibiendo que el      │
│   subagente invoque Engram o re-explore el repositorio.                    │
├────────────────────────────────────────────────────────────────────────────┤
│ NIVEL 4 — Historial de Sesiones bajo demanda (Engram: session_summary)     │
│ • Solo el agente padre escribe 1 resumen al cerrar la sesión (con          │
│   topic_key="session/latest/<branch>" o excluido de FTS por defecto).      │
│ • Los subagentes tienen PROHIBIDO llamar a mem_session_summary.            │
└────────────────────────────────────────────────────────────────────────────┘
```

### 10.2 Reglas de Recuperación Selectiva y Coordinación Padre-Subagente

1. **Subagentes "Memory-Free" por defecto:**
   - Un subagente ejecutor o verificador recibe en su prompt de invocación el **Context Pack** preparado por el padre (rutas de archivos, firmas relevantes obtenidas vía CodeGraph, y cualquier restricción de memoria ya consultada por el padre).
   - Los subagentes **no deben ejecutar** `mem_current_project`, `mem_context`, `mem_review`, `mem_search`, `mem_save`, `mem_judge` ni `mem_session_summary`. Solo el agente orquestador padre interactúa con Engram (1 lectura selectiva al inicio de la tarea y 1 escritura de decisión/bugfix real al finalizar).
2. **Eliminación de trackers `odd/*/tasks` de Engram:**
   - Las listas de tareas viven exclusivamente en `odd/tasks/<slug>.md` (o en artefactos de sesión) y solo se guarda en Engram el resumen arquitectónico final (`<= 1.2 KB`) al cerrar el feature.
3. **Presupuesto estricto de contexto en consultas Engram del padre:**
   - Usar `mem_context(project="openvms", compact=true)` (`2,144 B` en lugar de `8,423 B`).
   - En `mem_search`, usar `match_mode="all"` y `limit=5` (sin `response_format="compact"` hasta que Engram corrija la inflación de 15.9 KB de ese formato, o filtrando por `topic_key`).

### 10.3 Optimización del Monorepo

1. **Crear `.rgignore` (y `.claudeignore` / `.codexignore`) en la raíz `/root/openvms`:**
   - Excluir de las búsquedas por defecto:
     - `internal/api/gen/` (`api.gen.go`, 1.32 MB)
     - `internal/store/db/*.sql.go` (sqlc generado)
     - `apps/web/src/api/schema.d.ts` (385 KB)
     - `gen/` (protobuf generado)
     - `packages/api-contract/openapi.yaml` (306 KB)
     - `odd/tasks/` (958 KB de historial de tareas)
     - `models/*.onnx`, `apps/web/public/benchmark/*.mp4`, `docs/*.docx`
   - Esto reduce inmediatamente entre **68% y 85% los tokens devueltos por `rg`** sin impedir que un agente lea explícitamente esos archivos cuando edita contratos OpenAPI o SQL.
2. **Crear `/root/openvms/AGENTS.md` y `/root/openvms/CLAUDE.md` (symlink) de `<= 3.5 KB`:**
   - Incluir el mapa de los 30 paquetes de `internal/*`, las 7 apps en `apps/*`, la ubicación de las fuentes de verdad de contratos (`proto/`, `migrations/`, `internal/store/queries/`, `packages/api-contract/`) y las reglas de no-reexploración para subagentes.
3. **Mover los backups de `.atl/*deploy*` (`942 MB`) fuera del árbol de código (p. ej. a `/var/backups/openvms` o `~/.local/share/openvms/backups`) y quitar `--no-gitignore` del hook `skill-registry refresh`.**

### 10.4 Comparativa de herramientas complementarias

| Herramienta evaluada | Estado actual en `/root/openvms` | Beneficio concreto medido | Complejidad | Costo de mantenimiento | Veredicto |
|---|---|---|---|---|---|
| **CodeGraph MCP + CLI** (`codegraph 1.6.0`) | Instalado e indexado (`85.45 MB`, `19,816` nodos). Activo en Claude Code; **ausente en Codex (`config.toml` y `AGENTS.md`)**. | Reduce búsquedas de callers/símbolos a `67–328 tokens` (`129–140 ms`) y evita leer archivos generados. | **Baja** (solo registrar en `/root/.codex/config.toml` y añadir guía en `AGENTS.md`). | **Bajo** (ya está instalado y mantenido en `.codegraph/`). | **RECOMENDADO (Prioridad Alta):** habilitar en Codex para igualar capacidades con Claude Code. |
| **Archivos de exclusión `.rgignore` / `.ignore`** | **No existen.** | Reduce **67.7% a 85.3%** el volumen de tokens en `rg` a **0 ms** de costo adicional (`10–13 ms` por búsqueda). | **Muy baja** (1 archivo de texto de 15 líneas). | **Nulo.** | **RECOMENDADO (Prioridad Inmediata):** mayor retorno sobre inversión para ambos agentes. |
| **Language Server Protocol (`gopls` / `typescript-language-server`)** | Plugins instalados en marketplace de Claude, pero los agentes usan `rg`/`go test`/`tsc` por CLI. | Navegación de tipos exacta, pero consume `500 MB – 1.5 GB` de RAM residente por proceso LSP y no es compartido entre subagentes efímeros de Codex y Claude. | **Media-Alta** | **Medio** (procesos residentes huérfanos). | **NO RECOMENDADO ahora:** `CodeGraph` + `.rgignore` + `go test`/`tsc` cubren el 95% del caso de uso con mucha menor huella de memoria. |
| **Índices semánticos / Vectoriales adicionales (`.mimocode` / embeddings en Engram)** | Existe directorio residual `.mimocode` (`30 MB`) y columnas `embedding` vacías en `observations`. | Poco beneficio frente a FTS5 + `topic_key` jerárquico cuando el repositorio y las memorias están limpios; añade latencia de generación de embeddings. | **Alta** | **Alto** (sincronización de modelos de embedding). | **NO RECOMENDADO:** el cuello de botella en Engram no es falta de búsqueda vectorial, sino exceso de ruido (`session_summary` y `odd/*/tasks`). |
| **Caché de contexto Padre → Subagente ("Context Pack" en prompt)** | No implementado; cada subagente re-explora. | Elimina el **34%–84% de lecturas duplicadas** y las **14 llamadas medianas a Engram por subagente** en Codex. | **Baja** (cambio en reglas de delegación de `CLAUDE.md` / `AGENTS.md`). | **Nulo.** | **RECOMENDADO (Prioridad Alta).** |

---

## 11. Plan de optimización priorizado (Fase 7)

### Etapa 1 — Victorias inmediatas de configuración e instrucciones (Impacto Alto, Riesgo Bajo)

| Campo | R-01: Eliminar duplicación y adelgazar `/root/.codex/AGENTS.md`, `/root/.claude/CLAUDE.md` y `/root/.gemini/GEMINI.md` |
|---|---|
| **Problema** | Cada turno y cada subagente arranca con **36,451 a 74,356 tokens** de entrada porque `AGENTS.md` (`100.2 KB`), `CLAUDE.md` (`69.5 KB`) y `GEMINI.md` (`88.0 KB`) inyectan `62–85 KB` de prosa del orquestador SDD/ODD, y Codex además duplica `engram-instructions.md` (`9.4 KB`). |
| **Evidencia** | Medición directa de archivos en `/root/.codex/` y `/root/.claude/` + telemetría del Turno 1 en `18` padres y `176` subagentes de Claude (`74,356` y `57,175` tokens medianos) y `98` padres / `141` subagentes de Codex (`42,550` y `36,451` tokens medianos). |
| **Solución** | 1. Quitar `model_instructions_file = "/root/.codex/engram-instructions.md"` de `/root/.codex/config.toml` (o dejar solo la referencia y quitar el bloque duplicado de `AGENTS.md`).<br>2. Extraer los manuales detallados de `sdd-orchestrator` (`43–66 KB`) y `agent-routing` (`19 KB`) a skills bajo demanda (`/root/.claude/skills/sdd-orchestrator/SKILL.md`), dejando en `CLAUDE.md` / `AGENTS.md` un núcleo compacto de `<= 8 KB` (~2,000 tokens).<br>3. Eliminar el servidor MCP duplicado `gentle-ai-engram_engram` en `/root/.gemini/config/plugins/gentle-ai-engram/mcp_config.json`. |
| **Impacto** | **Alto** (ahorro inmediato de **15,000 a 22,000 tokens en cada turno** de cada agente y subagente). |
| **Complejidad** | **Baja** |
| **Riesgo** | Bajo si se conserva en el archivo global la tabla de triggers hacia los skills SDD bajo demanda. |
| **Validación** | Verificar en el primer evento `token_count` / `usage` de una sesión nueva que los tokens de entrada del Turno 1 bajen en `>= 15,000 tokens`. |

| Campo | R-02: Prohibir ceremonias de Engram (`mem_*`) dentro de subagentes y delegar mediante "Context Pack" |
|---|---|
| **Problema** | Los subagentes de Codex ejecutan una mediana de **14.0 llamadas a Engram por subagente** (`3,445` llamadas totales en `173` subagentes, incluyendo `557` llamadas a `mem_session_summary` y `655` a `mem_judge`), y releen los mismos archivos que el padre (`52%–84%` de redundancia). |
| **Evidencia** | Análisis de `/root/.codex/sessions/**/*.jsonl`: `Parent 01a109fc` (`23` subagentes) acumuló `760` llamadas a Engram y `84.7%` de accesos redundantes a archivos; `Parent 01a1546e` (`28` subagentes) realizó `503` llamadas a Engram desde subagentes. |
| **Solución** | En la sección `engram-protocol` y `agent-routing` de `CLAUDE.md` y `AGENTS.md`:<br>1. Establecer como regla explícita que **solo el agente principal (padre)** puede invocar `mem_current_project`, `mem_context`, `mem_review`, `mem_session_summary` y `mem_save`/`mem_judge`.<br>2. Cuando `source.subagent` esté activo o el agente sea invocado como subagente (`spawn_agent` / `Agent`), debe operar en modo **Memory-Free** salvo que el prompt del padre le ordene explícitamente una consulta puntual.<br>3. Exigir al padre incluir un bloque `## Context Pack` (rutas exactas, líneas y decisiones previas) al invocar cualquier subagente. |
| **Impacto** | **Alto** (elimina **> 60% de todas las llamadas MCP a Engram** y reduce en **4 a 8 turnos LLM** la duración de cada subagente en Codex). |
| **Complejidad** | **Baja** |
| **Riesgo** | Bajo; el agente padre conserva la responsabilidad única de persistir las decisiones y el resumen final de la sesión. |
| **Validación** | Medir en los logs de subagentes nuevos que el conteo de llamadas `mem_*` por subagente sea `0` (frente a la mediana actual de `14.0`) y que la mediana de duración de subagentes baje de `343 s` a `< 180 s`. |

| Campo | R-03: Crear `.rgignore` en `/root/openvms` y mapa arquitectónico local (`AGENTS.md` / `CLAUDE.md` en repo) |
|---|---|
| **Problema** | `rg` y `grep` escanean `67,375` líneas de código generado (`api.gen.go`, `*.sql.go`, `schema.d.ts`, `openapi.yaml`) y `60` archivos `odd/tasks/*.md`, inflando los resultados de búsqueda hasta en un **85.3%** (o **332 KB** con `grep -rn`). Además, no existe un mapa local de los 30 módulos de `internal/*`. |
| **Evidencia** | Prueba B (`B9` vs `B10`: `27,089 B` → `3,993 B`; `B1`: `332,754 B` en `api.gen.go`). En Claude Subagentes la fase de exploración previa al primer `Edit` toma una mediana de **165.4 s** y **23.5 llamadas**. |
| **Solución** | 1. Crear `/root/openvms/.rgignore` excluyendo `internal/api/gen/`, `internal/store/db/*.sql.go`, `apps/web/src/api/schema.d.ts`, `gen/`, `packages/api-contract/openapi.yaml`, `odd/tasks/`, `models/*.onnx`, `*.mp4`, `*.docx`.<br>2. Crear `/root/openvms/AGENTS.md` (y symlink `/root/openvms/CLAUDE.md`) de `<= 3.5 KB` con la tabla de los 30 paquetes `internal/*`, las 7 apps en `apps/*` y los comandos de verificación por paquete. |
| **Impacto** | **Alto** (reduce **68%–85%** los tokens consumidos en búsquedas de código y acorta la fase de exploración de subagentes). |
| **Complejidad** | **Baja** |
| **Riesgo** | Bajo (las exclusiones en `.rgignore` solo aplican a búsquedas implícitas con `rg`; `Read` o `rg -u` sobre rutas explícitas sigue funcionando). |
| **Validación** | Ejecutar `rg -n ServerFrigateConfig .` y comprobar que devuelve `~29 líneas / ~4 KB` en lugar de `212 líneas / 27.1 KB`, y medir que las llamadas previas al primer `Edit` en subagentes bajen de `23.5` a `< 10`. |

| Campo | R-04: Registrar `codegraph` MCP en `/root/.codex/config.toml` e incluir `codegraph-guidance` en `/root/.codex/AGENTS.md` |
|---|---|
| **Problema** | Codex no tiene `codegraph` configurado en `config.toml` ni en `AGENTS.md`, por lo que realizó **0 llamadas a CodeGraph** y ejecutó **10,314 comandos shell** (`sed`, `cat`, `rg`, `grep`, `find`). |
| **Evidencia** | Inspección de `/root/.codex/config.toml`, `/root/.codex/AGENTS.md` y conteo de llamadas en las `239` sesiones de Codex. |
| **Solución** | Añadir `[mcp_servers.codegraph]` (`command = "codegraph"`, `args = ["serve", "--mcp"]`) en `/root/.codex/config.toml` y añadir el bloque compacto de `codegraph-guidance` en `/root/.codex/AGENTS.md`. |
| **Impacto** | **Medio-Alto** (habilita consultas estructurales de `67–328 tokens` en Codex y alinea el flujo de trabajo con Claude Code). |
| **Complejidad** | **Baja** |
| **Riesgo** | Bajo (el índice `.codegraph/` ya existe y está actualizado en `/root/openvms/.codegraph`). |
| **Validación** | Verificar que `codex` liste las herramientas `mcp__codegraph__*` y las utilice en consultas de símbolos/callers. |

---

### Etapa 2 — Optimización del protocolo de persistencia ODD y saneamiento de Engram (Impacto Alto, Complejidad Media)

| Campo | R-05: Dejar de guardar listas de tareas `odd/<slug>/tasks` en Engram y usar únicamente `odd/tasks/<slug>.md` en el repo |
|---|---|
| **Problema** | Las 111 observaciones `odd/*` en Engram acumulan **533 revisiones**, **500 KB** de texto activo (hasta **49.2 KB** por fila), **1.13 MB** en `observation_versions` y **9.65 MB** en `observations_fts_data`, y fueron descargadas cientos de veces vía `mem_get_observation` (`#1086` 55 veces, `#458` 50 veces). |
| **Evidencia** | Consultas SQLite de la Fase 2 y análisis de `mem_get_observation` en la Fase 3. |
| **Solución** | Cambiar la política de persistencia en `sdd-orchestrator` / `persistence-contract.md` para que el seguimiento de tareas ODD/SDD viva **exclusivamente en `odd/tasks/<slug>.md`** en el filesystem, reservando Engram únicamente para decisiones de arquitectura, descubrimientos no obvios y bugfixes (`<= 1,500 bytes` por registro). |
| **Impacto** | **Alto** (elimina las descargas de `30–50 KB` por turno y detiene la fragmentación de `observations_fts_data` y `observation_versions`). |
| **Complejidad** | **Baja-Media** |
| **Riesgo** | Bajo (`odd/tasks/<slug>.md` ya está versionado en Git y es accesible tanto por Claude Code como por Codex). |
| **Validación** | Confirmar que ninguna nueva observación en `observations` supere los `2,500 bytes` ni utilice el prefijo `odd/*/tasks`. |

| Campo | R-06: Desactivar o limitar `mem_judge` sincrónico y depurar `session_summary` / `odd/*` en `engram.db` |
|---|---|
| **Problema** | 1. Los agentes ejecutaron **2,760 llamadas a `mem_judge`** con **0 conflictos reales (`0 conflicts_with`)**, perdiendo miles de segundos de inferencia.<br>2. **750 de las 1,435 observaciones (52.3%)** son `session_summary` sin `topic_key` que contaminan `observations_fts`.<br>3. Las 35 observaciones `supersedes` siguen activas en FTS. |
| **Evidencia** | Tabla `memory_relations` (`2,760` juzgadas, `0` `conflicts_with`), tabla `observations` (`750` `session_summary`), y `observations_fts_data` (`9.65 MB`). |
| **Solución** | 1. En el protocolo Engram, exigir que `mem_session_summary` use un `topic_key` por rama/proyecto (upsert sobre `session-summary/openvms` manteniendo solo las últimas N versiones) o archivar (`deleted_at = datetime('now')`) los resúmenes de subagentes y sesiones de más de 7 días.<br>2. Marcar `deleted_at` en las 35 observaciones destino de relaciones `supersedes` y en los trackers `odd/*/tasks` ya cerrados.<br>3. Omitir `mem_judge` en el protocolo de agente salvo cuando `type IN ('architecture', 'decision')` y el score de similitud supere el umbral de conflicto real.<br>4. Ejecutar una sola vez `INSERT INTO observations_fts(observations_fts) VALUES('optimize');` y `PRAGMA wal_checkpoint(TRUNCATE);` tras el archivado. |
| **Impacto** | **Alto** (elimina el 2.º tool call más frecuente de Claude Code y Codex, reduce `engram.db` + WAL de `22.9 MB` a `< 5 MB`, y mejora la precisión y velocidad de `mem_search`). |
| **Complejidad** | **Media** |
| **Riesgo** | Bajo si se realiza un backup previo de `/root/.engram/engram.db` antes del archivado lógico (`deleted_at`). |
| **Validación** | Verificar que `SELECT COUNT(*) FROM observations WHERE deleted_at IS NULL` baje de `1,435` a `~550` registros de alto valor, que `A2_sql_fts_search_broad` baje de `31.15 ms` a `< 5 ms`, y que las llamadas a `mem_judge` caigan en `> 90%`. |

| Campo | R-07: Crear índice compuesto `idx_obs_project_active_updated` y ajustar `PRAGMA synchronous = NORMAL` en Engram |
|---|---|
| **Problema** | `mem_context` ejecuta `USE TEMP B-TREE FOR ORDER BY` porque no existe índice sobre `(project, deleted_at, updated_at DESC)`, y `synchronous = FULL (2)` añade `fsync` innecesarios en modo WAL. |
| **Evidencia** | `EXPLAIN QUERY PLAN` en Prueba `A1_sql` y lectura de `PRAGMA synchronous` (`2`). |
| **Solución** | En una ventana de mantenimiento autorizada, crear `CREATE INDEX IF NOT EXISTS idx_obs_project_active_updated ON observations(project, updated_at DESC) WHERE deleted_at IS NULL;` y configurar `PRAGMA synchronous = NORMAL;` en la conexión WAL. |
| **Impacto** | **Medio** (elimina el ordenamiento temporal en memoria en `mem_context` y reduce la latencia de escritura en SQLite). |
| **Complejidad** | **Baja** |
| **Riesgo** | Muy bajo (índice parcial no destructivo). |
| **Validación** | Ejecutar `EXPLAIN QUERY PLAN` sobre la consulta de `mem_context` y verificar que desaparezca `USE TEMP B-TREE FOR ORDER BY` y que la mediana de `A1_sql` baje de `3.30 ms` a `< 0.30 ms`. |

---

### Etapa 3 — Higiene del Workspace y Hooks (Impacto Medio, Complejidad Baja)

| Campo | R-08: Quitar `--no-gitignore` del hook `skill-registry refresh` y reubicar los `942 MB` de dumps en `.atl/` |
|---|---|
| **Problema** | El hook `gentle-ai skill-registry refresh --quiet --no-gitignore --cwd "$PWD"` corre en cada `UserPromptSubmit` de Claude Code y en cada `SessionStart` de Codex con `--no-gitignore`, mientras `.atl/` acumula **942 MB** de dumps de Postgres y `node_modules/` **292 MB**. |
| **Evidencia** | `/root/.claude/settings.json` (línea 115), `/root/.codex/hooks.json` (línea 166) y `du -sh /root/openvms/.atl/*` (`942 MB`). |
| **Solución** | 1. Quitar `--no-gitignore` del comando `gentle-ai skill-registry refresh` en `/root/.claude/settings.json` y `/root/.codex/hooks.json`, y moverlo únicamente a `SessionStart` (no en cada `UserPromptSubmit`).<br>2. Limpiar los directorios temporales `.atl/demo-cleanup`, `.atl/face-deploy`, `.atl/onvif-deploy` (`414 MB`) o mover la ruta de dumps de `deploy/deploy.sh` fuera del workspace si crecen por encima de `DEPLOY_KEEP=5`. |
| **Impacto** | **Medio** (evita escaneos innecesarios en cada prompt y libera hasta `942 MB` del árbol del proyecto). |
| **Complejidad** | **Baja** |
| **Riesgo** | Nulo (los skills viven en `/root/.claude/skills`, `/root/.codex/skills` y `/root/.gemini/config/skills`, no dentro de directorios ignorados por `.gitignore`). |
| **Validación** | Confirmar que `UserPromptSubmit` en Claude Code ya no invoque `skill-registry refresh` en cada mensaje y que `du -sh /root/openvms/.atl` baje de `942 MB` a `< 150 MB`. |

---

## 12. Resumen de impacto esperado tras aplicar el plan

| Indicador clave (KPI) | Valor actual medido | Meta tras Etapa 1 + Etapa 2 |
|---|---:|---:|
| **Tokens de entrada en Turno 1 (Claude Padre / Subagente)** | `74,356` / `57,175 tok` | **`< 45,000` / `< 30,000 tok`** |
| **Tokens de entrada en Turno 1 (Codex Padre / Subagente)** | `42,550` / `36,451 tok` | **`< 18,000` / `< 12,000 tok`** |
| **Llamadas MCP a Engram por subagente en Codex (mediana)** | `14.0` llamadas | **`0` llamadas** |
| **Llamadas a `mem_judge` por cada `mem_save`** | `1.0 – 3.3` llamadas | **`0` llamadas** (salvo conflicto real de arquitectura) |
| **Bytes devueltos por `rg` en búsquedas de dominio (`ServerFrigateConfig`)** | `27,089 B` (`212` líneas) | **`3,993 B` (`29` líneas, `-85.3%`)** |
| **Tamaño máximo de observación activa en Engram** | `49,216 B` (`odd/*/tasks`) | **`<= 2,000 B`** |
| **Proporción de `session_summary` sobre el total de observaciones** | `52.3%` (`750 / 1,435`) | **`< 5%`** (1 resumen rolling por rama/proyecto) |
| **Tiempo mediano de exploración antes del 1.er `Edit` en subagentes** | `165.4 s` (`23.5` llamadas) | **`< 60 s` (`< 10` llamadas)** |
