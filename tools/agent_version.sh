#!/usr/bin/env bash
# Keeps the edge agent's version honest: a change to what ships in the agent needs a new
# deploy/agent/VERSION once the current one has been released.
#
#   agent_version.sh check [BASE] [--fix]   fail (or repair) when the agent changed without a bump
#   agent_version.sh bump patch|minor|major [--note TEXT]
#
# "The agent changed" means a non-test, non-doc file in a package the agent binary is built from
# (`go list -deps ./apps/edge-agent`) or anything under deploy/agent/ except VERSION. "Released"
# means the git tag agent-v<VERSION> exists; until then the version is still being built and its
# changes ride along. AGENT_PACKAGE_DIRS (space separated, repo-relative) replaces the go list, for tests.
set -euo pipefail

repo=$(git rev-parse --show-toplevel)
version_file="deploy/agent/VERSION"
changelog="CHANGELOG.md"

die() { echo "agent-version: $*" >&2; exit 1; }

package_dirs() {
  if [[ -n "${AGENT_PACKAGE_DIRS:-}" ]]; then
    tr ' ' '\n' <<<"$AGENT_PACKAGE_DIRS"
    return
  fi
  (cd "$repo" && go list -deps -f '{{if and (not .Standard) .Dir}}{{.Dir}}{{end}}' ./apps/edge-agent) |
    sed -n "s#^$repo/##p"
}

# agent_files lists the changed files that belong to the agent, one per line.
agent_files() {
  local base=$1 dirs
  dirs=$(package_dirs)
  { git -C "$repo" diff --name-only "$base" HEAD; git -C "$repo" diff --name-only HEAD; } | sort -u | while IFS= read -r f; do
    [[ -n "$f" ]] || continue
    case "$f" in
      "$version_file" | *_test.go | *.md | */testdata/*) continue ;;
      deploy/agent/*) echo "$f"; continue ;;
    esac
    dir=$(dirname "$f")
    if grep -qxF "$dir" <<<"$dirs"; then echo "$f"; fi
  done
}

next_version() {
  local level=$1 cur=$2 major minor patch
  IFS=. read -r major minor patch <<<"$cur"
  [[ "$major" =~ ^[0-9]+$ && "$minor" =~ ^[0-9]+$ && "$patch" =~ ^[0-9]+$ ]] || die "$cur is not MAJOR.MINOR.PATCH"
  case "$level" in
    major) echo "$((major + 1)).0.0" ;;
    minor) echo "$major.$((minor + 1)).0" ;;
    patch) echo "$major.$minor.$((patch + 1))" ;;
    *) die "level must be patch, minor or major" ;;
  esac
}

cmd_bump() {
  local level=${1:-} note="" cur new
  [[ -n "$level" ]] || die "usage: bump patch|minor|major [--note TEXT]"
  shift
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --note) note=${2:-}; shift 2 ;;
      *) die "unknown option $1" ;;
    esac
  done
  cur=$(tr -d '[:space:]' <"$repo/$version_file")
  new=$(next_version "$level" "$cur")
  printf '%s\n' "$new" >"$repo/$version_file"
  [[ -n "$note" ]] || note="agent changes (see the commits)"
  awk -v line="- Agent $new: $note" '
    /^## \[Unreleased\]/ { print; unreleased = 1; next }
    unreleased && /^## \[/ { if (!placed) { print "### Changed\n\n" line "\n"; placed = 1 } unreleased = 0 }
    unreleased && /^### Changed/ && !placed { print; getline; print; print line; placed = 1; next }
    { print }
    END { if (unreleased && !placed) print "\n### Changed\n\n" line }
  ' "$repo/$changelog" >"$repo/$changelog.tmp" && mv "$repo/$changelog.tmp" "$repo/$changelog"
  echo "agent-version: $cur -> $new"
}

cmd_check() {
  local base="" fix=0
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --fix) fix=1; shift ;;
      *) base=$1; shift ;;
    esac
  done
  [[ -n "$base" ]] || base=$(git -C "$repo" rev-parse --verify -q origin/main || echo main)
  git -C "$repo" rev-parse --verify -q "$base^{commit}" >/dev/null || die "unknown base ref $base"
  local mb cur old files
  mb=$(git -C "$repo" merge-base "$base" HEAD) || die "no merge base with $base"
  cur=$(tr -d '[:space:]' <"$repo/$version_file")
  old=$(git -C "$repo" show "$mb:$version_file" 2>/dev/null | tr -d '[:space:]' || true)

  if [[ -n "$old" && "$cur" != "$old" && "$(printf '%s\n%s\n' "$old" "$cur" | sort -V | head -1)" == "$cur" ]]; then
    die "deploy/agent/VERSION went from $old to $cur: the version is lower than on $base"
  fi
  files=$(agent_files "$mb")
  [[ -n "$files" ]] || { echo "agent-version: no agent change; $cur stays"; return 0; }

  if [[ "$cur" != "$old" ]]; then echo "agent-version: agent changed and the version moved $old -> $cur"; return 0; fi
  if ! git -C "$repo" rev-parse --verify -q "refs/tags/agent-v$cur" >/dev/null; then
    echo "agent-version: agent changed; $cur is not released yet, so it rides along"
    return 0
  fi
  if [[ $fix -eq 1 ]]; then
    cmd_bump patch --note "automatic bump: $(wc -l <<<"$files" | tr -d ' ') agent file(s) changed after $cur was released" >/dev/null
    echo "agent-version: $cur is already released and the agent changed; bumped to $(tr -d '[:space:]' <"$repo/$version_file")"
    return 0
  fi
  {
    echo "agent-version: the agent changed but $cur is already released (tag agent-v$cur):"
    sed 's/^/  /' <<<"$files"
    echo "Run: tools/agent_version.sh bump patch --note \"what changed\"   (or: make agent-bump LEVEL=patch NOTE=...)"
  } >&2
  return 1
}

case "${1:-}" in
  check) shift; cmd_check "$@" ;;
  bump) shift; cmd_bump "$@" ;;
  *) die "usage: agent_version.sh check [BASE] [--fix] | bump patch|minor|major [--note TEXT]" ;;
esac
