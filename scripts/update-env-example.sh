#!/bin/bash
# Regenerates .env.example with every variable docker-compose.yml reads, documented, and
# optionally adds the variables missing from an existing .env without touching its values.
#
# Usage (from the repository root):
#   bash scripts/update-env-example.sh          # rewrite .env.example (backup in .env.example.bak)
#   bash scripts/update-env-example.sh --env    # also append missing variables to .env
#
# Secrets are never printed. The script never generates OPENVMS_MASTER_KEY for an existing
# .env: changing that key makes the stored Frigate passwords unreadable.
set -eu

root=$(cd "$(dirname "$0")/.." && pwd)
cd "$root"
example=.env.example
tmp=$(mktemp)
trap 'rm -f "$tmp"' EXIT

cat >"$tmp" <<'TEMPLATE'
# OpenVMS environment file.
#
# Copy it to .env (cp .env.example .env), fill in the REQUIRED values and keep .env out of
# version control (chmod 600 .env). `make install` creates .env from this file and generates
# OPENVMS_MASTER_KEY and POSTGRES_PASSWORD for you. Every variable is read by
# docker-compose.yml; an empty value falls back to the default shown in the comment.
# Docker Compose v2 is required (`docker compose`, with a space).

# --- Required secrets ------------------------------------------------------------------

# REQUIRED. Key that encrypts the Frigate passwords stored by OpenVMS: 32 random bytes in
# base64. Generate it once with `openssl rand -base64 32` and never change it on an existing
# install (the stored passwords would become unreadable). Empty = an insecure development key.
OPENVMS_MASTER_KEY=

# REQUIRED. PostgreSQL password. It is applied only when the database volume is created;
# changing it later does not re-key an existing database.
POSTGRES_PASSWORD=

# --- Optional services -----------------------------------------------------------------

# Compose profiles to enable, comma-separated. Leave it commented for the default stack. Use
# `mqtt` to also run the Mosquitto broker (OpenVMS itself does not need it), `observability`
# for Prometheus and Grafana, `onvif-agent` for the local edge agent. Example: mqtt,observability
# COMPOSE_PROFILES=

# --- First administrator ---------------------------------------------------------------

# Platform administrator created on the first start (never resets an existing user).
OPENVMS_ADMIN_USERNAME=admin
# REQUIRED for a fresh install: its password (12+ characters recommended). Empty = a random
# password is generated and printed once in the api logs (`docker compose logs api`).
OPENVMS_ADMIN_PASSWORD=

# --- Default organization and site -----------------------------------------------------

# Created only when the database has no organization yet; later renames are kept.
OPENVMS_DEFAULT_TENANT=openvms
OPENVMS_DEFAULT_SITE=Sitio principal
# IANA time zone of the default site (schedules and reports use it).
OPENVMS_DEFAULT_SITE_TZ=America/Argentina/Cordoba

# --- Web ports and HTTPS ---------------------------------------------------------------

# Host ports of the web interface (HTTP and HTTPS).
WEB_PORT=8000
WEB_HTTPS_PORT=8443
# Address Caddy serves HTTP on inside the container (rarely changed).
SITE_ADDRESS=:80
# HTTPS site: the server IPv4 (e.g. 10.1.1.60) to issue the self-signed certificate for it.
# Default ":443" serves any name with the local certificate.
OPENVMS_HTTPS_SITE_IPV4=
# Extra names/IPs for the self-signed certificate, comma separated
# (default: OPENVMS_HTTPS_SITE_IPV4 or localhost,127.0.0.1,::1).
OPENVMS_HTTPS_SANS=
# Name used when a client connects without SNI (default: OPENVMS_HTTPS_SITE_IPV4 or localhost).
OPENVMS_HTTPS_DEFAULT_SNI=
# Your own certificate and key on the host (PEM). Empty = the self-signed certificate.
OPENVMS_WEB_TLS_CERT_HOST_FILE=
OPENVMS_WEB_TLS_KEY_HOST_FILE=
# Reverse proxies in front of the web allowed to set X-Forwarded-* (CIDRs, comma separated).
# Empty = forwarded headers are not trusted.
HTTPS_INGRESS_PROXY_CIDRS=
# Proxies trusted to forward client credentials to the API (CIDRs). Empty = the internal web
# container (OPENVMS_AGENT_WEB_IPV4).
CREDENTIAL_TRUSTED_PROXY_CIDRS=

