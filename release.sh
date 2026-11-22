#!/bin/sh
set -eu

cd "$(dirname "$0")"
repo="GirumDoctours/whatsapp-exporter"
asset="dist/WhatsApp-Exporter-mac.gz"

./notices.sh
if [ -n "$(git status --porcelain)" ]; then
  echo "Commit your changes first (THIRD-PARTY-NOTICES.txt may have been updated)."
  exit 1
fi
git fetch --quiet origin main
commit="$(git rev-parse HEAD)"
if [ "$commit" != "$(git rev-parse origin/main)" ]; then
  echo "Push main to GitHub first, so the release matches the published source."
  exit 1
fi

go test ./...
./build.sh
gzip -9 -n -c "dist/WhatsApp Exporter" > "$asset"

tag="v$(date +%Y.%m.%d-%H%M)"
gh release create "$tag" \
  --repo "$repo" \
  --target "$commit" \
  --title "WhatsApp Exporter $tag" \
  --notes "To install or update, follow the steps in the README: https://github.com/$repo#run-it" \
  "$asset" install.sh
rm "$asset"

echo "Released $tag"
