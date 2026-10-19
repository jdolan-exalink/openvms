#!/bin/bash
# Image-only rollback: retag openvms-<svc>:rollback-<TAG> to :latest and restart
# without building. It does NOT restore the database (see the dump printed by
# deploy.sh). Usage: deploy/rollback.sh <TAG>   (or: make rollback TAG=<TAG>)
# Fails, without touching the stack, when no service has that rollback tag or a
# retag fails.
set -eu

TAG=${1:-${TAG:-}}
[ -n "$TAG" ] || { echo "usage: deploy/rollback.sh <timestamp>" >&2; exit 1; }
cd "$(cd "$(dirname "$0")/.." && pwd)"

retagged=""
for svc in api relay worker web; do
  if docker image inspect "openvms-$svc:rollback-$TAG" >/dev/null 2>&1; then
    docker tag "openvms-$svc:rollback-$TAG" "openvms-$svc:latest" ||
      { echo "rollback: ERROR: docker tag failed for $svc, stack untouched" >&2; exit 1; }
    retagged="$retagged $svc"
  else
    echo "rollback: no rollback-$TAG image for $svc, skipped"
  fi
done
[ -n "$retagged" ] || { echo "rollback: ERROR: no service has tag rollback-$TAG, stack untouched" >&2; exit 1; }
echo "rollback: retagged:$retagged"
docker compose up -d --no-build
