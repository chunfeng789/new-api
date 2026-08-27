#!/usr/bin/env bash
# Generate a grouped, conventional-commit style changelog for a release tag.
#
# Usage:
#   scripts/gen-changelog.sh <tag> [previous-tag]
#
# When previous-tag is omitted it is resolved as the nearest tag before <tag>.
# The changelog (Markdown) is printed to stdout so callers can redirect it into
# a release body. Repo is taken from $GITHUB_REPOSITORY, or the origin remote.
set -euo pipefail

TAG="${1:-}"
if [ -z "$TAG" ]; then
  echo "usage: $0 <tag> [previous-tag]" >&2
  exit 1
fi

PREV_TAG="${2:-}"
if [ -z "$PREV_TAG" ]; then
  PREV_TAG="$(git describe --tags --abbrev=0 "${TAG}^" 2>/dev/null || true)"
fi

if [ -n "$PREV_TAG" ]; then
  RANGE="${PREV_TAG}..${TAG}"
else
  RANGE="$TAG"
fi

if [ -n "$PREV_TAG" ] && [ "$(git rev-list --count "$RANGE" 2>/dev/null || echo 0)" = "0" ]; then
  echo "_No changes since ${PREV_TAG}._"
  exit 0
fi

# Print a section for commits whose subject starts with the given type(s).
emit_section() {
  local title="$1" type_re="$2" matched
  matched="$(git log --no-merges --pretty=format:'%s|%h' "$RANGE" \
    | grep -iE "^(${type_re})(\([^)]*\))?!?:" || true)"
  [ -z "$matched" ] && return 0
  printf '### %s\n\n' "$title"
  while IFS='|' read -r subject hash; do
    [ -z "$subject" ] && continue
    printf -- '- %s (%s)\n' "$subject" "$hash"
  done <<< "$matched"
  printf '\n'
}

emit_section "✨ Features"       "feat"
emit_section "🐛 Bug Fixes"      "fix"
emit_section "⚡ Performance"    "perf"
emit_section "♻️ Refactoring"    "refactor"
emit_section "📝 Documentation"  "docs"
emit_section "👷 Build & CI"     "build|ci"
emit_section "📦 Chores"         "chore"

# Anything that is not one of the grouped conventional types.
others="$(git log --no-merges --pretty=format:'%s|%h' "$RANGE" \
  | grep -ivE '^(feat|fix|perf|refactor|docs|build|ci|chore)(\([^)]*\))?!?:' || true)"
if [ -n "$others" ]; then
  printf '### 🔧 Other Changes\n\n'
  while IFS='|' read -r subject hash; do
    [ -z "$subject" ] && continue
    printf -- '- %s (%s)\n' "$subject" "$hash"
  done <<< "$others"
  printf '\n'
fi

if [ -n "$PREV_TAG" ]; then
  repo="${GITHUB_REPOSITORY:-}"
  if [ -z "$repo" ]; then
    url="$(git config --get remote.origin.url || true)"
    repo="$(printf '%s' "$url" | sed -E 's#(git@github.com:|https://github.com/)##; s#\.git$##')"
  fi
  if [ -n "$repo" ]; then
    printf '**Full Changelog**: https://github.com/%s/compare/%s...%s\n' "$repo" "$PREV_TAG" "$TAG"
  fi
fi
