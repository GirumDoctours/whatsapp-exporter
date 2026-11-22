#!/bin/sh
set -eu

cd "$(dirname "$0")"
out="THIRD-PARTY-NOTICES.txt"
rule="================================================================"

modules() {
  for os in darwin windows; do
    GOOS=$os go list -deps -f '{{if .Module}}{{if not .Module.Main}}{{.Module.Path}}{{end}}{{end}}' .
  done | sort -u
}

section() {
  printf '%s\n%s\n%s\n\n' "$rule" "$1" "$rule"
}

{
  echo "WhatsApp Exporter is licensed under the GNU General Public License v3 (see LICENSE)."
  echo "It includes the following third-party software."
  echo
  section "Go standard library and runtime ($(go env GOVERSION))"
  goroot="$(go env GOROOT)"
  for file in "$goroot/LICENSE" "$goroot/../LICENSE" "$goroot/PATENTS"; do
    if [ -f "$file" ]; then
      cat "$file"
      echo
    fi
  done
  for module in $(modules); do
    version="$(go list -m -f '{{.Version}}' "$module")"
    dir="$(go list -m -f '{{.Dir}}' "$module")"
    section "$module $version"
    for file in "$dir"/LICENSE* "$dir"/LICENCE* "$dir"/COPYING* "$dir"/NOTICE*; do
      if [ -f "$file" ]; then
        cat "$file"
        echo
      fi
    done
  done
} > "$out"

echo "Wrote $out"
