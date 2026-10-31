#!/bin/sh
# Builds the frontend into internal/assets/dist (embedded in the binary), then
# the yagit binary for every shipped platform into dist/, stamped with
# `git describe`.
set -eu
cd "$(dirname "$0")/.."

find internal/assets/dist -mindepth 1 ! -name .gitkeep -exec rm -rf {} +
pnpm --dir web run build
# The daemon's Content-Security-Policy forbids data: URIs; only the released
# binary would show an asset Vite inlined into a stylesheet.
if grep -rlE "url\(\s*[\"']?data:" --include='*.css' internal/assets/dist; then
  echo "error: a stylesheet above embeds an asset as data: (keep assetsInlineLimit at 0 in web/vite.config.ts)" >&2
  exit 1
fi

version=$(git describe --tags --always --dirty 2>/dev/null || echo dev)
mkdir -p dist
for target in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64; do
  os=${target%/*}
  arch=${target#*/}
  output="dist/yagit-$os-$arch"
  [ "$os" = windows ] && output="$output.exe"
  echo "compiling $target"
  CGO_ENABLED=0 GOOS=$os GOARCH=$arch \
    go build -trimpath -ldflags "-s -w -X main.version=$version" -o "$output" ./cmd/yagit
done
ls -lh dist
