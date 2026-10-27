#!/bin/bash
# Tests for deploy/maintenance/web-apk-refresh.sh and its installer using a stubbed docker.
# Usage: bash deploy/maintenance/web_apk_refresh_test.sh
set -u
here=$(cd "$(dirname "$0")" && pwd)
tmp=$(mktemp -d)
[ -n "${KEEP_TMP:-}" ] || trap 'rm -rf "$tmp"' EXIT
fails=0
fail() { echo "FAIL: $1"; fails=$((fails + 1)); }

mkdir "$tmp/bin"
cat >"$tmp/bin/docker" <<'STUB'
#!/bin/sh
echo "docker $*" >>"$STUB_LOG"
case "$*" in
  "compose ps -q web") [ "${STUB_NO_WEB:-0}" = 1 ] || echo cid123 ;;
  "inspect -f {{.Image}} cid123") echo sha256:old ;;
  "run --rm --entrypoint sh sha256:old -c command -v apk") [ "${STUB_NO_APK:-0}" = 1 ] && exit 1; exit 0 ;;
  "run --rm --entrypoint apk "*" list -I")
    case "$5" in
      *apk-*) if [ "${STUB_UPDATE:-0}" = 1 ]; then echo "curl-8.23.0-r0 x86_64 {curl} (curl) [installed]"; else echo "curl-8.22.0-r0 x86_64 {curl} (curl) [installed]"; fi ;;
      *) echo "curl-8.22.0-r0 x86_64 {curl} (curl) [installed]" ;;
    esac
    echo "busybox-1.37.0-r1 x86_64 {busybox} (GPL-2.0-only) [installed]" ;;
  "build "*) cat >"$STUB_BUILD_INPUT"; [ "${STUB_BUILD_FAIL:-0}" = 1 ] && { echo "boom"; exit 1; }
             echo "#5 2.1 (1/1) Upgrading curl (8.22.0-r0 -> 8.23.0-r0)"; echo "#5 2.2 OK: 12 MiB in 30 packages" ;;
  "compose up -d --no-build --no-deps --wait --wait-timeout "*)
    n=$(grep -c "compose up" "$STUB_LOG"); [ "${STUB_UP_FAIL_FIRST:-0}" = 1 ] && [ "$n" = 1 ] && { echo "unhealthy"; exit 1; }
    [ "${STUB_UP_FAIL_ALL:-0}" = 1 ] && exit 1; exit 0 ;;
  "image ls openvms-web --format {{.Tag}}") printf '%s\n' $STUB_TAGS ;;
esac
exit 0
STUB
chmod +x "$tmp/bin/docker"

run() { # run <name> [VAR=value...]; leaves $tmp/<name>.log (script log) and $tmp/<name>.calls
  local name=$1; shift
  : >"$tmp/$name.calls"
  # Later assignments win in env(1): the defaults come first so a scenario can override them.
  env PATH="$tmp/bin:$PATH" STUB_LOG="$tmp/$name.calls" STUB_BUILD_INPUT="$tmp/$name.build" \
    APK_REFRESH_LOG="$tmp/$name.log" APK_REFRESH_ROOT="$tmp" APK_REFRESH_LOCK="$tmp/$name.lock" \
    APK_REFRESH_DEPLOY_PATTERN='no-such-[d]eploy-process' "$@" bash "$here/web-apk-refresh.sh" >"$tmp/$name.out" 2>&1
  echo $? >"$tmp/$name.rc"
}
rc() { cat "$tmp/$1.rc"; }
has() { grep -q -- "$2" "$tmp/$1.$3" || fail "$1: expected '$2' in $3"; }
hasnt() { ! grep -q -- "$2" "$tmp/$1.$3" || fail "$1: unexpected '$2' in $3"; }

# 1. No web container: nothing to do.
run nowweb STUB_NO_WEB=1
[ "$(rc nowweb)" = 0 ] || fail "nowweb: exit $(rc nowweb)"
has nowweb "web container is not running" log
hasnt nowweb "compose up" calls

# 2. Image without apk: nothing to do.
run noapk STUB_NO_APK=1
[ "$(rc noapk)" = 0 ] || fail "noapk: exit $(rc noapk)"
has noapk "has no apk" log

# 3. Already current: no swap, no container recreate, the temp image is removed.
run current STUB_UPDATE=0
[ "$(rc current)" = 0 ] || fail "current: exit $(rc current)"
has current "no package updates" log
hasnt current "compose up" calls
has current "docker rmi openvms-web:apk-" calls
has current "RUN apk upgrade --no-cache" build
has current "FROM openvms-web:apk-base-" build
hasnt current "FROM sha256" build

