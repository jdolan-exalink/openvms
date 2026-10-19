#!/bin/bash
# Tests for deploy/deploy.sh and deploy/rollback.sh using stubbed docker/curl.
# Usage: bash deploy/deploy_test.sh
set -u
here=$(cd "$(dirname "$0")" && pwd)
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
fails=0
fail() { echo "FAIL: $1"; fails=$((fails + 1)); }

# Stubs: docker logs every call; curl reports a ready API.
mkdir "$tmp/bin"
cat >"$tmp/bin/docker" <<'STUB'
#!/bin/sh
echo "docker $*" >>"$STUB_LOG"
case "$*" in
  "compose ps"*) echo abc ;;
  "compose port web 443") echo "${STUB_PORT:-0.0.0.0:9443}" ;;
  *"psql"*) echo 5 ;;
  *"pg_dump"*) echo DUMP ;;
  "image inspect openvms-"*)
    for t in $STUB_TAGS; do [ "$t" = "${3#openvms-}" ] && exit 0; done
    exit 1 ;;
  "tag "*) [ -z "${STUB_TAG_FAIL:-}" ] ;;
esac
STUB
cat >"$tmp/bin/curl" <<'STUB'
#!/bin/sh
echo "curl $*" >>"$STUB_LOG"
echo '{"status":"ok"}'
STUB
chmod +x "$tmp/bin/docker" "$tmp/bin/curl"
export STUB_LOG="$tmp/log"

mkrepo() {
  git init -q -b main "$1"
  git -C "$1" config user.email t@example.invalid
  git -C "$1" config user.name t
  echo a >"$1/f"
  git -C "$1" add f
  git -C "$1" commit -q -m init
}

dry() { DEPLOY_DRY_RUN=1 DEPLOY_SKIP_PULL=1 DEPLOY_ROOT="$1" bash "$here/deploy.sh" 2>&1; }

# 1. Dry run on a clean tree: all steps in order.
mkrepo "$tmp/ok"
out=$(dry "$tmp/ok"); rc=$?
[ "$rc" -eq 0 ] || fail "clean tree: expected exit 0, got $rc: $out"
prev=0
for step in "[1/8] preflight" "[2/8] metadata" "[3/8] backup" "[4/8] rollback tags" \
  "[5/8] build" "[6/8] up" "[7/8] health" "[8/8] summary"; do
  line=$(printf '%s\n' "$out" | grep -nF -- "$step" | head -n1 | cut -d: -f1)
  if [ -z "$line" ]; then fail "missing step: $step"; continue; fi
  [ "$line" -gt "$prev" ] || fail "step out of order: $step"
  prev=$line
done
printf '%s\n' "$out" | grep -q 'docker compose build --pull=false' || fail "build command missing"
printf '%s\n' "$out" | grep -q 'docker compose up -d$' || fail "up command missing"
printf '%s\n' "$out" | grep -q 'make rollback TAG=' || fail "rollback_help must lead with make rollback"

# 2. Dirty tree aborts before any mutation.
mkrepo "$tmp/dirty"
echo b >>"$tmp/dirty/f"
out=$(dry "$tmp/dirty"); rc=$?
[ "$rc" -ne 0 ] || fail "dirty tree: expected non-zero exit"
printf '%s\n' "$out" | grep -qi 'working tree is not clean' || fail "dirty tree: missing message"
printf '%s\n' "$out" | grep -qF '[3/8] backup' && fail "dirty tree: continued past preflight"

# 3. Wrong branch aborts; DEPLOY_BRANCH overrides.
mkrepo "$tmp/branch"
git -C "$tmp/branch" checkout -q -b feature
out=$(dry "$tmp/branch"); rc=$?
[ "$rc" -ne 0 ] || fail "wrong branch: expected non-zero exit"
out=$(export DEPLOY_BRANCH=feature; dry "$tmp/branch"); rc=$?
[ "$rc" -eq 0 ] || fail "DEPLOY_BRANCH override: expected exit 0: $out"

# 4. Full (non-dry) run with stubs: port comes from compose, curl is bounded.
: >"$STUB_LOG"
out=$(PATH="$tmp/bin:$PATH" DEPLOY_SKIP_PULL=1 DEPLOY_SKIP_BACKUP=1 STUB_TAGS="" DEPLOY_ROOT="$tmp/ok" bash "$here/deploy.sh" 2>&1); rc=$?
[ "$rc" -eq 0 ] || fail "stubbed run: expected exit 0, got $rc: $out"
grep -q 'curl .*--max-time' "$STUB_LOG" || fail "curl without --max-time"
grep -q 'curl .*--connect-timeout' "$STUB_LOG" || fail "curl without --connect-timeout"
grep -q 'https://localhost:9443/health/ready' "$STUB_LOG" || fail "port not taken from 'docker compose port'"

# 5. Pull that changes HEAD re-executes the script exactly once.
mkrepo "$tmp/up"
git clone -q "$tmp/up" "$tmp/clone"
git -C "$tmp/clone" config user.email t@example.invalid
git -C "$tmp/clone" config user.name t
echo c >"$tmp/up/g"; git -C "$tmp/up" add g; git -C "$tmp/up" commit -q -m two
: >"$STUB_LOG"
out=$(PATH="$tmp/bin:$PATH" DEPLOY_SKIP_BACKUP=1 STUB_TAGS="" DEPLOY_ROOT="$tmp/clone" bash "$here/deploy.sh" 2>&1); rc=$?
[ "$rc" -eq 0 ] || fail "re-exec run: expected exit 0, got $rc: $out"
printf '%s\n' "$out" | grep -q 're-executing' || fail "no re-exec notice after HEAD changed"
[ "$(printf '%s\n' "$out" | grep -cF '[8/8] summary')" -eq 1 ] || fail "deploy ran more than once"
out=$(PATH="$tmp/bin:$PATH" DEPLOY_SKIP_BACKUP=1 STUB_TAGS="" DEPLOY_ROOT="$tmp/clone" bash "$here/deploy.sh" 2>&1)
printf '%s\n' "$out" | grep -q 're-executing' && fail "re-exec when HEAD unchanged"

# 6. Rollback: fails without touching the stack unless something was retagged.
rb() { PATH="$tmp/bin:$PATH" bash "$here/rollback.sh" "$@" 2>&1; }
: >"$STUB_LOG"
out=$(STUB_TAGS="" rb T1); rc=$?
[ "$rc" -ne 0 ] || fail "rollback with no tags: expected failure"
grep -q 'compose up' "$STUB_LOG" && fail "rollback with no tags ran compose up"
: >"$STUB_LOG"
out=$(STUB_TAGS="api:rollback-T1 web:rollback-T1" STUB_TAG_FAIL=1 rb T1); rc=$?
[ "$rc" -ne 0 ] || fail "rollback with failing docker tag: expected failure"
grep -q 'compose up' "$STUB_LOG" && fail "rollback with failing tag ran compose up"
: >"$STUB_LOG"
out=$(STUB_TAGS="api:rollback-T1 web:rollback-T1" rb T1); rc=$?
[ "$rc" -eq 0 ] || fail "partial rollback: expected success: $out"
printf '%s\n' "$out" | grep -q 'retagged: api web' || fail "retagged list missing: $out"
grep -q 'compose up -d --no-build' "$STUB_LOG" || fail "rollback did not run compose up --no-build"
out=$(rb); [ $? -ne 0 ] || fail "rollback without TAG: expected failure"

[ "$fails" -eq 0 ] && echo "ok: deploy and rollback tests passed"
[ "$fails" -eq 0 ]
