#!/bin/sh
set -eu

if [ "$#" -ne 2 ]; then
  echo "usage: $0 <agent-ipv4> <new-output-directory>" >&2
  exit 2
fi
agent_ip=$1
output_dir=$2

case "$agent_ip" in
  *[!0-9.]*|.*|*..*|*.) echo "agent address must be an IPv4 literal" >&2; exit 2 ;;
esac
if ! printf '%s\n' "$agent_ip" | awk -F. 'NF == 4 { for (i=1; i<=4; i++) if ($i !~ /^[0-9]+$/ || $i < 0 || $i > 255) exit 1; exit 0 } { exit 1 }'; then
  echo "agent address must be an IPv4 literal" >&2
  exit 2
fi

parent=$(dirname -- "$output_dir")
mkdir -p -- "$parent"
# Claim the final path atomically before creating any secret material. mkdir
# fails if another invocation or operator already created any target path.
if ! mkdir -m 700 -- "$output_dir"; then
  echo "output directory already exists; refusing to overwrite" >&2
  exit 1
fi
claimed=1
cleanup() {
  if [ "$claimed" -eq 1 ]; then
    # Remove only artifacts this invocation can have created. rmdir refuses
    # to remove the claimed directory if unexpected contents appeared.
    rm -f -- "$output_dir/tls.key" "$output_dir/tls.crt" "$output_dir/agent.token"
    rmdir -- "$output_dir" 2>/dev/null || true
  fi
}
trap cleanup EXIT
trap 'exit 1' HUP INT TERM
umask 077

# The self-signed certificate is its own trust anchor and has the exact fixed
# agent IPv4 in its SAN. The API must be configured to trust this certificate.
openssl req -x509 -newkey rsa:3072 -nodes -sha256 -days 365 \
  -keyout "$output_dir/tls.key" -out "$output_dir/tls.crt" \
  -subj "/CN=OpenVMS local ONVIF agent $agent_ip" \
  -addext "subjectAltName=IP:$agent_ip" \
  -addext "basicConstraints=critical,CA:TRUE" \
  -addext "keyUsage=critical,digitalSignature,keyEncipherment,keyCertSign" \
  -addext "extendedKeyUsage=serverAuth" >/dev/null 2>&1
openssl rand -hex 32 > "$output_dir/agent.token"
chmod 600 "$output_dir/tls.key" "$output_dir/agent.token"
chmod 644 "$output_dir/tls.crt"
openssl x509 -in "$output_dir/tls.crt" -noout -checkip "$agent_ip" >/dev/null

claimed=0
trap - EXIT HUP INT TERM
echo "Created local agent TLS certificate and token files in $output_dir."
echo "The token is not registered with the API; deployment remains disabled until secure local registration is implemented."
