#!/usr/bin/env bash
set -euo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
mkdir -p "$tmp/bin"
touch "$tmp/.env"

cat >"$tmp/bin/docker" <<'STUB'
#!/usr/bin/env bash
printf '%s\n' "$*" >>"$DOCKER_LOG"
case "$*" in
  *"ps -q api") printf 'api-container-id\n' ;;
  *"build api") printf 'build complete\n' ;;
  *"config --images api") exit 0 ;;
  *"config --images") printf 'postgres:16\nopenvms-api\nopenvms-web\n' ;;
  "image inspect openvms-api") ;;
  *"NetworkSettings.Networks"*) printf 'default-network-id\n' ;;
  *".Config.Env"*) printf 'postgres://app:fixture-only@postgres:5432/openvms\n' ;;
  "run --rm -i --network default-network-id --env DATABASE_URL --entrypoint /vmsctl openvms-api reset-admin-password -user admin")
    [[ "$DATABASE_URL" == "postgres://app:fixture-only@postgres:5432/openvms" ]] || exit 89
    IFS= read -r password
    printf '%s\n' "$password" >>"$PASSWORD_STDIN_LOG"
    ;;
  *) exit 88 ;;
esac
STUB
chmod +x "$tmp/bin/docker"
export PATH="$tmp/bin:$PATH" DOCKER_LOG="$tmp/docker.log" PASSWORD_STDIN_LOG="$tmp/password-stdin.log"
export RESET_ADMIN_PROJECT="openvms-test" RESET_ADMIN_ENV_FILE="$tmp/.env"

fail() { printf 'FAIL: %s\n' "$1" >&2; exit 1; }

out=$({ sleep 0.2; printf 'Secret-Password-2026\nSecret-Password-2026\n'; } | script -qec "bash '$root/tools/reset_admin.sh'" /dev/null 2>&1) || fail "valid reset failed"
[[ "$out" != *"Secret-Password-2026"* ]] || fail "password appeared in terminal output"
grep -q -- '--project-name openvms-test' "$DOCKER_LOG" || fail "Compose project not explicitly scoped"
grep -Fq -- "--env-file $tmp/.env" "$DOCKER_LOG" || fail "Compose environment file was not explicitly loaded"
grep -q 'Secret-Password-2026' "$DOCKER_LOG" && fail "password appeared in process arguments"
grep -q '^Secret-Password-2026$' "$PASSWORD_STDIN_LOG" || fail "password was not passed on stdin to API maintenance command"
grep -q 'reset-admin-password' "$DOCKER_LOG" || fail "API maintenance reset command was not invoked"
grep -q 'build api' "$DOCKER_LOG" || fail "matching API CLI was not built from the checkout"
grep -q 'config --images$' "$DOCKER_LOG" || fail "unfiltered Compose image list was not resolved"
if grep -q 'config --images api$' "$DOCKER_LOG"; then fail "filtered Compose image lookup was used"; fi
grep -q 'run --rm -i --network default-network-id --env DATABASE_URL --entrypoint /vmsctl openvms-api reset-admin-password -user admin' "$DOCKER_LOG" || fail "interactive stdin was not enabled for the one-off API CLI"
grep -q 'ps -q api' "$DOCKER_LOG" || fail "running API environment was not selected"
grep 'inspect --format.*Config.Env' "$DOCKER_LOG" | grep -q 'DATABASE_URL=' || fail "database URL was not selected from the running API environment"
grep 'inspect --format.*Config.Env' "$DOCKER_LOG" | grep -Fq 'println .}}' && fail "container environment was not filtered to the database URL"
grep -q 'DATABASE_URL=postgres://app:fixture-only' "$DOCKER_LOG" && fail "database URL was logged in process arguments"
grep -Eq 'compose.*(down|volume rm)|volume rm' "$DOCKER_LOG" && fail "reset attempted destructive Compose data removal"
build_line=$(grep -n 'build api' "$DOCKER_LOG" | cut -d: -f1)
run_line=$(grep -n 'run --rm -i --network default-network-id' "$DOCKER_LOG" | cut -d: -f1)
[[ "$build_line" -lt "$run_line" ]] || fail "API image was not refreshed before reset"

run_count=$(grep -c 'run --rm -i --network default-network-id' "$DOCKER_LOG")
if { sleep 0.2; printf 'Secret-Password-2026\nDifferent-Password-2026\n'; } | script -qec "bash '$root/tools/reset_admin.sh'" /dev/null >/dev/null 2>&1; then
  fail "mismatched confirmation accepted"
fi
[[ "$(grep -c 'run --rm -i --network default-network-id' "$DOCKER_LOG")" == "$run_count" ]] || fail "mismatched confirmation reached the reset command"
printf 'reset_admin_test.sh: PASS\n'
