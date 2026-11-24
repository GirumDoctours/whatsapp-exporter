#!/bin/sh
set -eu

cd "$(dirname "$0")"
mkdir -p dist
flags="-s -w"
export MACOSX_DEPLOYMENT_TARGET=13.0
mac_min="-mmacosx-version-min=13.0"

CGO_ENABLED=1 GOOS=darwin GOARCH=arm64 CC="clang -arch arm64" CGO_CFLAGS="$mac_min" CGO_LDFLAGS="$mac_min" \
  go build -trimpath -ldflags "$flags" -o dist/.exporter-arm64 .
CGO_ENABLED=1 GOOS=darwin GOARCH=amd64 CC="clang -arch x86_64" CGO_CFLAGS="$mac_min" CGO_LDFLAGS="$mac_min" \
  go build -trimpath -ldflags "$flags" -o dist/.exporter-amd64 .
lipo -create -output "dist/WhatsApp Exporter" dist/.exporter-arm64 dist/.exporter-amd64
rm dist/.exporter-arm64 dist/.exporter-amd64
codesign --force --sign - "dist/WhatsApp Exporter"

for arch in amd64 arm64; do
  CGO_ENABLED=0 GOOS=windows GOARCH=$arch go build -trimpath -ldflags "$flags" -o "dist/WhatsApp Exporter-windows-$arch.exe" .
done

echo "Built dist/WhatsApp Exporter (Mac) and dist/WhatsApp Exporter-windows-amd64.exe / -arm64.exe (Windows)"
