#!/bin/bash
set -euo pipefail

repo="GirumDoctours/whatsapp-exporter"
asset="WhatsApp-Exporter-mac.gz"
folder="$HOME/WhatsApp Exporter"
app="$folder/WhatsApp Exporter"

say() {
  printf '\n  %s\n' "$1"
}

if [ "$(uname -s)" != "Darwin" ]; then
  say "WhatsApp Exporter runs on a Mac. Other computers aren't supported yet."
  exit 1
fi

version="$(sw_vers -productVersion)"
if [ "${version%%.*}" -lt 13 ]; then
  say "WhatsApp Exporter needs macOS 13 Ventura or newer. This Mac has macOS $version."
  exit 1
fi

mkdir -p "$folder"
download="$(mktemp "$folder/.download.XXXXXX")"
trap 'rm -f "$download"' EXIT

say "Downloading WhatsApp Exporter..."
if ! curl -fL --progress-bar "https://github.com/$repo/releases/latest/download/$asset" | gunzip > "$download"; then
  say "The download didn't finish. Check your internet connection, then paste the command again."
  exit 1
fi
chmod 755 "$download"
mv -f "$download" "$app"
trap - EXIT

exec "$app" < /dev/tty
