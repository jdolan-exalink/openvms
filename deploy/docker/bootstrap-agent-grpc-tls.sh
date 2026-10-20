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

# Existing Docker bind-mount placeholders are recoverable only when empty; rmdir
# is the proof that no data is being discarded. Never replace non-empty paths.
parent=$(dirname -- "$output_dir")
mkdir -p -- "$parent"
if [ -L "$output_dir" ] || { [ -e "$output_dir" ] && [ ! -d "$output_dir" ]; }; then
  echo "gRPC TLS output path must be a real directory" >&2; exit 1
fi
mkdir -p -m 700 -- "$output_dir"
chmod 700 -- "$output_dir"
lock=$output_dir/.bootstrap-lock
if ! mkdir -- "$lock" 2>/dev/null; then
  echo "another gRPC TLS bootstrap is active or left a lock at $lock" >&2; exit 1
fi
trap 'rmdir "$lock" 2>/dev/null || true' EXIT
for file in "$output_dir/tls.crt" "$output_dir/tls.key"; do
  if [ -d "$file" ] && [ ! -L "$file" ]; then
    rmdir -- "$file" 2>/dev/null || { echo "refusing non-empty directory at file target: $file" >&2; exit 1; }
  fi
done
if [ -e "$output_dir/tls.crt" ] || [ -L "$output_dir/tls.crt" ] || [ -e "$output_dir/tls.key" ] || [ -L "$output_dir/tls.key" ]; then
  [ -f "$output_dir/tls.crt" ] && [ ! -L "$output_dir/tls.crt" ] && [ -s "$output_dir/tls.crt" ] &&
    [ -f "$output_dir/tls.key" ] && [ ! -L "$output_dir/tls.key" ] && [ -s "$output_dir/tls.key" ] || {
      echo "gRPC certificate and key must both be regular, non-empty files" >&2; exit 1;
    }
  openssl x509 -in "$output_dir/tls.crt" -noout -checkend 0 >/dev/null
  if [ "$check_kind" = ip ]; then
    openssl x509 -in "$output_dir/tls.crt" -noout -ext subjectAltName | grep -F "IP Address:$host" >/dev/null || { echo "gRPC certificate SAN does not match $host" >&2; exit 1; }
  else
    openssl x509 -in "$output_dir/tls.crt" -noout -checkhost "$host" >/dev/null
  fi
  openssl verify -CAfile "$output_dir/tls.crt" "$output_dir/tls.crt" >/dev/null
  cert_pub=$(openssl x509 -in "$output_dir/tls.crt" -pubkey -noout | openssl pkey -pubin -outform DER | sha256sum | awk '{print $1}')
  key_pub=$(openssl pkey -in "$output_dir/tls.key" -pubout -outform DER | sha256sum | awk '{print $1}')
  [ "$cert_pub" = "$key_pub" ] || { echo "Agent gRPC certificate and key do not match." >&2; exit 1; }
  chmod 600 "$output_dir/tls.key"
  chmod 644 "$output_dir/tls.crt"
  echo "Validated existing dedicated agent gRPC TLS pair in $output_dir."
  exit 0
fi
claimed=1
cleanup() {
  if [ "$claimed" -eq 1 ]; then
    rm -f -- "$output_dir/tls.key" "$output_dir/tls.crt"
  fi
  rmdir "$lock" 2>/dev/null || true
  [ "$claimed" -eq 1 ] && rmdir -- "$output_dir" 2>/dev/null || true
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
rmdir "$lock"
trap - EXIT HUP INT TERM
echo "Created dedicated agent gRPC TLS certificate and private key in $output_dir."
echo "Trust tls.crt explicitly on agents; do not disable certificate verification."
