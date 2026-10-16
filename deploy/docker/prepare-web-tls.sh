#!/bin/sh
set -eu

cert_file=${OPENVMS_WEB_TLS_CERT_FILE:-}
key_file=${OPENVMS_WEB_TLS_KEY_FILE:-}
if [ -n "$cert_file" ] || [ -n "$key_file" ]; then
  if [ -z "$cert_file" ] || [ -z "$key_file" ] || [ ! -s "$cert_file" ] || [ ! -s "$key_file" ]; then
    echo "Both OPENVMS_WEB_TLS_CERT_HOST_FILE and OPENVMS_WEB_TLS_KEY_HOST_FILE must name readable, non-empty certificate files." >&2
    exit 1
  fi
  openssl x509 -in "$cert_file" -noout >/dev/null || { echo "Configured HTTPS certificate is invalid: $cert_file" >&2; exit 1; }
  openssl pkey -in "$key_file" -noout >/dev/null || { echo "Configured HTTPS private key is invalid: $key_file" >&2; exit 1; }
  cert_pub=$(openssl x509 -in "$cert_file" -pubkey -noout | openssl pkey -pubin -outform DER | sha256sum | awk '{print $1}')
  key_pub=$(openssl pkey -in "$key_file" -pubout -outform DER | sha256sum | awk '{print $1}')
  [ "$cert_pub" = "$key_pub" ] || { echo "Configured HTTPS certificate and key do not match." >&2; exit 1; }
  export CADDY_TLS_CERT_FILE=$cert_file CADDY_TLS_KEY_FILE=$key_file
  return 0 2>/dev/null || exit 0
fi

dir=${OPENVMS_WEB_TLS_DIR:-/var/lib/openvms/tls}
sans=${OPENVMS_HTTPS_SANS:-localhost,127.0.0.1,::1}
default_sni=${OPENVMS_HTTPS_DEFAULT_SNI:-localhost}
old_ifs=$IFS
IFS=,
set -- $sans
IFS=$old_ifs
[ "$#" -gt 0 ] || { echo "OPENVMS_HTTPS_SANS must contain at least one DNS name or IP address." >&2; exit 1; }
san_ext=
has_default_sni=false
for san do
  [ -n "$san" ] || { echo "OPENVMS_HTTPS_SANS contains an empty entry." >&2; exit 1; }
  case "$san" in *[!a-zA-Z0-9.:-]*|.*|*.|*..*) echo "Invalid HTTPS SAN entry: $san" >&2; exit 1;; esac
  case "$san" in
    *:*) kind=IP
      case "$san" in *[!0-9a-fA-F:]*|*:::*|*::*::*) echo "Invalid IPv6 HTTPS SAN: $san" >&2; exit 1;; esac
      printf '%s\n' "$san" | awk -F: 'BEGIN { valid=1 } { groups=0; for (i=1; i<=NF; i++) if ($i != "") { if (length($i)>4) valid=0; groups++ } if (index($0,"::") > 0) { if (groups >= 8) valid=0 } else if (groups != 8) valid=0 } END { exit !valid }' || { echo "Invalid IPv6 HTTPS SAN: $san" >&2; exit 1; } ;;
    *[!0-9.]* ) kind=DNS ;;
    *.*) kind=IP
      echo "$san" | awk -F. 'NF != 4 {exit 1} {for (i=1; i<=4; i++) if ($i !~ /^[0-9]+$/ || length($i)>3 || $i > 255) exit 1}' || { echo "Invalid IPv4 HTTPS SAN: $san" >&2; exit 1; } ;;
    *) kind=DNS ;;
  esac
  [ "$san" = "$default_sni" ] && has_default_sni=true
  if [ "$kind" = DNS ]; then
    [ "${#san}" -le 253 ] || { echo "DNS HTTPS SAN is too long: $san" >&2; exit 1; }
    case "$san" in *[!a-zA-Z0-9.-]*|[.-]*|*[-.]) echo "Invalid DNS HTTPS SAN: $san" >&2; exit 1;; esac
    printf '%s\n' "$san" | awk -F. '{for (i=1; i<=NF; i++) if (length($i) > 63) exit 1}' || { echo "DNS HTTPS SAN label exceeds 63 characters: $san" >&2; exit 1; }
    san_ext=${san_ext}${san_ext:+,}DNS:$san
  else
    san_ext=${san_ext}${san_ext:+,}IP:$san
  fi
