#!/usr/bin/env bash
# Automate a New API release: pick a version, tag it, and push the tag to
# trigger the GitHub Actions workflows that build the Docker image (to the
# private registry) and publish the GitHub Release with a changelog.
#
# Usage:
#   ./release.sh v1.0.1            # explicit version
#   ./release.sh patch             # bump patch from the latest vX.Y.Z tag
#   ./release.sh minor             # bump minor
#   ./release.sh major             # bump major
#   ./release.sh                   # interactive prompt
#
# Options:
#   -y, --yes    do not ask for confirmation
#   -h, --help   show this help and exit
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REMOTE="origin"
ASSUME_YES=0
VERSION_ARG=""

usage() { awk 'NR==1{next} /^#/{sub(/^# ?/,""); print; next} {exit}' "${BASH_SOURCE[0]}"; }

die() { echo "error: $*" >&2; exit 1; }

for arg in "$@"; do
  case "$arg" in
    -y|--yes) ASSUME_YES=1 ;;
    -h|--help) usage; exit 0 ;;
    -*) die "unknown option: $arg" ;;
    *) [ -n "$VERSION_ARG" ] && die "unexpected extra argument: $arg"; VERSION_ARG="$arg" ;;
  esac
done

git rev-parse --is-inside-work-tree >/dev/null 2>&1 || die "not inside a git repository"

# Working tree must be clean so the tag points at a well-defined commit.
if ! git diff --quiet || ! git diff --cached --quiet; then
  die "working tree is not clean; commit or stash changes first"
fi

CURRENT_BRANCH="$(git rev-parse --abbrev-ref HEAD)"
if [ "$CURRENT_BRANCH" != "main" ]; then
  echo "warning: current branch is '$CURRENT_BRANCH', not 'main'."
fi

echo "Fetching tags from $REMOTE ..."
git fetch --tags --quiet "$REMOTE"

VERSION_RE='^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.]+)?$'

latest_stable_tag() {
  git tag --list 'v*' | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$' | sort -V | tail -1
}

bump_version() {
  local part="$1" latest major minor patch
  latest="$(latest_stable_tag)"
  [ -z "$latest" ] && latest="v0.0.0"
  IFS='.' read -r major minor patch <<< "${latest#v}"
  case "$part" in
    major) major=$((major + 1)); minor=0; patch=0 ;;
    minor) minor=$((minor + 1)); patch=0 ;;
    patch) patch=$((patch + 1)) ;;
  esac
  printf 'v%s.%s.%s' "$major" "$minor" "$patch"
}

case "$VERSION_ARG" in
  major|minor|patch)
    NEW_VERSION="$(bump_version "$VERSION_ARG")"
    ;;
  "")
    LATEST_ANY="$(git describe --tags --abbrev=0 2>/dev/null || echo '(none)')"
    echo "Latest tag: $LATEST_ANY"
    read -r -p "New version (e.g. v1.0.1): " NEW_VERSION
    ;;
  *)
    NEW_VERSION="$VERSION_ARG"
    ;;
esac

[[ "$NEW_VERSION" =~ $VERSION_RE ]] || die "invalid version '$NEW_VERSION' (expected vX.Y.Z or vX.Y.Z-suffix)"

if git rev-parse "refs/tags/$NEW_VERSION" >/dev/null 2>&1; then
  die "tag $NEW_VERSION already exists locally"
fi
if git ls-remote --exit-code --tags "$REMOTE" "refs/tags/$NEW_VERSION" >/dev/null 2>&1; then
  die "tag $NEW_VERSION already exists on $REMOTE"
fi

PREV_TAG="$(git describe --tags --abbrev=0 2>/dev/null || true)"
echo
echo "==================== Changelog preview ===================="
bash "$SCRIPT_DIR/scripts/gen-changelog.sh" HEAD "$PREV_TAG" || true
echo "==========================================================="
echo
echo "About to tag $CURRENT_BRANCH @ $(git rev-parse --short HEAD) as $NEW_VERSION and push to $REMOTE."
case "$NEW_VERSION" in
  *-alpha*) echo "note: -alpha tag builds the Docker image only (no GitHub Release)." ;;
esac

if [ "$ASSUME_YES" -ne 1 ]; then
  read -r -p "Proceed? [y/N] " reply
  case "$reply" in
    y|Y|yes|YES) ;;
    *) echo "aborted."; exit 1 ;;
  esac
fi

git tag -a "$NEW_VERSION" -m "Release $NEW_VERSION"
git push "$REMOTE" "$NEW_VERSION"

REPO_URL="$(git config --get "remote.${REMOTE}.url" || true)"
REPO_SLUG="$(printf '%s' "$REPO_URL" | sed -E 's#(git@github.com:|https://github.com/)##; s#\.git$##')"
echo
echo "Pushed tag $NEW_VERSION."
if [ -n "$REPO_SLUG" ]; then
  echo "Track the build: https://github.com/$REPO_SLUG/actions"
  echo "Release will appear at: https://github.com/$REPO_SLUG/releases/tag/$NEW_VERSION"
fi
