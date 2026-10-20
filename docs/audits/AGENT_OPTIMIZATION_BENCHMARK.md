# Benchmark Comparativo Antes vs. Después (Etapa 8 y Etapa 9)

**Fecha de ejecución:** 2026-10-20  
**Repositorio:** `/root/openvms` (`main` @ `6d39b75`, `0.4.0`)

---

## 1. Tabla consolidada Antes vs. Después

| Indicador / Métrica | Baseline (Antes) | Después de Optimización (Etapas 1–6) | Variación medida | Estado frente a Meta |
|---|---:|---:|---:|---|
| **Instrucciones estáticas globales en Codex (`AGENTS.md` + `engram-instructions.md`)** | `109,618 B` (`~27,404 tok`) | **`7,992 B` (`~1,998 tok`)** | **`-92.7%` (`-25,406 tok/turno`)** | Cumplido (`< 8 KB`) |
| **Instrucciones estáticas globales en Claude Code (`CLAUDE.md`)** | `69,471 B` (`~17,367 tok`) | **`7,992 B` (`~1,998 tok`)** | **`-88.5%` (`-15,369 tok/turno`)** | Cumplido (`< 8 KB`) |
| **Instrucciones estáticas globales en Antigravity (`GEMINI.md`)** | `87,957 B` (`~21,989 tok`) | **`7,992 B` (`~1,998 tok`)** | **`-90.9%` (`-19,991 tok/turno`)** | Cumplido (`< 8 KB`) |
| **Mapa arquitectónico local del repo (`/root/openvms/{AGENTS,CLAUDE}.md`)** | `0 B` *(inexistente)* | **`4,678 B` (`~1,169 tok`)** | Mapa de 7 apps + 30 paquetes `internal/*` disponible en Turno 1 | Cumplido |
| **Búsqueda `rg -n ServerFrigateConfig .` (con `.rgignore`)** | `212` líneas / `27,089 B` (`~6,772 tok`) | **`29` líneas / `3,993 B` (`~998 tok`)** | **`-85.3%` en bytes (`-86.3%` líneas)** | Cumplido (Meta: `65–85%`) |
| **Búsqueda `rg -n FrigateCameraConfig .` (con `.rgignore`)** | `35` líneas / `7,114 B` (`~1,778 tok`) | **`18` líneas / `2,296 B` (`~574 tok`)** | **`-67.7%` en bytes (`-48.6%` líneas)** | Cumplido (Meta: `65–85%`) |
| **Búsqueda `rg -n enrollment .` (con `.rgignore`)** | `216` líneas / `50,570 B` (`~12,642 tok`) | **`139` líneas / `21,948 B` (`~5,487 tok`)** | **`-56.6%` en bytes** | Cumplido |
| **`mem_context` payload (`compact=false` vs `compact=true`)** | `8,423 B` (`~2,105 tok`, `45.80 ms`) | **`2,144 B` (`~536 tok`, `43.62 ms`)** | **`-74.5%` en bytes** | Cumplido |
| **`mem_search` payload (`response_format="compact"` vs formato texto legado `limit=5..10`)** | `15,910 B` (`~3,977 tok`) | **`7,467 B` (`~1,866 tok`)** | **`-53.1%` en bytes** | Cumplido |
| **Servidores MCP `engram` cargados en Antigravity** | `2` (`engram` + `gentle-ai-engram_engram`, `44` herramientas) | **`1` (`engram --tools=agent`, `14` herramientas)** | **`-68.2%` definiciones de herramientas MCP** | Cumplido |
| **Servidor MCP `codegraph` en Codex (`/root/.codex/config.toml`)** | No configurado (`0` herramientas) | **Activo (`codegraph_explore` verificado por `stdio`)** | Paridad completa Claude Code ↔ Codex | Cumplido |
| **Hook `PreToolUse` en cada `Agent` de Claude Code (`sdd-preflight-hook`)** | Fallaba con `exit 1` tras `129.04 ms` (máx `714.53 ms`) | **`0 ms` (eliminado subcomando inexistente en `gentle-ai 4.2.0`)** | **`-100%` de latencia fallida en `PreToolUse`** | Cumplido |
| **Hook `skill-registry refresh` en Claude Code** | Ejecutado en **cada prompt (`UserPromptSubmit`)** con `--no-gitignore` (`7.01–27.31 ms`) | Ejecutado **solo en `SessionStart`** respetando `.gitignore` (`0 ms` por prompt) | **`-100%` en `UserPromptSubmit`** | Cumplido |
| **SQLite `mem_context` / `mem_search` (Benchmark en copia aislada Etapa 7)** | `2.278 ms` / `27.349 ms` (`9.65 MB` FTS data) | **`0.028 ms` (`81.4x`) / `0.578 ms` (`47.3x`, `3.91 MB` FTS data)** | **`-98.8%` y `-97.9%` latencia SQL** (listo para aplicar bajo autorización) | Validado en copia aislada |

