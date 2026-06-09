#!/usr/bin/env sh
set -eu

ROOT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
OUT_DIR="${OUT_DIR:-$ROOT_DIR/dist}"
VERSION="${VERSION:-dev}"

mkdir -p "$OUT_DIR"

cd "$ROOT_DIR/web"
npm run build

cd "$ROOT_DIR"
for arch in amd64 arm64; do
  target="$OUT_DIR/frpc-web_${VERSION}_linux_${arch}"
  echo "building $target"
  CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go build -tags embed -trimpath -ldflags "-s -w" -o "$target" ./cmd/frpc-web
  chmod 0755 "$target"
done

echo "release artifacts written to $OUT_DIR"
