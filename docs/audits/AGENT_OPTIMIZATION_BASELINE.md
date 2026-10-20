# Baseline de Optimización de Agentes, Engram y MCP (Etapa 0)

**Fecha de captura:** 2026-10-20T18:13:20Z  
**Repositorio:** `/root/openvms` (`main` @ `6d39b75`, `0.4.0`)  
**Directorio de Backup Verificado:** `/root/.agent-optimization-backups/20261020T181320Z` (`chmod 700`, `SHA256SUMS` verificado, snapshot SQLite con `PRAGMA integrity_check = ok` y `1,435` observaciones intactas).

---

## 1. Estado de Git preexistente

- Rama: `main...origin/main` (limpia respecto a archivos trackeados).
- Rutas no trackeadas preexistentes:
  - `data/` (datos locales de Postgres/servicios — **intocable**).
  - `docs/audits/AGENT_PERFORMANCE_AUDIT.md` (informe de diagnóstico previo).

---

## 2. Inventario y verificación de Backups (`20261020T181320Z`)

| Subdirectorio de backup | Tamaño | Contenido respaldado | Estado de verificación |
|---|---:|---|---|
| `codex/` | `76 MB` | `AGENTS.md`, `config.toml`, `engram-instructions.md`, `engram-compact-prompt.md`, `hooks.json`, `skills/`, `rules/`, `plugins/` | `sha256sum -c OK` |
| `claude/` | `2.9 MB` | `CLAUDE.md`, `settings.json`, `dot-claude.json`, `agents/`, `skills/`, `commands/`, `output-styles/` | `sha256sum -c OK` |
| `gemini/` | `411 KB` | `GEMINI.md`, `settings.json`, `system.md`, `antigravity/`, `config/` | `sha256sum -c OK` |
| `engram/` | `26 MB` | `engram.db`, `engram.db-wal`, `engram.db-shm`, `protocol-mode.json`, `.instance-id` + `engram-consistent-snapshot.db` | `PRAGMA integrity_check = ok` (`1,435` filas) |
| `repo/` | `1.0 KB` | `.gitignore` | `sha256sum -c OK` |

---

## 3. Métricas Baseline antes de cambios

### 3.1 Tamaño de instrucciones cargadas automáticamente en Turno 1

| Archivo / Fuente | Bytes | Líneas | Tokens estimados (~4 B/tok) |
|---|---:|---:|---:|
| `/root/.codex/AGENTS.md` | `100,234 B` | `899` | `~25,058 tok` |
| `/root/.codex/engram-instructions.md` (vía `config.toml`) | `9,384 B` | `143` | `~2,346 tok` |
| **Total instrucciones estáticas Codex** | **`109,618 B`** | **`1,042`** | **`~27,404 tok`** |
| `/root/.claude/CLAUDE.md` | `69,471 B` | `477` | `~17,367 tok` |
| `/root/.gemini/GEMINI.md` | `87,957 B` | `814` | `~21,989 tok` |

### 3.2 Telemetría de sesiones reales (Baseline histórico sobre 433 sesiones)

| Métrica | Claude Padre (`18`) | Claude Subagente (`176`) | Codex Padre (`98`) | Codex Subagente (`173`) |
|---|---:|---:|---:|---:|
| **Tokens de entrada en Turno 1 (mediana)** | `74,356 tok` | `57,175 tok` | `42,550 tok` | `36,451 tok` |
| **Consultas Engram por sesión (mediana / media / total)** | `~20` / `29.8` / `536` | `0.0` / `0.1` / `22` | `0.0` / `24.3` / `2,150` | **`14.0` / `22.8` / `3,445`** |
| **Llamadas `mem_judge` (total)** | `372` (`3.29` por `mem_save`) | `12` | `416` | `655` |
| **Llamadas `mem_session_summary` (total)** | `10` | `2` | `323` | **`557`** |
| **Tiempo hasta primera edición `Edit`/`Write` (mediana / media)** | `1,570.4 s` / `12,650.1 s` | **`165.4 s` / `207.1 s`** (`23.5` llamadas) | N/A (`exec`) | N/A (`exec`) |
| **Duración de subagentes (mediana / p90)** | N/A | **`357.9 s` / `1,495.1 s`** | N/A | **`343.0 s` / `7,297.5 s`** |
| **Redundancia de lectura de archivos en sesiones multi-subagente** | — | `34.5% – 44.8%` | — | `52.6% – 84.7%` |

### 3.3 Búsquedas en el monorepo y MCP (Baseline)

| Prueba Baseline | Resultado medido |
|---|---|
| `rg -n FrigateCameraConfig .` (sin `.rgignore`) | `35` líneas, **`7,114 bytes` (`~1,778 tok`)**, mediana `13.67 ms` |
| `rg -n ServerFrigateConfig .` (sin `.rgignore`) | `212` líneas, **`27,089 bytes` (`~6,772 tok`)**, mediana `13.79 ms` |
| `rg -n enrollment .` (sin `.rgignore`) | `216` líneas, **`50,570 bytes` (`~12,642 tok`)**, mediana `11.82 ms` |
| `engram mcp --tools=agent` (`mem_context` default vs `compact=true`) | Default: **`8,423 B`** (`45.80 ms`) / Compact: **`2,144 B`** (`43.62 ms`) |
| `engram mcp --tools=agent` (`mem_search` formato legado `limit=10` vs `response_format="compact"` `limit=5`) | Legado: **`7,467 B`** (`34.62 ms`) / `response_format="compact"`: **`15,910 B`** (`24.40 ms`) |
| `codegraph status` / `codegraph callers FrigateCameraConfig` | Operativo (`1,129` archivos, `19,816` nodos, `66,221` aristas) / `12` líneas, **`270 B`**, mediana `129.22 ms` |
| CodeGraph MCP en `/root/.codex/config.toml` | **No configurado** (`0` llamadas en Codex) |
| Servidores MCP en Antigravity (`/root/.gemini/`) | `engram` duplicado (`engram` + `gentle-ai-engram_engram`) |
