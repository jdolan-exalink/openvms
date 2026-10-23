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
  "compose ps -a --status created"*) [ -n "${STUB_ORPHANS:-}" ] && printf '%s\n' "$STUB_ORPHANS"; exit 0 ;;
  "compose up -d --no-build --no-deps --wait "*) [ "${STUB_UP_FAIL_ON:-}" = "${7:-}" ] && exit 1; exit 0 ;;
  "compose ps"*) echo abc ;;
  "compose port web 443") echo "${STUB_PORT:-0.0.0.0:9443}" ;;
  "compose exec -T web printenv HTTPS_SITE_ADDRESS") [ -n "${STUB_SITE:-}" ] && echo "$STUB_SITE" ;;
  *"psql"*) echo 5 ;;
  *"pg_dump"*) echo DUMP ;;
  "image inspect --format"*)
    ref=$5
    for t in $STUB_TAGS; do [ "$t" = "${ref#openvms-}" ] && { echo "id-$ref"; exit 0; }; done
    exit 1 ;;
  "image inspect openvms-"*)
    for t in $STUB_TAGS; do [ "$t" = "${3#openvms-}" ] && exit 0; done
    exit 1 ;;
  "image ls "*)
    [ -n "${STUB_LS_TAGS:-}" ] && printf '%s\n' $STUB_LS_TAGS
    [ -f "$STUB_TAGGED" ] && awk -v s="$3" '$1 == s {print $2}' "$STUB_TAGGED"
    exit 0 ;;
  "tag "*)
    [ -z "${STUB_TAG_FAIL:-}" ] || exit 1
    [ -n "${STUB_TAG_FAIL_ON:-}" ] && [ "$2" = "openvms-$STUB_TAG_FAIL_ON:rollback-${STUB_TAG_FAIL_TAG:-T1}" ] && exit 1
    case $3 in openvms-*:rollback-*) echo "${3%%:*} ${3#*:}" >>"$STUB_TAGGED" ;; esac
    exit 0 ;;
esac
STUB
cat >"$tmp/bin/curl" <<'STUB'
#!/bin/sh
echo "curl $*" >>"$STUB_LOG"
echo '{"status":"ok"}'
STUB
chmod +x "$tmp/bin/docker" "$tmp/bin/curl"
export STUB_LOG="$tmp/log"
export STUB_TAGGED="$tmp/tagged"

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

# 1b. Step 6 recreates the app services one at a time (Compose 2.26 conflicts on
# container names when api and relay are recreated together): start what is missing
# without recreating, recreate api, relay, worker and web in order, then converge.
up_section=$(printf '%s\n' "$out" | sed -n '/\[6\/8\]/,/\[7\/8\]/p')
printf '%s\n' "$up_section" | grep -qF 'docker compose up -d --no-recreate' || fail "step 6: missing 'up -d --no-recreate'"
prev=0
for svc in api relay worker web; do
  line=$(printf '%s\n' "$up_section" | grep -nF -- "docker compose up -d --no-build --no-deps --wait $svc" | head -n1 | cut -d: -f1)
  if [ -z "$line" ]; then fail "step 6: missing sequential recreate of $svc"; continue; fi
  [ "$line" -gt "$prev" ] || fail "step 6: $svc recreated out of order"
  prev=$line
done
last_up=$(printf '%s\n' "$up_section" | grep -n 'docker compose up -d$' | tail -n1 | cut -d: -f1)
[ -n "$last_up" ] && [ "$last_up" -gt "$prev" ] || fail "step 6: final converging 'up -d' must follow the per-service recreates"

