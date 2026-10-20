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
claimed=0
if [ -L "$output_dir" ] || { [ -e "$output_dir" ] && [ ! -d "$output_dir" ]; }; then
  echo "output path must be a real directory" >&2
  exit 1
fi
if [ ! -e "$output_dir" ]; then
  if ! mkdir -m 700 -- "$output_dir"; then
    echo "output directory appeared during bootstrap; refusing to use it" >&2
    exit 1
  fi
  claimed=1
fi
chmod 700 -- "$output_dir"
lock=$output_dir/.local-bootstrap-lock
if ! mkdir -- "$lock" 2>/dev/null; then
  echo "another local TLS bootstrap is active or left a lock at $lock" >&2
  exit 1
fi
cleanup() {
  if [ "$claimed" -eq 1 ]; then
    rm -f -- "$output_dir/tls.key" "$output_dir/tls.crt"
  fi
  rmdir "$lock" 2>/dev/null || true
  if [ "$claimed" -eq 1 ]; then
    rmdir -- "$output_dir" 2>/dev/null || true
  fi
}
trap cleanup EXIT
trap 'exit 1' HUP INT TERM
umask 077

web_cert=$output_dir/tls.crt
web_key=$output_dir/tls.key
if [ -e "$web_cert" ] || [ -e "$web_key" ]; then
  [ -f "$web_cert" ] && [ ! -L "$web_cert" ] && [ -s "$web_cert" ] &&
    [ -f "$web_key" ] && [ ! -L "$web_key" ] && [ -s "$web_key" ] || {
      echo "HTTPS certificate and key must both be regular, non-empty files" >&2; exit 1;
    }
  openssl x509 -in "$web_cert" -noout -checkend 0 >/dev/null
  openssl x509 -in "$web_cert" -noout -checkip "$site_ip" | grep -Fx "IP $site_ip does match certificate" >/dev/null || {
    echo "Existing HTTPS certificate does not contain IP SAN $site_ip" >&2; exit 1;
  }
  cert_pub=$(openssl x509 -in "$web_cert" -pubkey -noout | openssl pkey -pubin -outform DER | sha256sum | awk '{print $1}')
  key_pub=$(openssl pkey -in "$web_key" -pubout -outform DER | sha256sum | awk '{print $1}')
  [ "$cert_pub" = "$key_pub" ] || { echo "HTTPS certificate and key do not match" >&2; exit 1; }
  chmod 600 "$web_key" "$web_cert"
else
  claimed=1
  # This is a local self-signed IP certificate for explicit browser trust only.
  openssl req -x509 -newkey rsa:3072 -nodes -sha256 -days 365 \
    -keyout "$web_key" -out "$web_cert" \
    -subj "/CN=OpenVMS local HTTPS $site_ip" \
    -addext "subjectAltName=IP:$site_ip" \
    -addext "basicConstraints=critical,CA:TRUE" \
    -addext "keyUsage=critical,digitalSignature,keyEncipherment,keyCertSign" \
    -addext "extendedKeyUsage=serverAuth" >/dev/null 2>&1
  chmod 600 "$web_key" "$web_cert"
  openssl x509 -in "$web_cert" -noout -checkend 0 >/dev/null
fi

# Compose bind-mounts the dedicated agent gRPC TLS files by default. Create
# them before the stack starts so Docker cannot silently create directories at
# the missing file paths. The certificate SAN uses the same advertised host.
script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
sh "$script_dir/bootstrap-agent-grpc-tls.sh" "$site_ip" "$output_dir/agent-grpc"

claimed=0
rmdir "$lock"
trap - EXIT HUP INT TERM
echo "Validated local HTTPS certificate and private key in $output_dir."
echo "The browser must explicitly trust this self-signed certificate; do not bypass certificate errors."
