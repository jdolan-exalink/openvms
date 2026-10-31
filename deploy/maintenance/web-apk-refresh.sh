#!/bin/bash
# Weekly Alpine package refresh of the running web image.
#
# The web image is built on caddy:2-alpine. Its `apk upgrade` layer is cached, so a fixed
# vulnerability in a base library (the CI image scan fails the build on HIGH or CRITICAL
# findings that have a fix) would otherwise stay in the running container until the next build
# without cache. This script upgrades the packages of the image that is running right now,
# on top of that same image, so it does not depend on the checkout or its branch.
#
# Steps: preflight, package list before, upgrade layer (no cache), package list after, swap with a
# rollback tag, readiness check (rollback on failure), retention. Every step is logged.
#
# Environment:
#   APK_REFRESH_LOG       log file (default: /var/log/openvms/web-apk-refresh.log)
#   APK_REFRESH_ROOT      compose project directory (default: parent of the maintenance directory)
#   APK_REFRESH_IMAGE     compose image name of the web service (default: openvms-web)
#   APK_REFRESH_KEEP      newest rollback-*-apk tags kept (default: 5)
#   APK_REFRESH_WAIT      seconds to wait for readiness (default: 120)
#   APK_REFRESH_DRY_RUN=1 build and report what would change, keep the running container
#   APK_REFRESH_LOCK      lock file (default: /run/lock/openvms-web-apk-refresh.lock)
#   APK_REFRESH_DEPLOY_PATTERN  pgrep pattern of a running deploy (default: deploy/deploy\.sh)
#
# Exit: 0 refreshed or nothing to do (also when skipped), 1 failure (the previous image is back).
set -eu -o pipefail
export LC_ALL=C

ROOT=${APK_REFRESH_ROOT:-$(cd "$(dirname "$0")/../.." && pwd)}
LOG=${APK_REFRESH_LOG:-/var/log/openvms/web-apk-refresh.log}
IMAGE=${APK_REFRESH_IMAGE:-openvms-web}
KEEP=${APK_REFRESH_KEEP:-5}
WAIT=${APK_REFRESH_WAIT:-120}
DRY=${APK_REFRESH_DRY_RUN:-0}
LOCK=${APK_REFRESH_LOCK:-/run/lock/openvms-web-apk-refresh.lock}
TS=$(date -u +%Y%m%dT%H%M%SZ)
WORK=$(mktemp -d)
base=
# BuildKit reads a bare image id in FROM as a registry reference, so the running image gets a local tag.
trap 'rm -rf "$WORK"; [ -z "$base" ] || docker rmi "$base" >/dev/null 2>&1 || true' EXIT

mkdir -p "$(dirname "$LOG")"
log() { printf '%s %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$*" | tee -a "$LOG"; }
fail() { log "ERROR: $*"; exit 1; }

log "== web apk refresh $TS (dry-run=$DRY)"

# A refresh and a deploy both recreate the web container: never at the same time.
exec 9>"$LOCK"
flock -n 9 || { log "another refresh is running; skipped"; exit 0; }
if pgrep -f "${APK_REFRESH_DEPLOY_PATTERN:-deploy/deploy\.sh}" >/dev/null 2>&1; then
  log "a deploy is running; skipped (the next run picks it up)"
  exit 0
fi

cd "$ROOT" || fail "cannot enter $ROOT"
cid=$(docker compose ps -q web 2>/dev/null | head -n1 || true)
if [ -z "$cid" ]; then
  log "the web container is not running; nothing to refresh"
  exit 0
fi
cur=$(docker inspect -f '{{.Image}}' "$cid") || fail "cannot inspect the web container"
log "running image $cur"

if ! docker run --rm --entrypoint sh "$cur" -c 'command -v apk' >/dev/null 2>&1; then
  log "the running image has no apk; nothing to refresh"
  exit 0
fi

# name -> version, sorted: "curl 8.22.0-r0". apk prints name-version-rN, so strip the last two dashes.
pkgs() {
  docker run --rm --entrypoint apk "$1" list -I |
    awk '{ n = $1; sub(/-[^-]+-r[0-9]+$/, "", n); v = substr($1, length(n) + 2); print n, v }' | sort
}
pkgs "$cur" >"$WORK/before" || fail "cannot list the installed packages"
log "packages installed before: $(wc -l <"$WORK/before" | tr -d ' ')"

new="$IMAGE:apk-$TS"
base="$IMAGE:apk-base-$TS"
docker tag "$cur" "$base"
log "building the upgrade layer without cache ($new)"
if ! printf 'FROM %s\nRUN apk upgrade --no-cache\n' "$base" |
  DOCKER_BUILDKIT=1 docker build --no-cache --progress=plain -t "$new" - >"$WORK/build.out" 2>&1; then
  sed -E 's/^/    build: /' "$WORK/build.out" | tail -n 30 | tee -a "$LOG"
  fail "the upgrade layer failed to build (the running container is untouched)"
fi
grep -E 'Upgrading|Installing|Purging|^OK:|WARNING|ERROR' "$WORK/build.out" | sed -E 's/^#[0-9]+ [0-9.]+ //; s/^/    apk: /' | tee -a "$LOG" || true

pkgs "$new" >"$WORK/after" || fail "cannot list the packages of the upgraded image"
join -a1 -a2 -e '-' -o '0,1.2,2.2' "$WORK/before" "$WORK/after" |
  awk '$2 != $3 { printf "    %s: %s -> %s\n", $1, $2, $3; n++ } END { exit n > 0 ? 0 : 1 }' >"$WORK/changes" ||
  true
changed=$(wc -l <"$WORK/changes" | tr -d ' ')
if [ "$changed" -eq 0 ]; then
  log "no package updates; the running image is current"
  docker rmi "$new" >/dev/null 2>&1 || true
  exit 0
fi
log "packages updated: $changed"
tee -a "$LOG" <"$WORK/changes"

if [ "$DRY" = 1 ]; then
  log "dry-run: the running container is untouched"
  docker rmi "$new" >/dev/null 2>&1 || true
  exit 0
fi

rb="rollback-$TS-apk"
docker tag "$cur" "$IMAGE:$rb"
docker tag "$new" "$IMAGE:latest"
docker rmi "$new" >/dev/null 2>&1 || true
log "swapping the web container (rollback tag $IMAGE:$rb)"
if ! docker compose up -d --no-build --no-deps --wait --wait-timeout "$WAIT" web >"$WORK/up.out" 2>&1; then
  tail -n 20 "$WORK/up.out" | sed -E 's/^/    compose: /' | tee -a "$LOG"
  log "the refreshed web container did not become ready; restoring $IMAGE:$rb"
  docker tag "$IMAGE:$rb" "$IMAGE:latest"
  docker compose up -d --no-build --no-deps --wait --wait-timeout "$WAIT" web >/dev/null 2>&1 ||
    fail "rollback also failed: restore by hand with: make rollback TAG=$rb"
  fail "refresh rolled back; the previous image is running again"
fi
log "the web container is ready on the refreshed image"

# Keep the newest $KEEP apk rollback tags; deploy.sh prunes its own rollback tags.
docker image ls "$IMAGE" --format '{{.Tag}}' 2>/dev/null | grep -E '^rollback-.*-apk$' | sort -ru |
  awk -v keep="$KEEP" 'NR > keep' |
  while read -r t; do
    docker rmi "$IMAGE:$t" >/dev/null 2>&1 && log "removed old rollback tag $t"
  done || true

log "== done: $changed packages updated, rollback tag $rb"