done
[ "$has_default_sni" = true ] || { echo "OPENVMS_HTTPS_DEFAULT_SNI must also appear in OPENVMS_HTTPS_SANS." >&2; exit 1; }

mkdir -p "$dir"
chmod 700 "$dir"
lock=$dir/.generation-lock
lock_wait_seconds=${OPENVMS_TLS_LOCK_WAIT_SECONDS:-30}
case "$lock_wait_seconds" in ''|*[!0-9]*) echo "OPENVMS_TLS_LOCK_WAIT_SECONDS must be an integer from 1 to 300." >&2; exit 1;; esac
[ "$lock_wait_seconds" -ge 1 ] && [ "$lock_wait_seconds" -le 300 ] || { echo "OPENVMS_TLS_LOCK_WAIT_SECONDS must be an integer from 1 to 300." >&2; exit 1; }
lock_waited=0
while ! mkdir "$lock" 2>/dev/null; do
  if [ "$lock_waited" -ge "$lock_wait_seconds" ]; then
    echo "Timed out waiting for HTTPS certificate generation lock at $lock. If this persists, stop the web container and verify no startup process is using this TLS volume before removing the stale lock and retrying." >&2
    exit 1
  fi
  sleep 1
  lock_waited=$((lock_waited + 1))
done
trap 'rmdir "$lock" 2>/dev/null || true' EXIT HUP INT TERM
current=$dir/current
current_generation=$(readlink "$current" 2>/dev/null || true)
for abandoned in "$dir"/.generation-*; do
  [ "$abandoned" = "$lock" ] && continue
  [ -d "$abandoned" ] || continue
  [ ! -L "$abandoned" ] || continue
  [ "${abandoned##*/}" = "$current_generation" ] && continue
  rm -rf "$abandoned"
done
rotate=true
if [ -L "$current" ] && [ -s "$current/tls.crt" ] && [ -s "$current/tls.key" ] && [ -s "$current/sans" ]; then
  if [ "$(cat "$current/sans")" = "$san_ext" ] && openssl x509 -in "$current/tls.crt" -noout -checkend 2592000 >/dev/null 2>&1; then rotate=false; fi
fi
if [ "$rotate" = true ]; then
  generation=$(mktemp -d "$dir/.generation-XXXXXXXXXX")
  trap 'rm -rf "$generation"; rmdir "$lock" 2>/dev/null || true' EXIT HUP INT TERM
  openssl req -x509 -newkey rsa:3072 -nodes -sha256 -days 365 \
    -subj "/CN=${1:-localhost}" -addext "subjectAltName=$san_ext" \
    -addext "basicConstraints=critical,CA:FALSE" -addext "extendedKeyUsage=serverAuth" \
    -addext "keyUsage=digitalSignature,keyEncipherment" \
    -keyout "$generation/tls.key" -out "$generation/tls.crt" >/dev/null 2>&1
  printf '%s' "$san_ext" > "$generation/sans"
  chmod 600 "$generation/tls.key" "$generation/sans"
  chmod 644 "$generation/tls.crt"
  openssl x509 -in "$generation/tls.crt" -noout -checkend 0 >/dev/null
  cert_pub=$(openssl x509 -in "$generation/tls.crt" -pubkey -noout | openssl pkey -pubin -outform DER | sha256sum | awk '{print $1}')
  key_pub=$(openssl pkey -in "$generation/tls.key" -pubout -outform DER | sha256sum | awk '{print $1}')
  [ "$cert_pub" = "$key_pub" ] || { echo "Generated HTTPS certificate and key do not match." >&2; exit 1; }
  ln -s "$(basename "$generation")" "$dir/.current-$$"
  previous=$(readlink "$current" 2>/dev/null || true)
  mv -Tf "$dir/.current-$$" "$current"
  trap 'rmdir "$lock" 2>/dev/null || true' EXIT HUP INT TERM
  case "$previous" in .generation-*) rm -rf "$dir/$previous";; esac
fi
rmdir "$lock"
trap - EXIT HUP INT TERM
export CADDY_TLS_CERT_FILE=$current/tls.crt CADDY_TLS_KEY_FILE=$current/tls.key
