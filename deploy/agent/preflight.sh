#!/bin/sh
# Read-only safety gate for the new-host installer. This file is also executed
# directly over SSH before any bundle files are uploaded to the target.
set -eu

refuse() {
  printf 'preflight refused: %s\n' "$1" >&2
  exit 1
}

[ "$(uname -s)" = Linux ] || refuse 'only Linux hosts are supported'
[ "$(id -u)" = 0 ] || refuse 'SSH login must be root'
[ -r /etc/os-release ] || refuse 'cannot identify the Linux distribution'
. /etc/os-release
case "${ID:-}" in
  ubuntu|debian) ;;
  *) refuse 'only Debian and Ubuntu are supported' ;;
esac
command -v apt-get >/dev/null 2>&1 || refuse 'apt-get is required'
command -v systemctl >/dev/null 2>&1 || refuse 'systemd is required'
[ -d /run/systemd/system ] || refuse 'systemd is not running'

case "$(uname -m)" in
  x86_64) goarch=amd64 ;;
  aarch64) goarch=arm64 ;;
  *) refuse 'unsupported CPU architecture' ;;
esac

# The current control-plane build and agent update path support matching host
# architectures only. Do not copy a wrong-architecture binary.
if [ -n "${OPENVMS_EXPECTED_GOARCH:-}" ] && [ "$goarch" != "$OPENVMS_EXPECTED_GOARCH" ]; then
  refuse 'host architecture does not match the available OpenVMS agent build'
fi

if command -v docker >/dev/null 2>&1; then
  containers=$(docker ps -a --format '{{.Names}} {{.Image}}') || refuse 'cannot inspect existing Docker containers safely'
  if printf '%s\n' "$containers" | grep -Eiq 'frigate'; then
    refuse 'an existing Frigate container was detected'
  fi
fi

if ps -eo comm= | grep -Fxq frigate; then
  refuse 'an existing Frigate process was detected'
fi

for path in /opt/openvms/frigate /opt/frigate /srv/frigate /etc/frigate /var/lib/frigate; do
  [ ! -e "$path" ] || refuse 'an existing Frigate installation path was detected'
done

# Recording storage is provided by the operator. Root-disk use is available
# only through an explicit demo opt-in carried from the API request.
[ ! -L /mnt/cctv ] || refuse 'the /mnt/cctv recording path must not be a symlink'
if [ -e /mnt/cctv ] && [ ! -d /mnt/cctv ]; then
  refuse 'the /mnt/cctv recording path exists but is not a directory'
fi
command -v mountpoint >/dev/null 2>&1 || refuse 'mountpoint is required to verify recording storage'
recordings=mounted
if [ ! -d /mnt/cctv ] || ! mountpoint -q /mnt/cctv; then
  [ "${OPENVMS_ALLOW_SYSTEM_DISK:-0}" = 1 ] || refuse 'the required /mnt/cctv recording filesystem is not mounted; demo system-disk use was not authorized'
  recordings=system_disk
fi

if find /etc /opt /srv /home /root -maxdepth 5 -type f \
  \( -iname 'frigate.yml' -o -iname 'frigate.yaml' -o -iname 'frigate.sqlite' \) \
  -print -quit 2>/dev/null | grep -q .; then
  refuse 'an existing Frigate configuration was detected'
fi

printf 'openvms_preflight=ok os_id=%s goarch=%s recordings=%s\n' "$ID" "$goarch" "$recordings"
