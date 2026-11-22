#!/bin/sh
set -eu

cd "$(dirname "$0")"
gist="74929d87bb6c220c23bea4d9d1b37377"
stage="dist/.gist"

with_gh() {
  git -c credential.helper= -c 'credential.helper=!gh auth git-credential' "$@"
}

./notices.sh
if [ -n "$(git status --porcelain)" ]; then
  echo "Commit your changes first (THIRD-PARTY-NOTICES.txt may have been updated)."
  exit 1
fi

go test ./...
./build.sh

rm -rf "$stage"
mkdir -p "$stage"
gzip -9 -n -c "dist/WhatsApp Exporter" > "$stage/WhatsApp-Exporter-mac.gz"
for arch in amd64 arm64; do
  gzip -9 -n -c "dist/WhatsApp Exporter-windows-$arch.exe" > "$stage/WhatsApp-Exporter-windows-$arch.exe.gz"
done
git archive --format=tar.gz --prefix=whatsapp-exporter/ -o "$stage/whatsapp-exporter-source.tar.gz" HEAD
cp README.md LICENSE THIRD-PARTY-NOTICES.txt install.sh install.ps1 "$stage/"

version="$(date +%Y.%m.%d-%H%M) ($(git rev-parse --short HEAD))"
git -C "$stage" init -q -b main
git -C "$stage" add -A
git -C "$stage" -c user.name="$(git config user.name)" -c user.email="$(git config user.email)" commit -q -m "WhatsApp Exporter $version"
with_gh -C "$stage" push -q --force "https://gist.github.com/$gist.git" main
rm -rf "$stage"
with_gh push -q origin main

echo "Released WhatsApp Exporter $version"
