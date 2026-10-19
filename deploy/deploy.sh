#!/bin/bash
# One-command deploy of the local docker compose stack. Run it with `make deploy`.
#
# Steps: preflight, metadata, protected Postgres dump, rollback tags, build, up,
# health check, summary. Any failure aborts. Uses plain `docker compose`, so an
# untracked docker-compose.override.yml is honored.
#
# Environment:
#   DEPLOY_BRANCH       branch that may be deployed (default: main)
#   DEPLOY_SKIP_PULL=1  skip `git pull --ff-only`
#   DEPLOY_SKIP_BACKUP=1 skip the Postgres dump (explicit opt-out)
#   DEPLOY_DRY_RUN=1    print mutating commands instead of running them
#   DEPLOY_ROOT         repository root (default: parent of this script)
#   DEPLOY_KEEP         newest rollback-* image tags per service and dump dirs kept
#                       after a successful deploy (default: 5)
#   DEPLOY_WAIT         seconds to wait for readiness (default: 120)
#   VERSION             image version label (default: git describe)
#   WEB_HTTPS_PORT      fallback HTTPS port if `docker compose port web 443` fails (default: 8443)
set -eu

SELF=$(cd "$(dirname "$0")" && pwd)/$(basename "$0")
ROOT=${DEPLOY_ROOT:-$(cd "$(dirname "$0")/.." && pwd)}
BRANCH=${DEPLOY_BRANCH:-main}
DRY=${DEPLOY_DRY_RUN:-0}
WAIT=${DEPLOY_WAIT:-120}
KEEP=${DEPLOY_KEEP:-5}
IMAGES="api relay worker web"
TS=$(date -u +%Y%m%dT%H%M%SZ)

step() { printf '\n== [%s/8] %s\n' "$1" "$2"; }
info() { printf '   %s\n' "$*"; }
die() { printf 'deploy: ERROR: %s\n' "$*" >&2; exit 1; }

# run prints the command in dry-run mode and executes it otherwise.
run() {
  if [ "$DRY" = 1 ]; then printf '   + %s\n' "$*"; else "$@"; fi
}

cd "$ROOT" || die "cannot enter $ROOT"

# --- 1. Preflight -----------------------------------------------------------
step 1 preflight
git rev-parse --is-inside-work-tree >/dev/null 2>&1 || die "$ROOT is not a git repository"
[ -z "$(git status --porcelain --untracked-files=no)" ] ||
  die "working tree is not clean (commit or stash tracked changes first)"
current=$(git rev-parse --abbrev-ref HEAD)
[ "$current" = "$BRANCH" ] ||
  die "on branch '$current', expected '$BRANCH' (set DEPLOY_BRANCH to override)"
info "branch $current, tree clean"
if [ "${DEPLOY_SKIP_PULL:-0}" = 1 ]; then
  info "pull skipped (DEPLOY_SKIP_PULL=1)"
else
  before=$(git rev-parse HEAD)
  run git pull --ff-only || die "git pull --ff-only failed"
  after=$(git rev-parse HEAD)
  if [ "$before" != "$after" ]; then
    # bash reads the script incrementally; run the pulled version from the start.
    info "pull moved HEAD ($before -> $after); re-executing $(basename "$SELF") with the pulled version"
    DEPLOY_SKIP_PULL=1 exec bash "$SELF" "$@"
  fi
fi
if [ "$DRY" = 1 ]; then
  info "dry run: docker checks skipped"
else
  docker compose version >/dev/null 2>&1 || die "docker compose is not available"
  pg_running() { [ -n "$(docker compose ps --status running --quiet postgres 2>/dev/null)" ]; }
  if ! pg_running; then
    info "postgres is not running, starting it"
    docker compose up -d --wait postgres || die "could not start postgres"
  fi
  pg_running || die "postgres is not running"
fi

# --- 2. Metadata ------------------------------------------------------------
step 2 metadata
COMMIT=$(git rev-parse --short HEAD)
BUILD_TIME=$(date -u +%Y-%m-%dT%H:%M:%SZ)
VERSION=${VERSION:-$(git describe --tags --always 2>/dev/null || echo "$COMMIT")}
export COMMIT BUILD_TIME VERSION

migration_version() {
  docker compose exec -T postgres psql -U openvms -d openvms -Atc \
    "select coalesce(max(version_id), 0) from goose_db_version where is_applied" 2>/dev/null || echo unknown
}
if [ "$DRY" = 1 ]; then MIG_BEFORE=dry-run; else MIG_BEFORE=$(migration_version); fi
info "commit $COMMIT, version $VERSION, built $BUILD_TIME, migration $MIG_BEFORE"

# --- 3. Protected dump ------------------------------------------------------
step 3 backup
DUMP="" SHA=""
if [ "${DEPLOY_SKIP_BACKUP:-0}" = 1 ]; then
  info "WARNING: backup skipped (DEPLOY_SKIP_BACKUP=1)"
