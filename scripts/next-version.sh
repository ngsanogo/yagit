#!/bin/sh
# Prints the next release version from the Conventional Commits since the last
# vX.Y.Z tag, or nothing when no commit asks for a release. Before 1.0 a breaking
# change bumps the minor version.
set -eu

last=$(git describe --tags --abbrev=0 --match 'v[0-9]*.[0-9]*.[0-9]*' 2>/dev/null || echo v0.0.0)
range="$last..HEAD"
[ "$last" = v0.0.0 ] && range=HEAD

bump='none'
subjects=$(git log --format='%s' "$range")
bodies=$(git log --format='%b' "$range")
if printf '%s\n' "$bodies" | grep -qE '^BREAKING[ -]CHANGE:' ||
  printf '%s\n' "$subjects" | grep -qE '^[a-z]+(\([^)]*\))?!:'; then
  bump='major'
elif printf '%s\n' "$subjects" | grep -qE '^feat(\([^)]*\))?:'; then
  bump='minor'
elif printf '%s\n' "$subjects" | grep -qE '^(fix|perf|revert)(\([^)]*\))?:'; then
  bump='patch'
fi

IFS=. read -r major minor patch <<VERSION
${last#v}
VERSION
case "$bump" in
  none) exit 0 ;;
  major) if [ "$major" -eq 0 ]; then minor=$((minor + 1)); patch=0; else major=$((major + 1)); minor=0; patch=0; fi ;;
  minor) minor=$((minor + 1)); patch=0 ;;
  patch) patch=$((patch + 1)) ;;
esac
echo "v$major.$minor.$patch"
