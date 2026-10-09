#!/bin/sh
set -eu

if [ "$#" -ne 2 ]; then
  echo "usage: $0 <https-site-ipv4> <new-output-directory>" >&2
  exit 2
fi
site_ip=$1
output_dir=$2

case "$site_ip" in
  *[!0-9.]*|.*|*..*|*.) echo "HTTPS site address must be an IPv4 literal" >&2; exit 2 ;;
esac
if ! printf '%s\n' "$site_ip" | awk -F. 'NF == 4 { for (i=1; i<=4; i++) if ($i !~ /^[0-9]+$/ || $i < 0 || $i > 255) exit 1; exit 0 } { exit 1 }'; then
  echo "HTTPS site address must be an IPv4 literal" >&2
  exit 2
fi

parent=$(dirname -- "$output_dir")
mkdir -p -- "$parent"
# Claim the output atomically so concurrent setup cannot replace existing files.
if ! mkdir -m 700 -- "$output_dir"; then
  echo "output directory already exists; refusing to overwrite" >&2
  exit 1
fi
claimed=1
cleanup() {
  if [ "$claimed" -eq 1 ]; then
    rm -f -- "$output_dir/tls.key" "$output_dir/tls.crt"
    rmdir -- "$output_dir" 2>/dev/null || true
  fi
}
trap cleanup EXIT
trap 'exit 1' HUP INT TERM
umask 077

# This is a local self-signed IP certificate for explicit browser trust only.
# It is not production PKI and is never generated automatically by Caddy.
openssl req -x509 -newkey rsa:3072 -nodes -sha256 -days 365 \
  -keyout "$output_dir/tls.key" -out "$output_dir/tls.crt" \
  -subj "/CN=OpenVMS local HTTPS $site_ip" \
  -addext "subjectAltName=IP:$site_ip" \
  -addext "basicConstraints=critical,CA:TRUE" \
  -addext "keyUsage=critical,digitalSignature,keyEncipherment,keyCertSign" \
  -addext "extendedKeyUsage=serverAuth" >/dev/null 2>&1
chmod 600 "$output_dir/tls.key" "$output_dir/tls.crt"
openssl x509 -in "$output_dir/tls.crt" -noout -checkip "$site_ip" >/dev/null

claimed=0
trap - EXIT HUP INT TERM
echo "Created local HTTPS certificate and private key in $output_dir."
echo "The browser must explicitly trust this self-signed certificate; do not bypass certificate errors."