else
  dir="$ROOT/.atl/deploy/$TS"
  DUMP="$dir/postgres.dump"
  if [ "$DRY" = 1 ]; then
    info "+ pg_dump -Fc via docker compose exec -T postgres > $DUMP"
    info "+ sha256sum $DUMP; pg_restore --list in a throwaway postgres:17-alpine --network none"
  else
    (umask 077 && mkdir -p "$dir") && chmod 700 "$dir"
    (umask 077 && docker compose exec -T postgres pg_dump -U openvms -d openvms -Fc >"$DUMP") ||
      die "pg_dump failed"
    chmod 600 "$DUMP"
    [ -s "$DUMP" ] || die "dump is empty: $DUMP"
    SHA=$(sha256sum "$DUMP" | cut -d' ' -f1)
    docker run --rm -i --network none postgres:17-alpine pg_restore --list <"$DUMP" >/dev/null ||
      die "dump verification (pg_restore --list) failed: $DUMP"
    info "dump $DUMP"
    info "sha256 $SHA (verified with pg_restore --list)"
  fi
fi

# --- 4. Rollback tags -------------------------------------------------------
step 4 "rollback tags"
TAGGED=0
for svc in $IMAGES; do
  if [ "$DRY" = 1 ]; then
    info "+ docker tag openvms-$svc:latest openvms-$svc:rollback-$TS (if the image exists)"
  elif docker image inspect "openvms-$svc:latest" >/dev/null 2>&1; then
    docker tag "openvms-$svc:latest" "openvms-$svc:rollback-$TS"
    TAGGED=$((TAGGED + 1))
    info "tagged openvms-$svc:rollback-$TS"
  else
    info "openvms-$svc has no image yet, skipped"
  fi
done

if [ "$DRY" != 1 ] && [ "$TAGGED" -eq 0 ]; then
  info "no image was tagged (first deploy): no rollback available for images"
fi

rollback_help() {
  if [ "$DRY" = 1 ] || [ "$TAGGED" -gt 0 ]; then
    echo "Rollback (images only, no database restore):  make rollback TAG=$TS"
  else
    echo "No rollback available for images: nothing was tagged (first deploy)."
  fi
  if [ -n "$DUMP" ]; then
    echo "If new migrations were applied, also restore the database from the dump (destructive, review first):"
    echo "  docker compose exec -T postgres pg_restore -U openvms -d openvms --clean --if-exists < $DUMP"
  fi
}

# --- 5. Build ---------------------------------------------------------------
step 5 build
run docker compose build --pull=false || { rollback_help; die "build failed (running containers untouched)"; }

# --- 6. Up ------------------------------------------------------------------
step 6 up
run docker compose up -d || { rollback_help; die "docker compose up failed"; }

# --- 7. Health --------------------------------------------------------------
step 7 health
# The published port may come from .env or an override, so ask compose.
PORT=""
if [ "$DRY" != 1 ]; then
  PORT=$(docker compose port web 443 2>/dev/null | head -n1 | sed 's/.*://')
fi
case $PORT in ''|*[!0-9]*) PORT=${WEB_HTTPS_PORT:-8443} ;; esac
url="https://localhost:$PORT/health/ready"
if [ "$DRY" = 1 ]; then
  info "+ curl -fsk $url (--connect-timeout 3 --max-time 5, up to ${WAIT}s, expects \"status\":\"ok\")"
else
  waited=0
  until curl -fsk --connect-timeout 3 --max-time 5 "$url" 2>/dev/null | grep -Eq '"status" *: *"ok"'; do
    if [ "$waited" -ge "$WAIT" ]; then
      docker compose logs --tail 50 api || true
      rollback_help
      die "$url not ready after ${WAIT}s"
    fi
    sleep 3
    waited=$((waited + 3))
  done
  info "ready after ~${waited}s"
fi

# --- 8. Summary -------------------------------------------------------------
step 8 summary
if [ "$DRY" = 1 ]; then MIG_AFTER=dry-run; else MIG_AFTER=$(migration_version); fi
echo "commit:     $COMMIT ($VERSION)"
echo "migration:  $MIG_BEFORE -> $MIG_AFTER"
echo "dump:       ${DUMP:-none}${SHA:+ (sha256 $SHA)}"
if [ "$DRY" = 1 ] || [ "$TAGGED" -gt 0 ]; then echo "rollback:   tag rollback-$TS"; else echo "rollback:   none (first deploy)"; fi
rollback_help

# --- Retention --------------------------------------------------------------
# Keep the newest $KEEP rollback-* tags per service and dump dirs; never remove
# the ones created by this run. Timestamps sort lexicographically.
case $KEEP in ''|*[!0-9]*) KEEP=5 ;; esac
prune() {
  echo
  echo "retention: keeping the newest $KEEP rollback tags per service and dump dirs"
  for svc in $IMAGES; do
    docker image ls "openvms-$svc" --format '{{.Tag}}' 2>/dev/null | grep '^rollback-' | sort -ru |
      tail -n +"$((KEEP + 1))" | while read -r t; do
        [ "$t" = "rollback-$TS" ] && continue
        if [ "$DRY" = 1 ]; then echo "   would remove image tag openvms-$svc:$t"
        else docker rmi "openvms-$svc:$t" >/dev/null 2>&1 && echo "   removed image tag openvms-$svc:$t"; fi
      done || true
  done
  if [ -d "$ROOT/.atl/deploy" ]; then
    (cd "$ROOT/.atl/deploy" && ls -1 | grep -E '^[0-9]{8}T[0-9]{6}Z$' | sort -ru) | tail -n +"$((KEEP + 1))" |
      while read -r d; do
        [ "$d" = "$TS" ] && continue
        if [ "$DRY" = 1 ]; then echo "   would remove dump dir .atl/deploy/$d"
        else rm -rf "$ROOT/.atl/deploy/$d" && echo "   removed dump dir .atl/deploy/$d"; fi
      done || true
  fi
  return 0
}
prune
