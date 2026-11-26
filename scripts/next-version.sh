#!/usr/bin/env bash
# Prints the version the next release should have, or nothing when the
# commits since the last release do not call for one.
#
# The version follows from the Conventional Commits prefixes of the commits
# since the last version tag:
#
#   fix: or perf:                      -> patch  (v0.3.1 -> v0.3.2)
#   feat:                              -> minor  (v0.3.1 -> v0.4.0)
#   type!: or a BREAKING CHANGE footer -> major  (v1.3.1 -> v2.0.0)
#
# While the version is below v1.0.0, a breaking change raises the minor
# number instead, as Semantic Versioning allows for initial development.
# Commits of any other type (docs, test, chore, ci, refactor, ...) release
# nothing.
set -euo pipefail

last=$(git describe --tags --abbrev=0 --match 'v[0-9]*.[0-9]*.[0-9]*' 2>/dev/null || true)
if [[ -n "$last" ]]; then
  range="$last..HEAD"
else
  range="HEAD"
  last="v0.0.0"
fi

log=$(git log --format='%s%n%b' "$range")
if grep -qE '^[a-z]+(\([^)]*\))?!:|^BREAKING[ -]CHANGE:' <<<"$log"; then
  bump=major
elif grep -qE '^feat(\([^)]*\))?:' <<<"$log"; then
  bump=minor
elif grep -qE '^(fix|perf)(\([^)]*\))?:' <<<"$log"; then
  bump=patch
else
  exit 0
fi

IFS=. read -r major minor patch <<<"${last#v}"
if [[ "$bump" == major && "$major" == 0 ]]; then
  bump=minor
fi
case "$bump" in
  major) major=$((major + 1)); minor=0; patch=0 ;;
  minor) minor=$((minor + 1)); patch=0 ;;
  patch) patch=$((patch + 1)) ;;
esac
echo "v$major.$minor.$patch"