# 4. Update available: logged, rollback tag, swap, container recreated.
run update STUB_UPDATE=1
[ "$(rc update)" = 0 ] || fail "update: exit $(rc update)"
has update "packages updated: 1" log
has update "curl: 8.22.0-r0 -> 8.23.0-r0" log
has update "docker tag sha256:old openvms-web:rollback-" calls
has update "docker tag openvms-web:apk-" calls
has update "compose up -d --no-build --no-deps --wait --wait-timeout 120 web" calls
has update "== done: 1 packages updated" log
grep -q -E 'rollback-[0-9TZ]+-apk' "$tmp/update.calls" || fail "update: rollback tag must end with -apk"

# 5. Dry run: reports the change and keeps the container.
run dry STUB_UPDATE=1 APK_REFRESH_DRY_RUN=1
[ "$(rc dry)" = 0 ] || fail "dry: exit $(rc dry)"
has dry "packages updated: 1" log
has dry "dry-run: the running container is untouched" log
hasnt dry "compose up" calls
hasnt dry "rollback-" calls
hasnt dry "openvms-web:latest" calls
has dry "docker rmi openvms-web:apk-base-" calls

# 6. Refreshed container never becomes ready: the previous image is restored, exit 1.
run rollback STUB_UPDATE=1 STUB_UP_FAIL_FIRST=1
[ "$(rc rollback)" = 1 ] || fail "rollback: exit $(rc rollback), want 1"
has rollback "did not become ready; restoring" log
has rollback "refresh rolled back" log
[ "$(grep -c 'compose up' "$tmp/rollback.calls")" = 2 ] || fail "rollback: expected two compose up calls"

# 7. Rollback also fails: say how to restore by hand.
run broken STUB_UPDATE=1 STUB_UP_FAIL_ALL=1
[ "$(rc broken)" = 1 ] || fail "broken: exit $(rc broken), want 1"
has broken "restore by hand with: make rollback TAG=rollback-" log

# 8. Build failure: the running container is untouched, exit 1.
run buildfail STUB_UPDATE=1 STUB_BUILD_FAIL=1
[ "$(rc buildfail)" = 1 ] || fail "buildfail: exit $(rc buildfail), want 1"
has buildfail "failed to build (the running container is untouched)" log
hasnt buildfail "compose up" calls

# 9. Retention keeps the newest APK_REFRESH_KEEP apk rollback tags only.
run keep STUB_UPDATE=1 APK_REFRESH_KEEP=2 \
  STUB_TAGS="rollback-20260101T000000Z-apk rollback-20260201T000000Z-apk rollback-20260301T000000Z-apk rollback-20260401T000000Z-apk rollback-20260315T000000Z latest"
has keep "docker rmi openvms-web:rollback-20260101T000000Z-apk" calls
has keep "docker rmi openvms-web:rollback-20260201T000000Z-apk" calls
hasnt keep "docker rmi openvms-web:rollback-20260401T000000Z-apk" calls
hasnt keep "docker rmi openvms-web:rollback-20260315T000000Z" calls

# 10. A running deploy defers the refresh.
sleep 30 &
sp=$!
run deploying APK_REFRESH_DEPLOY_PATTERN="sleep 30" STUB_UPDATE=1
kill "$sp" 2>/dev/null
[ "$(rc deploying)" = 0 ] || fail "deploying: exit $(rc deploying)"
has deploying "a deploy is running; skipped" log
hasnt deploying "compose up" calls

# 11. Installer: files land under the prefix, the unit points at the project root, uninstall removes them.
root="$tmp/proj"; mkdir -p "$root"; : >"$root/docker-compose.yml"
env INSTALL_PREFIX="$tmp/pfx" SKIP_SYSTEMCTL=1 APK_REFRESH_ROOT="$root" bash "$here/install-web-apk-refresh.sh" >"$tmp/install.out" 2>&1 ||
  fail "install: $(cat "$tmp/install.out")"
[ -x "$tmp/pfx/usr/local/sbin/openvms-web-apk-refresh" ] || fail "install: script missing"
grep -q "WorkingDirectory=$root" "$tmp/pfx/etc/systemd/system/openvms-web-apk-refresh.service" || fail "install: unit root not substituted"
grep -q '@ROOT@' "$tmp/pfx/etc/systemd/system/openvms-web-apk-refresh.service" && fail "install: placeholder left"
grep -q 'OnCalendar=Sun \*-\*-\* 23:59:00' "$tmp/pfx/etc/systemd/system/openvms-web-apk-refresh.timer" || fail "install: timer schedule"
[ -f "$tmp/pfx/etc/logrotate.d/openvms-web-apk-refresh" ] || fail "install: logrotate rule missing"
env INSTALL_PREFIX="$tmp/pfx" SKIP_SYSTEMCTL=1 bash "$here/install-web-apk-refresh.sh" --uninstall >/dev/null 2>&1
[ ! -e "$tmp/pfx/usr/local/sbin/openvms-web-apk-refresh" ] || fail "uninstall: script left"

if [ "$fails" -eq 0 ]; then echo "all web apk refresh tests passed"; else echo "$fails failure(s)"; exit 1; fi