---

## 2. Impacto en el Contexto del Turno 1 y en Subagentes

1. **Reducción directa de tokens fijos por turno**:
   - **Codex Padre y Subagentes**: pasan de cargar `109,618 B` (`AGENTS.md` + `engram-instructions.md`, `~27,404 tokens`) a `7,992 B` (`AGENTS.md` global) + `4,678 B` (`AGENTS.md` del repo) = **`12,670 B` (`~3,167 tokens`)**, ahorrando **`~24,237 tokens` en el Turno 1** (y en cada turno subsecuente de la conversación).
   - **Claude Code Padre y Subagentes**: pasan de `69,471 B` (`~17,367 tokens`) a `7,992 B` (`CLAUDE.md` global) + `4,678 B` (`CLAUDE.md` del repo) = **`12,670 B` (`~3,167 tokens`)**, ahorrando **`~14,200 tokens` en el Turno 1**.
   - **Antigravity**: pasa de `87,957 B` (`~21,989 tokens`) + `44` herramientas Engram duplicadas a `7,992 B` (`~1,998 tokens`) + `14` herramientas Engram (`--tools=agent`).
2. **Eliminación de ceremonias Engram en subagentes (`Parent-Owned Memory`)**:
   - Con la regla `Memory-Free by default` para subagentes y la prohibición de espejar `odd/<slug>/tasks` completos en Engram, las `14.0` llamadas medianas a Engram por subagente en Codex (`mem_current_project`, `mem_context`, `mem_review`, `mem_search`, `mem_get_observation`, `mem_save`, `mem_judge`, `mem_session_summary`) se reducen a **`0` llamadas en ejecuciones estándar**.

---

## 3. Verificación funcional de extremo a extremo (Etapa 8)

| Verificación | Comando / Método ejecutado | Resultado |
|---|---|---|
| **Claude Code CLI** | `claude --version` + validación JSON de `/root/.claude/settings.json` | `2.1.303 (Claude Code)` — OK |
| **Codex CLI** | `codex --version` + validación `tomllib.load("/root/.codex/config.toml")` | `codex-cli 0.163.0` (`context7`, `engram`, `codegraph` registrados) — OK |
| **Engram MCP (`stdio`)** | `initialize` + `mem_current_project` + `mem_context(compact=true)` sobre `/usr/local/bin/engram mcp --tools=agent` | Respuesta JSON-RPC válida (`project: "openvms"`) — OK |
| **Integridad de `/root/.engram/engram.db`** | `PRAGMA integrity_check` + `SELECT COUNT(*) FROM observations WHERE deleted_at IS NULL` | `ok`, `1,435` observaciones activas (sin alteración) — OK |
| **CodeGraph MCP (`stdio`) y CLI** | `codegraph status` + `codegraph serve --mcp` (`initialize` + `tools/list` + `codegraph_explore`) | `1,129` archivos indexados, herramienta `codegraph_explore` activa — OK |
| **Skill Registry (`.atl/skill-registry.md`)** | `gentle-ai skill-registry refresh --force --cwd /root/openvms` | `8` skills indexados (incluyendo `orchestrator-reference`) — OK |
| **Búsqueda explícita en rutas excluidas por `.rgignore`** | `rg -n ServerFrigateConfig packages/api-contract/openapi.yaml` y `internal/api/gen/api.gen.go` | `5` líneas y `166` líneas devueltas en `< 5 ms` — OK |
