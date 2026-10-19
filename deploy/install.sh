#!/bin/bash
# One-step first install of the local docker compose stack. Run it with `make install`.
#
# Steps: preflight (Docker + Compose v2), .env with generated secrets, build and up,
# readiness check, summary. Never overwrites an existing .env. For later updates use
# `make deploy`.
#
# Environment:
#   INSTALL_DRY_RUN=1       print mutating commands instead of running them
#   INSTALL_ROOT            repository root (default: parent of this script)
#   INSTALL_ENV_EXAMPLE     template for a new .env (default: $INSTALL_ROOT/.env.example)
#   INSTALL_WAIT            seconds to wait for readiness (default: 180)
#   VERSION                 image version label (default: git describe)
#   WEB_HTTPS_PORT          fallback HTTPS port if `docker compose port web 443` fails (default: 8443)
set -eu

ROOT=${INSTALL_ROOT:-$(cd "$(dirname "$0")/.." && pwd)}
ENV_EXAMPLE=${INSTALL_ENV_EXAMPLE:-$ROOT/.env.example}
DRY=${INSTALL_DRY_RUN:-0}
WAIT=${INSTALL_WAIT:-180}

step() { printf '\n== [%s/5] %s\n' "$1" "$2"; }
info() { printf '   %s\n' "$*"; }
warn() { printf '   WARNING: %s\n' "$*"; }
die() { printf 'install: ERROR: %s\n' "$*" >&2; exit 1; }

# run prints the command in dry-run mode and executes it otherwise.
run() {
  if [ "$DRY" = 1 ]; then printf '   + %s\n' "$*"; else "$@"; fi
}

cd "$ROOT" || die "cannot enter $ROOT"

# --- 1. Preflight -----------------------------------------------------------
step 1 preflight
if [ "$DRY" = 1 ]; then
  info "dry run: docker checks skipped"
else
  command -v docker >/dev/null 2>&1 || die "docker is not installed (https://docs.docker.com/engine/install/)"
  docker info >/dev/null 2>&1 || die "the Docker daemon is not reachable (is it running? does your user have access to it?)"
  compose_major=""
  if cv=$(docker compose version 2>/dev/null); then
    compose_major=$(printf '%s\n' "$cv" | sed -n 's/.*[vV]\{0,1\}\([0-9][0-9]*\)\.[0-9][0-9]*.*/\1/p' | head -n1)
  fi
  case $compose_major in ''|*[!0-9]*) compose_major=0 ;; esac
  if [ "$compose_major" -lt 2 ]; then
    if command -v docker-compose >/dev/null 2>&1; then
      v1=$(docker-compose version 2>/dev/null | head -n1 || true)
      die "Compose v1 is not supported (found: ${v1:-docker-compose}). Install the Compose v2 plugin (apt-get install -y docker-compose-v2, or docker-compose-plugin from Docker's repository) and use 'docker compose' with a space, not 'docker-compose'."
    fi
    die "Docker Compose v2 is not available. Install the plugin (apt-get install -y docker-compose-v2, or docker-compose-plugin from Docker's repository)."
  fi
  info "docker ok, compose v$compose_major"
fi

# --- 2. Environment file ----------------------------------------------------
step 2 ".env"

rand_b64() {
  openssl rand -base64 32 2>/dev/null || head -c 32 /dev/urandom | base64
}
rand_alnum() {
  head -c 64 /dev/urandom | base64 | tr -dc 'A-Za-z0-9' | head -c 24
}
# set_kv FILE KEY VALUE replaces the KEY= line or appends it; the value never
# reaches argv or the output (it travels through the environment).
set_kv() {
  _v=$3 awk -v k="$2" '
    index($0, k "=") == 1 { print k "=" ENVIRON["_v"]; done = 1; next }
    { print }
    END { if (!done) print k "=" ENVIRON["_v"] }' "$1" >"$1.tmp" && mv "$1.tmp" "$1"
}

