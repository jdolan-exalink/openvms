#!/usr/bin/env bash
# Tests tools/agent_version.sh in throwaway git repositories. The agent's package directories are
# injected (AGENT_PACKAGE_DIRS) so no Go module is needed.
set -euo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)
script="$root/tools/agent_version.sh"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
fail() { echo "FAIL: $*" >&2; exit 1; }

new_repo() {
  local dir="$tmp/$1"
  mkdir -p "$dir/internal/agent" "$dir/internal/api" "$dir/apps/edge-agent" "$dir/deploy/agent"
  git -C "$dir" init -q -b main
  git -C "$dir" config user.email t@example.invalid
  git -C "$dir" config user.name t
  printf '0.4.0\n' >"$dir/deploy/agent/VERSION"
  printf 'package agent\n' >"$dir/internal/agent/a.go"
  printf 'package api\n' >"$dir/internal/api/b.go"
  printf '# Changelog\n\n## [Unreleased]\n\n### Added\n\n- something\n\n## [0.4.0] - 2026-10-19\n' >"$dir/CHANGELOG.md"
  git -C "$dir" add -A && git -C "$dir" commit -q -m base
  git -C "$dir" checkout -q -b work
  echo "$dir"
}
check() { (cd "$1" && AGENT_PACKAGE_DIRS="internal/agent apps/edge-agent" "$script" check main 2>&1); }

# 1. An agent change on a version that is already released needs a bump.
d=$(new_repo released)
git -C "$d" tag agent-v0.4.0
echo 'var X = 1' >>"$d/internal/agent/a.go"; git -C "$d" commit -qam change
out=$(check "$d") && fail "released version with an agent change must fail"
grep -q "already released" <<<"$out" || fail "message should say the version is released: $out"

# 2. The same change on a version nobody released yet rides along.
d=$(new_repo unreleased)
echo 'var X = 1' >>"$d/internal/agent/a.go"; git -C "$d" commit -qam change
check "$d" >/dev/null || fail "an unreleased version must accept agent changes"

# 3. A bump satisfies the check.
d=$(new_repo bumped)
git -C "$d" tag agent-v0.4.0
echo 'var X = 1' >>"$d/internal/agent/a.go"; printf '0.4.1\n' >"$d/deploy/agent/VERSION"; git -C "$d" commit -qam change
check "$d" >/dev/null || fail "a bumped version must pass"

# 4. A change outside the agent does not need one.
d=$(new_repo outside)
git -C "$d" tag agent-v0.4.0
echo 'var Y = 1' >>"$d/internal/api/b.go"; git -C "$d" commit -qam change
check "$d" >/dev/null || fail "a change outside the agent must pass"

# 5. Tests and docs of the agent do not need one.
d=$(new_repo tests)
git -C "$d" tag agent-v0.4.0
printf 'package agent\n' >"$d/internal/agent/a_test.go"; printf 'notes\n' >"$d/internal/agent/README.md"; git -C "$d" add -A; git -C "$d" commit -qm tests
check "$d" >/dev/null || fail "test and doc files must not require a bump"

# 6. The version must not go down.
d=$(new_repo lowered)
printf '0.3.9\n' >"$d/deploy/agent/VERSION"; git -C "$d" commit -qam lower
out=$(check "$d") && fail "a lowered version must fail"
grep -q "lower" <<<"$out" || fail "message should say the version went down: $out"

# 7. The installer bundle counts as the agent (everything under deploy/agent except VERSION).
d=$(new_repo bundle)
git -C "$d" tag agent-v0.4.0
echo '# x' >"$d/deploy/agent/install.sh"; git -C "$d" add -A; git -C "$d" commit -qm bundle
check "$d" >/dev/null && fail "an installer change on a released version must fail"

# 8. bump writes VERSION and a changelog line under Unreleased.
d=$(new_repo bump)
(cd "$d" && "$script" bump minor --note "adds a thing") >/dev/null
[[ $(cat "$d/deploy/agent/VERSION") == "0.5.0" ]] || fail "minor bump should give 0.5.0, got $(cat "$d/deploy/agent/VERSION")"
awk '/^## \[Unreleased\]/{u=1} /^## \[0.4.0\]/{u=0} u && /Agent 0.5.0: adds a thing/{found=1} END{exit !found}' "$d/CHANGELOG.md" || fail "changelog line missing: $(cat "$d/CHANGELOG.md")"
(cd "$d" && "$script" bump patch --note "fix") >/dev/null
[[ $(cat "$d/deploy/agent/VERSION") == "0.5.1" ]] || fail "patch bump should give 0.5.1"
(cd "$d" && "$script" bump major --note "break") >/dev/null
[[ $(cat "$d/deploy/agent/VERSION") == "1.0.0" ]] || fail "major bump should give 1.0.0"

# 9. check --fix bumps a patch by itself and says so, only when a bump is needed.
d=$(new_repo fix)
git -C "$d" tag agent-v0.4.0
echo 'var X = 1' >>"$d/internal/agent/a.go"; git -C "$d" commit -qam change
(cd "$d" && AGENT_PACKAGE_DIRS="internal/agent apps/edge-agent" "$script" check main --fix) >/dev/null || fail "--fix should repair the version"
[[ $(cat "$d/deploy/agent/VERSION") == "0.4.1" ]] || fail "--fix should bump to 0.4.1"
check "$d" >/dev/null || fail "the check must pass after --fix"
before=$(cat "$d/deploy/agent/VERSION")
(cd "$d" && AGENT_PACKAGE_DIRS="internal/agent apps/edge-agent" "$script" check main --fix) >/dev/null
[[ $(cat "$d/deploy/agent/VERSION") == "$before" ]] || fail "--fix must not bump twice"

# 10. An unknown base fails loudly instead of passing.
d=$(new_repo nobase)
(cd "$d" && AGENT_PACKAGE_DIRS="internal/agent" "$script" check no-such-ref >/dev/null 2>&1) && fail "an unknown base must fail"

echo "agent_version_test: ok"
