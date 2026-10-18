#!/bin/sh
# Dry-run tests for deploy/deploy.sh: step order and fail-fast behavior.
# Usage: sh deploy/deploy_test.sh
set -u
here=$(cd "$(dirname "$0")" && pwd)
script="$here/deploy.sh"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
fails=0

fail() { echo "FAIL: $1"; fails=$((fails + 1)); }

mkrepo() { # $1 = dir
  git init -q -b main "$1"
  git -C "$1" config user.email t@example.invalid
  git -C "$1" config user.name t
  echo a >"$1/f"
  git -C "$1" add f
  git -C "$1" commit -q -m init
}

run() { DEPLOY_DRY_RUN=1 DEPLOY_SKIP_PULL=1 DEPLOY_ROOT="$1" sh "$script" 2>&1; }

# 1. Clean tree on main: all steps run in order.
mkrepo "$tmp/ok"
out=$(run "$tmp/ok"); rc=$?
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

# 2. Dirty tracked file: abort before any mutation.
mkrepo "$tmp/dirty"
echo b >>"$tmp/dirty/f"
out=$(run "$tmp/dirty"); rc=$?
[ "$rc" -ne 0 ] || fail "dirty tree: expected non-zero exit"
printf '%s\n' "$out" | grep -qi 'working tree is not clean' || fail "dirty tree: missing message"
printf '%s\n' "$out" | grep -qF '[3/8] backup' && fail "dirty tree: continued past preflight"

# 3. Wrong branch aborts; DEPLOY_BRANCH overrides.
mkrepo "$tmp/branch"
git -C "$tmp/branch" checkout -q -b feature
out=$(run "$tmp/branch"); rc=$?
[ "$rc" -ne 0 ] || fail "wrong branch: expected non-zero exit"
out=$(export DEPLOY_BRANCH=feature; run "$tmp/branch"); rc=$?
[ "$rc" -eq 0 ] || fail "DEPLOY_BRANCH override: expected exit 0: $out"

[ "$fails" -eq 0 ] && echo "ok: deploy dry-run tests passed"
[ "$fails" -eq 0 ]