if [ -e .env ]; then
  info ".env already exists, left untouched"
  for key in OPENVMS_MASTER_KEY POSTGRES_PASSWORD; do
    grep -q "^$key=" .env ||
      warn "$key is not set in .env: compose falls back to its insecure development default"
  done
elif [ "$DRY" = 1 ]; then
  info "+ create .env (mode 0600) from $ENV_EXAMPLE with a generated OPENVMS_MASTER_KEY and POSTGRES_PASSWORD"
else
  [ -f "$ENV_EXAMPLE" ] || die "$ENV_EXAMPLE not found"
  (umask 077 && cp "$ENV_EXAMPLE" .env) || die "could not create .env"
  chmod 600 .env
  master=$(rand_b64)
  pgpass=$(rand_alnum)
  [ -n "$master" ] && [ -n "$pgpass" ] || { rm -f .env; die "could not generate secrets"; }
  (umask 077 && set_kv .env OPENVMS_MASTER_KEY "$master" && set_kv .env POSTGRES_PASSWORD "$pgpass") ||
    { rm -f .env; die "could not write secrets to .env"; }
  chmod 600 .env
  info "created .env (mode 0600) with a generated OPENVMS_MASTER_KEY and POSTGRES_PASSWORD (not printed)"
  info "back up OPENVMS_MASTER_KEY: encrypted data cannot be recovered without it"
fi
if [ "$DRY" != 1 ] && docker volume ls --format '{{.Name}}' 2>/dev/null | grep -q 'postgres'; then
  warn "a postgres volume already exists: it keeps the password it was created with, and changing POSTGRES_PASSWORD does not re-key the database"
fi

# --- 3. Build and up --------------------------------------------------------
step 3 "build and up"
COMMIT=$(git rev-parse --short HEAD 2>/dev/null || echo unknown)
BUILD_TIME=$(date -u +%Y-%m-%dT%H:%M:%SZ)
VERSION=${VERSION:-$(git describe --tags --always 2>/dev/null || echo "$COMMIT")}
export COMMIT BUILD_TIME VERSION
info "commit $COMMIT, version $VERSION, built $BUILD_TIME"
run docker compose up -d --build || die "docker compose up failed"

# --- 4. Health --------------------------------------------------------------
step 4 health
PORT="" HTTP_PORT=""
if [ "$DRY" != 1 ]; then
  PORT=$(docker compose port web 443 2>/dev/null | head -n1 | sed 's/.*://')
  HTTP_PORT=$(docker compose port web 80 2>/dev/null | head -n1 | sed 's/.*://')
fi
case $PORT in ''|*[!0-9]*) PORT=${WEB_HTTPS_PORT:-8443} ;; esac
case $HTTP_PORT in ''|*[!0-9]*) HTTP_PORT=8000 ;; esac
url="https://localhost:$PORT/health/ready"
if [ "$DRY" = 1 ]; then
  info "+ curl -fsk $url (--connect-timeout 3 --max-time 5, up to ${WAIT}s, expects \"status\":\"ok\")"
else
  waited=0
  until curl -fsk --connect-timeout 3 --max-time 5 "$url" 2>/dev/null | grep -Eq '"status" *: *"ok"'; do
    if [ "$waited" -ge "$WAIT" ]; then
      docker compose logs --tail 50 api || true
      die "$url not ready after ${WAIT}s"
    fi
    sleep 3
    waited=$((waited + 3))
  done
  info "ready after ~${waited}s"
fi

# --- 5. Summary -------------------------------------------------------------
step 5 summary
echo "http:   http://localhost:$HTTP_PORT"
echo "https:  https://localhost:$PORT (self-signed certificate unless you configured your own)"
echo
echo "Next step, create the first admin (choose a long password):"
echo "  docker compose exec api /vmsctl bootstrap -password '<una-clave-larga>'"
echo
echo "For later updates use: make deploy"