# --- Sessions and logging --------------------------------------------------------------

# Maximum session lifetime and idle timeout (Go durations: 30m, 2h, 12h).
SESSION_TTL=12h
SESSION_IDLE=2h
# debug | info | warn | error
LOG_LEVEL=info
# Web feature flags, comma separated.
OPENVMS_FEATURES=persistentPlayers,videoSurfaceLayer

# --- WhatsApp notifications (WAHA) -----------------------------------------------------

# API key between OpenVMS and the internal WAHA gateway. Change it in production.
WAHA_API_KEY=

# --- ONVIF discovery and the local agent -----------------------------------------------

# Networks where ONVIF cameras may be probed (CIDRs, comma separated) and the interfaces
# used for multicast discovery.
ONVIF_ALLOWED_CIDRS=192.0.2.0/24
ONVIF_DISCOVERY_INTERFACES=eth0
# Private bridge between the api, the web and the optional local onvif agent
# (profile onvif-agent). Change it only if it collides with an existing network.
OPENVMS_AGENT_SUBNET=172.29.240.0/24
OPENVMS_AGENT_API_IPV4=172.29.240.2
OPENVMS_AGENT_IPV4=172.29.240.3
OPENVMS_AGENT_WEB_IPV4=172.29.240.4
# Local onvif agent (profile onvif-agent): HTTPS port, user/group and host files.
OPENVMS_AGENT_TLS_PORT=7443
OPENVMS_AGENT_UID=1000
OPENVMS_AGENT_GID=1000
OPENVMS_AGENT_TOKEN_HOST_FILE=./deploy/agent/compose/local/agent.token
OPENVMS_AGENT_TLS_CERT_HOST_FILE=./deploy/agent/compose/local/tls.crt
OPENVMS_AGENT_TLS_KEY_HOST_FILE=./deploy/agent/compose/local/tls.key

# --- Build metadata --------------------------------------------------------------------

# Leave empty: `make deploy`/`make install` and the Dockerfiles read VERSION and
# deploy/agent/VERSION. Set them only to override a build.
VERSION=
AGENT_VERSION=
COMMIT=
BUILD_TIME=

# --- Not set here ----------------------------------------------------------------------
# The gRPC control channel is plaintext unless GRPC_TLS_CERT_FILE and GRPC_TLS_KEY_FILE are
# provided to the api container (see docs/grpc-tls.md).
TEMPLATE

if [ -f "$example" ]; then
  cp -p "$example" "$example.bak"
  echo "Backup: $example.bak"
fi
install -m 0644 "$tmp" "$example"
echo "Updated: $example ($(grep -c '^[A-Z0-9_]*=' "$example") variables)"
if [ -f "$example.bak" ]; then
  echo "--- Changes against the backup (review them; keep anything you had added) ---"
  diff -u "$example.bak" "$example" || true
fi

if [ "${1:-}" = "--env" ]; then
  if [ ! -f .env ]; then
    echo ".env does not exist: run 'make install' or 'cp .env.example .env' first." >&2
    exit 1
  fi
  added=0
  {
    printf '\n# Added by scripts/update-env-example.sh on %s (empty = compose default).\n' "$(date -u +%Y-%m-%d)"
  } >"$tmp"
  for key in $(grep -o '^[A-Z0-9_]*=' "$example" | tr -d '='); do
    if ! grep -q "^${key}=" .env; then
      if [ "$key" = OPENVMS_MASTER_KEY ]; then
        echo "WARNING: .env has no OPENVMS_MASTER_KEY: this install uses the development key." >&2
        echo "  Do not add a new key now: the stored Frigate passwords would become unreadable." >&2
        echo "  Rotating it needs re-entering every Frigate password; see the README." >&2
        continue
      fi
      printf '%s=\n' "$key" >>"$tmp"
      added=$((added + 1))
    fi
  done
  if [ "$added" -gt 0 ]; then
    cp -p .env .env.bak
    cat "$tmp" >>.env
    chmod 600 .env .env.bak
    echo "Added $added missing variable(s) to .env (empty values; backup in .env.bak). Values were not printed."
  else
    echo ".env already has every variable."
  fi
fi
