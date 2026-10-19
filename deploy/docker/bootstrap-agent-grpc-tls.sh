#!/bin/sh
set -eu

if [ "$#" -ne 2 ]; then
  echo "usage: $0 <public-agent-grpc-host> <new-output-directory>" >&2
  exit 2
fi
host=$1
output_dir=$2

# Keep the value safe for both the OpenSSL subject and SAN arguments. Compose's
# default is OPENVMS_HTTPS_SITE_IPV4 (or localhost); explicit DNS names are also
# supported when OPENVMS_AGENT_GRPC_PUBLIC_ADDR is configured separately.
case "$host" in
  ''|*[!a-zA-Z0-9.:-]*|.*|*.|*..*) echo "Agent gRPC host must be a DNS name or IP literal" >&2; exit 2 ;;
esac
case "$host" in
  *:*) san="IP:$host"; check_kind=ip ;;
  *[!0-9.]*) san="DNS:$host"; check_kind=dns ;;
  *) san="IP:$host"; check_kind=ip ;;
esac
if [ "$check_kind" = dns ]; then
  [ "${#host}" -le 253 ] || { echo "Agent gRPC DNS name is too long" >&2; exit 2; }
  printf '%s\n' "$host" | awk -F. '{for (i=1; i<=NF; i++) if ($i == "" || length($i) > 63 || $i ~ /^-/ || $i ~ /-$/) exit 1}' || {
    echo "Agent gRPC host must be a valid DNS name" >&2
    exit 2
  }
fi

# Claim a brand-new private directory before creating any material; never rotate
# or overwrite credentials an API container may already be using.
parent=$(dirname -- "$output_dir")
mkdir -p -- "$parent"
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

openssl req -x509 -newkey rsa:3072 -nodes -sha256 -days 365 \
  -keyout "$output_dir/tls.key" -out "$output_dir/tls.crt" \
  -subj "/CN=OpenVMS agent gRPC $host" \
  -addext "subjectAltName=$san" \
  -addext "basicConstraints=critical,CA:TRUE" \
  -addext "keyUsage=critical,digitalSignature,keyEncipherment,keyCertSign" \
  -addext "extendedKeyUsage=serverAuth" >/dev/null 2>&1
chmod 600 "$output_dir/tls.key"
chmod 644 "$output_dir/tls.crt"
openssl x509 -in "$output_dir/tls.crt" -noout -checkend 0 >/dev/null
if [ "$check_kind" = ip ]; then
  openssl x509 -in "$output_dir/tls.crt" -noout -checkip "$host" >/dev/null
else
  openssl x509 -in "$output_dir/tls.crt" -noout -checkhost "$host" >/dev/null
fi
openssl verify -CAfile "$output_dir/tls.crt" "$output_dir/tls.crt" >/dev/null
cert_pub=$(openssl x509 -in "$output_dir/tls.crt" -pubkey -noout | openssl pkey -pubin -outform DER | sha256sum | awk '{print $1}')
key_pub=$(openssl pkey -in "$output_dir/tls.key" -pubout -outform DER | sha256sum | awk '{print $1}')
[ "$cert_pub" = "$key_pub" ] || { echo "Agent gRPC certificate and key do not match." >&2; exit 1; }

claimed=0
trap - EXIT HUP INT TERM
echo "Created dedicated agent gRPC TLS certificate and private key in $output_dir."
echo "Trust tls.crt explicitly on agents; do not disable certificate verification."
