#!/bin/sh
set -eu
. /usr/local/bin/prepare-web-tls.sh
exec caddy run --config /etc/caddy/Caddyfile --adapter caddyfile
