#!/bin/bash
# Tests for deploy/install.sh using stubbed docker/docker-compose/curl/openssl.
# Usage: bash deploy/install_test.sh
set -u
here=$(cd "$(dirname "$0")" && pwd)
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
fails=0
fail() { echo "FAIL: $1"; fails=$((fails + 1)); }

export STUB_LOG="$tmp/log"
mkdir "$tmp/bin"
# docker: logs every call. STUB_NO_V2=1 makes `docker compose version` fail,
# STUB_DAEMON_DOWN=1 makes `docker info` fail.
cat >"$tmp/bin/docker" <<'STUB'
#!/bin/sh
echo "docker $*" >>"$STUB_LOG"
case "$*" in
  "info") [ -z "${STUB_DAEMON_DOWN:-}" ] || exit 1 ;;
  "compose version") [ -z "${STUB_NO_V2:-}" ] || exit 1; echo "${STUB_COMPOSE_VERSION:-Docker Compose version v2.29.1}" ;;
  "compose port web 443") echo "0.0.0.0:9443" ;;
  "compose port web 80") echo "0.0.0.0:9080" ;;
esac
exit 0
STUB
cat >"$tmp/bin/docker-compose" <<'STUB'
#!/bin/sh
echo "docker-compose $*" >>"$STUB_LOG"
echo "docker-compose version 1.29.2, build 5becea4c"
STUB
cat >"$tmp/bin/curl" <<'STUB'
#!/bin/sh
echo "curl $*" >>"$STUB_LOG"
[ -z "${STUB_CURL_FAIL:-}" ] || exit 22
echo '{"status":"ok"}'
STUB
cat >"$tmp/bin/openssl" <<'STUB'
#!/bin/sh
echo "openssl $*" >>"$STUB_LOG"
echo "c3R1Yi1tYXN0ZXItc2VjcmV0LXZhbHVlLTEyMzQ1Njc4OTA="
STUB
chmod +x "$tmp"/bin/*

# A PATH with the basic tools but no docker, for the "docker missing" case.
mkdir "$tmp/nodocker"
for t in bash sh dirname basename cp mv rm mkdir chmod grep awk head tr base64 date git cat sleep id tail cut stat; do
  p=$(command -v "$t" 2>/dev/null) && ln -s "$p" "$tmp/nodocker/$t"
done

# Fixture repo root with its own .env.example (never the real one).
mkroot() {
  mkdir -p "$1"
  cat >"$1/.env.example" <<'ENV'
# fixture
FOO=bar
OPENVMS_MASTER_KEY=change-me
ENV
}
run() { # run <root> [env...]; PATH carries the stubs
  local root=$1; shift
  env PATH="$tmp/bin:$PATH" INSTALL_ROOT="$root" INSTALL_ENV_EXAMPLE="$root/.env.example" INSTALL_WAIT=3 "$@" bash "$here/install.sh" 2>&1
}
lineof() { grep -nF -- "$1" "$STUB_LOG" | head -n1 | cut -d: -f1; }

# 1. v2 happy path.
mkroot "$tmp/ok"; : >"$STUB_LOG"
out=$(run "$tmp/ok"); rc=$?
[ "$rc" -eq 0 ] || fail "happy path: expected exit 0, got $rc: $out"
[ -f "$tmp/ok/.env" ] || fail "happy path: .env not created"
[ "$(stat -c %a "$tmp/ok/.env" 2>/dev/null)" = 600 ] || fail "happy path: .env mode is not 0600"
grep -q '^FOO=bar$' "$tmp/ok/.env" || fail "happy path: fixture content not preserved"
[ "$(grep -c '^OPENVMS_MASTER_KEY=' "$tmp/ok/.env")" -eq 1 ] || fail "happy path: master key line not unique"
grep -q '^OPENVMS_MASTER_KEY=c3R1Yi1tYXN0ZXItc2VjcmV0LXZhbHVlLTEyMzQ1Njc4OTA=$' "$tmp/ok/.env" || fail "happy path: master key not from openssl"
pw=$(grep '^POSTGRES_PASSWORD=' "$tmp/ok/.env" | cut -d= -f2-)
case $pw in ''|openvms|change-me) fail "happy path: POSTGRES_PASSWORD missing or default" ;; esac
printf '%s' "$pw" | grep -Eq '^[A-Za-z0-9]{16,}$' || fail "happy path: POSTGRES_PASSWORD not alphanumeric (16+)"
printf '%s\n' "$out" | grep -qF "$pw" && fail "happy path: postgres password printed"
printf '%s\n' "$out" | grep -qF 'c3R1Yi1tYXN0ZXI' && fail "happy path: master key printed"
a=$(lineof 'docker compose up -d --build'); b=$(lineof 'https://localhost:9443/health/ready')
[ -n "$a" ] && [ -n "$b" ] && [ "$a" -lt "$b" ] || fail "happy path: up must run before the health check"
printf '%s\n' "$out" | grep -qF 'docker compose logs api | grep "initial admin"' || fail "happy path: admin password hint missing"
printf '%s\n' "$out" | grep -qF "make deploy" || fail "happy path: make deploy mention missing"
printf '%s\n' "$out" | grep -qF "9080" || fail "happy path: http port missing from summary"

# 2. Existing .env is never touched; missing keys only produce a warning.
mkroot "$tmp/keep"; printf 'FOO=mine\n' >"$tmp/keep/.env"; chmod 640 "$tmp/keep/.env"; : >"$STUB_LOG"
out=$(run "$tmp/keep"); rc=$?
[ "$rc" -eq 0 ] || fail "existing .env: expected exit 0: $out"
[ "$(cat "$tmp/keep/.env")" = "FOO=mine" ] || fail "existing .env was modified"
[ "$(stat -c %a "$tmp/keep/.env")" = 640 ] || fail "existing .env mode changed"
printf '%s\n' "$out" | grep -q 'OPENVMS_MASTER_KEY' || fail "existing .env: no warning for missing master key"
grep -q '^openssl' "$STUB_LOG" && fail "existing .env: secrets generated anyway"

# 3. Compose v1 only: clear message, nothing runs.
mkroot "$tmp/v1"; : >"$STUB_LOG"
out=$(run "$tmp/v1" STUB_NO_V2=1); rc=$?
[ "$rc" -ne 0 ] || fail "compose v1: expected failure"
printf '%s\n' "$out" | grep -qi 'compose v1' || fail "compose v1: message missing: $out"
printf '%s\n' "$out" | grep -q 'docker-compose-v2' || fail "compose v1: install hint missing"
grep -q 'compose up' "$STUB_LOG" && fail "compose v1: ran compose up"
[ ! -e "$tmp/v1/.env" ] || fail "compose v1: created .env"

# 4. Missing docker fails early.
mkroot "$tmp/nod"
out=$(env PATH="$tmp/nodocker" INSTALL_ROOT="$tmp/nod" INSTALL_ENV_EXAMPLE="$tmp/nod/.env.example" bash "$here/install.sh" 2>&1); rc=$?
[ "$rc" -ne 0 ] || fail "no docker: expected failure"
printf '%s\n' "$out" | grep -qi 'docker is not installed' || fail "no docker: message missing: $out"
[ ! -e "$tmp/nod/.env" ] || fail "no docker: created .env"

# 5. Daemon down fails before touching .env.
mkroot "$tmp/down"
out=$(run "$tmp/down" STUB_DAEMON_DOWN=1); rc=$?
[ "$rc" -ne 0 ] || fail "daemon down: expected failure"
[ ! -e "$tmp/down/.env" ] || fail "daemon down: created .env"

# 6. Readiness timeout shows api logs and fails.
mkroot "$tmp/to"; : >"$STUB_LOG"
out=$(run "$tmp/to" STUB_CURL_FAIL=1); rc=$?
[ "$rc" -ne 0 ] || fail "timeout: expected failure"
grep -qF 'docker compose logs --tail 50 api' "$STUB_LOG" || fail "timeout: api logs not shown"

# 7. Existing bind-mounted postgres data warns that changing the password does not re-key it.
mkroot "$tmp/pgdata"; mkdir -p "$tmp/pgdata/custom"; printf data >"$tmp/pgdata/custom/PG_VERSION"
out=$(run "$tmp/pgdata" OPENVMS_POSTGRES_DATA_DIR="$tmp/pgdata/custom"); rc=$?
[ "$rc" -eq 0 ] || fail "postgres data dir: expected exit 0: $out"
printf '%s\n' "$out" | grep -qi 'postgres.*data.*password\|password.*postgres.*data' || fail "postgres data dir: warning missing"

# 8. Dry run mutates nothing.
mkroot "$tmp/dry"; : >"$STUB_LOG"
out=$(run "$tmp/dry" INSTALL_DRY_RUN=1); rc=$?
[ "$rc" -eq 0 ] || fail "dry run: expected exit 0: $out"
[ ! -e "$tmp/dry/.env" ] || fail "dry run created .env"
grep -q 'compose up' "$STUB_LOG" && fail "dry run ran compose up"
printf '%s\n' "$out" | grep -q 'docker compose up -d --build' || fail "dry run: up command not printed"

# 9. The Compose major version is read from the first "N.M" of the version line.
for v in "Docker Compose version 2.26.1-4" "Docker Compose version v2.20.0" "Docker Compose version v2.31.0-desktop.2" "Docker Compose version v2.1.0"; do
  mkroot "$tmp/ver"; rm -f "$tmp/ver/.env"
  out=$(run "$tmp/ver" STUB_COMPOSE_VERSION="$v"); rc=$?
  [ "$rc" -eq 0 ] || fail "compose version '$v': rejected a valid v2: $out"
done
mkroot "$tmp/old"; rm -f "$tmp/old/.env"
out=$(run "$tmp/old" STUB_COMPOSE_VERSION="docker-compose version 1.29.2, build 5becea4c"); rc=$?
[ "$rc" -ne 0 ] || fail "compose version 1.29.2: accepted as v2"

# 10. OPENVMS_ADMIN_PASSWORD from the environment lands in a new .env and is never printed;
# an existing .env is not changed and the operator is told so.
mkroot "$tmp/adm"; rm -f "$tmp/adm/.env"
out=$(run "$tmp/adm" OPENVMS_ADMIN_PASSWORD="Adm1n-Secret-2026"); rc=$?
[ "$rc" -eq 0 ] || fail "admin password: expected exit 0: $out"
grep -q '^OPENVMS_ADMIN_PASSWORD=Adm1n-Secret-2026$' "$tmp/adm/.env" || fail "admin password: not written to .env"
printf '%s\n' "$out" | grep -qF 'Adm1n-Secret-2026' && fail "admin password: printed"
mkroot "$tmp/adm2"; printf 'FOO=mine\n' >"$tmp/adm2/.env"
out=$(run "$tmp/adm2" OPENVMS_ADMIN_PASSWORD="Adm1n-Secret-2026"); rc=$?
[ "$(cat "$tmp/adm2/.env")" = "FOO=mine" ] || fail "admin password: existing .env modified"
printf '%s\n' "$out" | grep -q 'OPENVMS_ADMIN_PASSWORD' || fail "admin password: no notice for an existing .env"

# 11. A too short OPENVMS_ADMIN_PASSWORD (< 8 characters) fails up front: no .env, nothing started.
mkroot "$tmp/short"; rm -f "$tmp/short/.env"; : >"$STUB_LOG"
out=$(run "$tmp/short" OPENVMS_ADMIN_PASSWORD="abc1234"); rc=$?
[ "$rc" -ne 0 ] || fail "short admin password: expected failure"
printf '%s\n' "$out" | grep -q 'at least 8 characters' || fail "short admin password: message missing: $out"
printf '%s\n' "$out" | grep -qF 'abc1234' && fail "short admin password: printed"
[ ! -e "$tmp/short/.env" ] || fail "short admin password: created .env"
grep -q 'compose up' "$STUB_LOG" && fail "short admin password: ran compose up"
mkroot "$tmp/eight"; rm -f "$tmp/eight/.env"
out=$(run "$tmp/eight" OPENVMS_ADMIN_PASSWORD="abcd1234"); rc=$?
[ "$rc" -eq 0 ] || fail "8-character admin password: expected exit 0: $out"
grep -q '^OPENVMS_ADMIN_PASSWORD=abcd1234$' "$tmp/eight/.env" || fail "8-character admin password: not written to .env"
printf '%s\n' "$out" | grep -qF 'docker compose logs api | grep "initial admin"' || fail "summary: how to read the generated password missing"

[ "$fails" -eq 0 ] && echo "ok: install tests passed"
[ "$fails" -eq 0 ]