# 1c. Stale temporary containers left by an interrupted recreate are removed; nothing else is.
: >"$STUB_LOG"
out=$(PATH="$tmp/bin:$PATH" DEPLOY_SKIP_PULL=1 DEPLOY_SKIP_BACKUP=1 STUB_TAGS="" DEPLOY_ROOT="$tmp/ok" \
  STUB_ORPHANS="a4a9960c12ad b1343138b1b4_openvms-relay-1
deadbeef0001 openvms-tls-bootstrap-1" bash "$here/deploy.sh" 2>&1); rc=$?
[ "$rc" -eq 0 ] || fail "orphan cleanup run: expected exit 0: $out"
grep -qF 'docker rm a4a9960c12ad' "$STUB_LOG" || fail "stale temp container not removed: $(cat "$STUB_LOG")"
grep -qF 'docker rm deadbeef0001' "$STUB_LOG" && fail "a regular created container was removed"
rm_line=$(grep -n 'docker rm a4a9960c12ad' "$STUB_LOG" | head -n1 | cut -d: -f1)
up_line=$(grep -n 'compose up -d' "$STUB_LOG" | head -n1 | cut -d: -f1)
[ -n "$rm_line" ] && [ -n "$up_line" ] && [ "$rm_line" -lt "$up_line" ] || fail "orphans must be removed before the first compose up"

# 1d. A failing per-service recreate aborts: later services are untouched and the rollback hint is shown.
: >"$STUB_LOG"
out=$(PATH="$tmp/bin:$PATH" DEPLOY_SKIP_PULL=1 DEPLOY_SKIP_BACKUP=1 STUB_TAGS="api:latest" STUB_UP_FAIL_ON=relay DEPLOY_ROOT="$tmp/ok" bash "$here/deploy.sh" 2>&1); rc=$?
[ "$rc" -ne 0 ] || fail "recreate failure: expected non-zero exit"
grep -qF -- '--wait api' "$STUB_LOG" || fail "recreate failure: api should have been recreated first"
grep -qF -- '--wait worker' "$STUB_LOG" && fail "recreate failure: worker recreated after relay failed"
grep -qF -- '--wait web' "$STUB_LOG" && fail "recreate failure: web recreated after relay failed"
printf '%s\n' "$out" | grep -q 'make rollback TAG=' || fail "recreate failure: missing rollback hint"
printf '%s\n' "$out" | grep -q 'relay' || fail "recreate failure: message should name the failing service"

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

# 4b. A configured HTTPS site address is probed by name (its certificate only
# covers that address) while the connection still targets the local stack.
: >"$STUB_LOG"
out=$(PATH="$tmp/bin:$PATH" DEPLOY_SKIP_PULL=1 DEPLOY_SKIP_BACKUP=1 STUB_TAGS="" STUB_SITE=10.1.1.24 DEPLOY_ROOT="$tmp/ok" bash "$here/deploy.sh" 2>&1); rc=$?
[ "$rc" -eq 0 ] || fail "site address run: expected exit 0, got $rc: $out"
grep -qF 'https://10.1.1.24:9443/health/ready' "$STUB_LOG" || fail "health check ignores HTTPS_SITE_ADDRESS: $(grep curl "$STUB_LOG")"
grep -qF -- '--connect-to 10.1.1.24:9443:127.0.0.1:9443' "$STUB_LOG" || fail "health check does not pin the connection to the local stack"
: >"$STUB_LOG"
out=$(PATH="$tmp/bin:$PATH" DEPLOY_SKIP_PULL=1 DEPLOY_SKIP_BACKUP=1 STUB_TAGS="" STUB_SITE='bad host;x' DEPLOY_ROOT="$tmp/ok" bash "$here/deploy.sh" 2>&1)
grep -qF 'https://localhost:9443/health/ready' "$STUB_LOG" || fail "invalid site address must fall back to localhost"

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

# 7. Atomic rollback: every tag is validated before the first retag, and a
# mid-way retag failure restores the previous latest of the services already done.
: >"$STUB_LOG"
out=$(STUB_TAGS="api:latest relay:latest web:latest api:rollback-T1 relay:rollback-T1 web:rollback-T1" STUB_TAG_FAIL_ON=web rb T1); rc=$?
[ "$rc" -ne 0 ] || fail "mid-way retag failure: expected failure"
grep -q 'compose up' "$STUB_LOG" && fail "mid-way retag failure ran compose up"
grep -qF 'docker tag id-openvms-api:latest openvms-api:latest' "$STUB_LOG" || fail "api latest not restored: $(cat "$STUB_LOG")"
grep -qF 'docker tag id-openvms-relay:latest openvms-relay:latest' "$STUB_LOG" || fail "relay latest not restored"
grep -qF 'docker tag id-openvms-web:latest' "$STUB_LOG" && fail "web was restored but never retagged"
first_tag=$(grep -n '^docker tag' "$STUB_LOG" | head -n1 | cut -d: -f1)
last_inspect=$(grep -n 'image inspect.*rollback-T1' "$STUB_LOG" | tail -n1 | cut -d: -f1)
[ -n "$first_tag" ] && [ -n "$last_inspect" ] && [ "$last_inspect" -lt "$first_tag" ] || fail "tags must all be validated before the first retag"

# 8. First deploy (nothing tagged): no 'make rollback' hint, a clear note instead.
: >"$STUB_LOG"
out=$(PATH="$tmp/bin:$PATH" DEPLOY_SKIP_PULL=1 DEPLOY_SKIP_BACKUP=1 STUB_TAGS="" DEPLOY_ROOT="$tmp/ok" bash "$here/deploy.sh" 2>&1); rc=$?
[ "$rc" -eq 0 ] || fail "first deploy: expected exit 0: $out"
printf '%s\n' "$out" | grep -q 'make rollback TAG=' && fail "first deploy printed a rollback hint"
printf '%s\n' "$out" | grep -qi 'no rollback available' || fail "first deploy: missing 'no rollback available' note"
: >"$STUB_LOG"
out=$(PATH="$tmp/bin:$PATH" DEPLOY_SKIP_PULL=1 DEPLOY_SKIP_BACKUP=1 STUB_TAGS="api:latest" DEPLOY_ROOT="$tmp/ok" bash "$here/deploy.sh" 2>&1)
printf '%s\n' "$out" | grep -q 'make rollback TAG=' || fail "tagged deploy: rollback hint missing"

# 9. Retention: keep the newest DEPLOY_KEEP tags and dump dirs, never the new ones.
mkrepo "$tmp/keep"
for d in 20250101T000000Z 20250102T000000Z 20250103T000000Z 99990101T000000Z; do mkdir -p "$tmp/keep/.atl/deploy/$d"; done
: >"$STUB_LOG"; rm -f "$tmp/tagged"
out=$(PATH="$tmp/bin:$PATH" DEPLOY_SKIP_PULL=1 DEPLOY_SKIP_BACKUP=1 DEPLOY_KEEP=1 STUB_TAGS="api:latest" \
  STUB_LS_TAGS="latest rollback-20250101T000000Z rollback-20250103T000000Z rollback-99990101T000000Z" DEPLOY_ROOT="$tmp/keep" bash "$here/deploy.sh" 2>&1); rc=$?
[ "$rc" -eq 0 ] || fail "retention run: expected exit 0: $out"
grep -qF 'docker rmi openvms-api:rollback-20250101T000000Z' "$STUB_LOG" || fail "old tag 20250101 not removed"
grep -qF 'docker rmi openvms-api:rollback-20250103T000000Z' "$STUB_LOG" || fail "old tag 20250103 not removed"
grep -qF 'rmi openvms-api:rollback-99990101T000000Z' "$STUB_LOG" && fail "newest tag removed"
grep -qF 'rmi openvms-api:latest' "$STUB_LOG" && fail "non-rollback tag removed"
ts_tag=$(awk '$1 == "openvms-api" {print $2}' "$tmp/tagged")
[ -n "$ts_tag" ] && grep -qF "rmi openvms-api:$ts_tag" "$STUB_LOG" && fail "tag just created was removed"
[ ! -d "$tmp/keep/.atl/deploy/20250101T000000Z" ] || fail "old dump dir kept"
[ ! -d "$tmp/keep/.atl/deploy/20250103T000000Z" ] || fail "old dump dir 20250103 kept"
[ -d "$tmp/keep/.atl/deploy/99990101T000000Z" ] || fail "newest dump dir removed"

# 10. Retention in dry-run only reports.
mkrepo "$tmp/keepdry"
mkdir -p "$tmp/keepdry/.atl/deploy/20250101T000000Z" "$tmp/keepdry/.atl/deploy/20250102T000000Z"
out=$(PATH="$tmp/bin:$PATH" DEPLOY_DRY_RUN=1 DEPLOY_SKIP_PULL=1 DEPLOY_KEEP=1 DEPLOY_ROOT="$tmp/keepdry" bash "$here/deploy.sh" 2>&1)
printf '%s\n' "$out" | grep -q 'would remove.*20250101T000000Z' || fail "dry run: missing would-remove for the old dump: $out"
[ -d "$tmp/keepdry/.atl/deploy/20250101T000000Z" ] || fail "dry run removed a dump dir"

[ "$fails" -eq 0 ] && echo "ok: deploy and rollback tests passed"
[ "$fails" -eq 0 ]
