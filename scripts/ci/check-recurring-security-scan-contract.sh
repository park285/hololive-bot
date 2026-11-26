#!/usr/bin/env bash
set -euo pipefail

root_dir="$(git rev-parse --show-toplevel)"
cd "$root_dir"

fail() {
  echo "recurring security scan contract failed: $*" >&2
  exit 1
}

tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT

sed -n 's/^Environment=[A-Z0-9_]*_IMAGE=\(localhost\/\)/\1/p' deploy/quadlet/*/*.container | LC_ALL=C sort -u >"$tmp_dir/final-images"
[[ -s "$tmp_dir/final-images" ]] || fail "no production Quadlet images found"
cut -d'|' -f3 scripts/ci/final-image-scan-manifest.txt | LC_ALL=C sort >"$tmp_dir/manifest-images"
cmp -s "$tmp_dir/manifest-images" "$tmp_dir/final-images" ||
  fail "final image manifest does not cover the exact current production Quadlet image corpus"

awk -F'|' '
  NF != 3 || $1 != "local" || $2 != "linux/arm64" || $3 == "" { exit 1 }
' scripts/ci/final-image-scan-manifest.txt || fail "final image manifest must list only arm64 local builds"

echo "final image scan inventory passed"
