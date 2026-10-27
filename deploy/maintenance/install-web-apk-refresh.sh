#!/bin/bash
# Installs the weekly web image apk refresh on this host: script, systemd service and timer,
# logrotate rule. Run as root: bash deploy/maintenance/install-web-apk-refresh.sh
# Remove with: bash deploy/maintenance/install-web-apk-refresh.sh --uninstall
#
# Environment:
#   APK_REFRESH_ROOT   compose project directory the service runs in (default: the repository root)
#   INSTALL_PREFIX     install under this directory instead of / (tests)
#   SKIP_SYSTEMCTL=1   copy files only, do not touch systemd (tests)
set -eu

here=$(cd "$(dirname "$0")" && pwd)
root=${APK_REFRESH_ROOT:-$(cd "$here/../.." && pwd)}
prefix=${INSTALL_PREFIX:-}
sbin="$prefix/usr/local/sbin/openvms-web-apk-refresh"
unit_dir="$prefix/etc/systemd/system"
doc="$prefix/usr/local/share/doc/openvms/maintenance-web-apk-refresh.md"
rotate="$prefix/etc/logrotate.d/openvms-web-apk-refresh"
ctl() { [ "${SKIP_SYSTEMCTL:-0}" = 1 ] || systemctl "$@"; }

if [ "${1:-}" = "--uninstall" ]; then
  ctl disable --now openvms-web-apk-refresh.timer 2>/dev/null || true
  rm -f "$sbin" "$unit_dir/openvms-web-apk-refresh.service" "$unit_dir/openvms-web-apk-refresh.timer" "$rotate" "$doc"
  ctl daemon-reload
  echo "removed (the log /var/log/openvms/web-apk-refresh.log is kept)"
  exit 0
fi

[ "$(id -u)" = 0 ] || [ -n "$prefix" ] || { echo "run as root" >&2; exit 1; }
[ -f "$root/docker-compose.yml" ] || { echo "no docker-compose.yml in $root (set APK_REFRESH_ROOT)" >&2; exit 1; }

install -d "$(dirname "$sbin")" "$unit_dir" "$(dirname "$rotate")" "$(dirname "$doc")" "$prefix/var/log/openvms"
install -m 0755 "$here/web-apk-refresh.sh" "$sbin"
sed "s#@ROOT@#$root#g" "$here/openvms-web-apk-refresh.service" >"$unit_dir/openvms-web-apk-refresh.service"
install -m 0644 "$here/openvms-web-apk-refresh.timer" "$unit_dir/openvms-web-apk-refresh.timer"
install -m 0644 "$here/logrotate-openvms-web-apk-refresh" "$rotate"
[ -f "$here/../../docs/maintenance-web-apk-refresh.md" ] &&
  install -m 0644 "$here/../../docs/maintenance-web-apk-refresh.md" "$doc"

ctl daemon-reload
ctl enable --now openvms-web-apk-refresh.timer
echo "installed. Next run:"
ctl list-timers openvms-web-apk-refresh.timer --no-pager 2>/dev/null || true
echo "log: /var/log/openvms/web-apk-refresh.log   dry run: APK_REFRESH_DRY_RUN=1 /usr/local/sbin/openvms-web-apk-refresh"
