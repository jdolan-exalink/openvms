# Plan de Rollback y Restauración Verificable (Etapa 9)

**Fecha de creación:** 2026-10-20  
**Directorio de Backup:** `/root/.agent-optimization-backups/20261020T181320Z`  
**Manifiesto de integridad:** `/root/.agent-optimization-backups/20261020T181320Z/SHA256SUMS`

---

## 1. Verificación previa del Backup

Antes de restaurar cualquier archivo, comprobar que el backup sigue íntegro:

```bash
cd /root/.agent-optimization-backups/20261020T181320Z && sha256sum -c SHA256SUMS
```

---

## 2. Rollback granular por etapa

### Revertir Etapa 1, 2 y 3 (Instrucciones globales y contrato de persistencia)

```bash
BACKUP="/root/.agent-optimization-backups/20261020T181320Z"

cp -a "$BACKUP/claude/CLAUDE.md" /root/.claude/CLAUDE.md
cp -a "$BACKUP/codex/AGENTS.md" /root/.codex/AGENTS.md
cp -a "$BACKUP/codex/engram-instructions.md" /root/.codex/engram-instructions.md
cp -a "$BACKUP/gemini/GEMINI.md" /root/.gemini/GEMINI.md

cp -a "$BACKUP/claude/skills/_shared/persistence-contract.md" /root/.claude/skills/_shared/persistence-contract.md
cp -a "$BACKUP/codex/skills/_shared/persistence-contract.md" /root/.codex/skills/_shared/persistence-contract.md
cp -a "$BACKUP/gemini/config/skills/_shared/persistence-contract.md" /root/.gemini/config/skills/_shared/persistence-contract.md

rm -rf /root/.claude/skills/orchestrator-reference \
       /root/.codex/skills/orchestrator-reference \
       /root/.gemini/config/skills/orchestrator-reference \
       /root/.gemini/skills/orchestrator-reference \
       /root/.codex/skills/_shared/sdd-orchestrator-workflow.md \
       /root/.gemini/config/skills/_shared/sdd-orchestrator-workflow.md

gentle-ai skill-registry refresh --force --cwd /root/openvms
```

### Revertir Etapa 4 (Navegación del monorepo: `.rgignore`, `AGENTS.md`, `CLAUDE.md`)

```bash
rm -f /root/openvms/.rgignore /root/openvms/AGENTS.md /root/openvms/CLAUDE.md
```

### Revertir Etapa 5 (Configuración de Codex `config.toml`)

```bash
BACKUP="/root/.agent-optimization-backups/20261020T181320Z"
cp -a "$BACKUP/codex/config.toml" /root/.codex/config.toml
```

### Revertir Etapa 6 (Hooks de Claude Code y configuración MCP de Antigravity)

```bash
BACKUP="/root/.agent-optimization-backups/20261020T181320Z"
cp -a "$BACKUP/claude/settings.json" /root/.claude/settings.json
cp -a "$BACKUP/gemini/config/mcp_config.json" /root/.gemini/config/mcp_config.json
cp -a "$BACKUP/gemini/config/plugins/gentle-ai-engram/mcp_config.json" /root/.gemini/config/plugins/gentle-ai-engram/mcp_config.json
cp -a "$BACKUP/gemini/config/plugins/gentle-ai-engram/hooks.json" /root/.gemini/config/plugins/gentle-ai-engram/hooks.json
```

---

## 3. Rollback completo en un solo comando (Etapas 1 a 6)

```bash
set -eu
BACKUP="/root/.agent-optimization-backups/20261020T181320Z"
(cd "$BACKUP" && sha256sum -c SHA256SUMS >/dev/null)

cp -a "$BACKUP/claude/CLAUDE.md" /root/.claude/CLAUDE.md
cp -a "$BACKUP/claude/settings.json" /root/.claude/settings.json
cp -a "$BACKUP/claude/skills/_shared/persistence-contract.md" /root/.claude/skills/_shared/persistence-contract.md

cp -a "$BACKUP/codex/AGENTS.md" /root/.codex/AGENTS.md
cp -a "$BACKUP/codex/config.toml" /root/.codex/config.toml
cp -a "$BACKUP/codex/engram-instructions.md" /root/.codex/engram-instructions.md
cp -a "$BACKUP/codex/hooks.json" /root/.codex/hooks.json
cp -a "$BACKUP/codex/skills/_shared/persistence-contract.md" /root/.codex/skills/_shared/persistence-contract.md

cp -a "$BACKUP/gemini/GEMINI.md" /root/.gemini/GEMINI.md
cp -a "$BACKUP/gemini/config/mcp_config.json" /root/.gemini/config/mcp_config.json
cp -a "$BACKUP/gemini/config/plugins/gentle-ai-engram/mcp_config.json" /root/.gemini/config/plugins/gentle-ai-engram/mcp_config.json
cp -a "$BACKUP/gemini/config/plugins/gentle-ai-engram/hooks.json" /root/.gemini/config/plugins/gentle-ai-engram/hooks.json
cp -a "$BACKUP/gemini/config/skills/_shared/persistence-contract.md" /root/.gemini/config/skills/_shared/persistence-contract.md

rm -rf /root/.claude/skills/orchestrator-reference \
       /root/.codex/skills/orchestrator-reference \
       /root/.gemini/config/skills/orchestrator-reference \
       /root/.gemini/skills/orchestrator-reference \
       /root/.codex/skills/_shared/sdd-orchestrator-workflow.md \
       /root/.gemini/config/skills/_shared/sdd-orchestrator-workflow.md

rm -f /root/openvms/.rgignore /root/openvms/AGENTS.md /root/openvms/CLAUDE.md
gentle-ai skill-registry refresh --force --cwd /root/openvms
```

---

## 4. Restauración de emergencia de Engram SQLite (si se autoriza la Etapa 7 en el futuro)

La base `/root/.engram/engram.db` no fue modificada durante las Etapas 0–9. En caso de ejecutar en el futuro el mantenimiento de la Etapa 7 y requerir restauración:

```bash
BACKUP="/root/.agent-optimization-backups/20261020T181320Z"
rm -f /root/.engram/engram.db /root/.engram/engram.db-wal /root/.engram/engram.db-shm
cp -a "$BACKUP/engram/engram-consistent-snapshot.db" /root/.engram/engram.db
chmod 600 /root/.engram/engram.db
```
