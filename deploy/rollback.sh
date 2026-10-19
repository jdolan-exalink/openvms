#!/bin/bash
# Image-only rollback: retag openvms-<svc>:rollback-<TAG> to :latest and restart
# without building. It does NOT restore the database (see the dump printed by
# deploy.sh). Usage: deploy/rollback.sh <TAG>   (or: make rollback TAG=<TAG>)
#
# Atomic: every service is checked for its rollback tag before anything is
# retagged. If a retag still fails midway, the previous :latest of the services
# already retagged is restored and the script exits non-zero without `up`.
# Services that never had a rollback image are skipped, but at least one
# service must be rolled back.
set -eu

TAG=${1:-${TAG:-}}
[ -n "$TAG" ] || { echo "usage: deploy/rollback.sh <timestamp>" >&2; exit 1; }
cd "$(cd "$(dirname "$0")/.." && pwd)"

# Phase 1: validate every tag before touching anything.
todo=""
for svc in api relay worker web; do
  if docker image inspect "openvms-$svc:rollback-$TAG" >/dev/null 2>&1; then
    todo="$todo $svc"
  else
    echo "rollback: no rollback-$TAG image for $svc, skipped"
  fi
done
[ -n "$todo" ] || { echo "rollback: ERROR: no service has tag rollback-$TAG, stack untouched" >&2; exit 1; }

# Phase 2: retag, remembering the previous :latest image id for each service.
done_svcs=""
restore() {
  for entry in $done_svcs; do
    svc=${entry%%=*}
    prev=${entry#*=}
    if [ -n "$prev" ]; then
      docker tag "$prev" "openvms-$svc:latest" ||
        echo "rollback: WARNING: could not restore openvms-$svc:latest to $prev" >&2
    fi
  done
}
for svc in $todo; do
  prev=$(docker image inspect --format '{{.Id}}' "openvms-$svc:latest" 2>/dev/null || true)
  if docker tag "openvms-$svc:rollback-$TAG" "openvms-$svc:latest"; then
    done_svcs="$done_svcs $svc=$prev"
  else
    restore
    echo "rollback: ERROR: docker tag failed for $svc, previous images restored, stack untouched" >&2
    exit 1
  fi
done
echo "rollback: retagged:$todo"
docker compose up -d --no-build
