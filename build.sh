#!/bin/sh
set -eu

cd "$(dirname "$0")"
mkdir -p dist
flags="-s -w"

GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags "$flags" -o dist/.exporter-arm64 .
GOOS=darwin GOARCH=amd64 go build -trimpath -ldflags "$flags" -o dist/.exporter-amd64 .
lipo -create -output "dist/WhatsApp Exporter" dist/.exporter-arm64 dist/.exporter-amd64
rm dist/.exporter-arm64 dist/.exporter-amd64
codesign --force --sign - "dist/WhatsApp Exporter"

for arch in amd64 arm64; do
  GOOS=windows GOARCH=$arch go build -trimpath -ldflags "$flags" -o "dist/WhatsApp Exporter-windows-$arch.exe" .
done

echo "Built dist/WhatsApp Exporter (Mac) and dist/WhatsApp Exporter-windows-amd64.exe / -arm64.exe (Windows)"
