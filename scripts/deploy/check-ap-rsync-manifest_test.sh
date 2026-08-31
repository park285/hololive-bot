#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
CHECKER="$ROOT_DIR/scripts/deploy/check-ap-rsync-manifest.sh"
MANIFEST="$ROOT_DIR/scripts/deploy/ap-rsync-files.txt"
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT

fail() {
  echo "[FAIL] $*" >&2
  exit 1
}

expect_failure() {
  local label="$1"
  local expected="$2"
  local manifest="$3"
  local checker="${4:-$CHECKER}"
  local output

  if output="$("$checker" "$manifest" 2>&1)"; then
    fail "$label"
  fi
  if [[ "$output" != *"$expected"* ]]; then
    fail "$label: unexpected output: $output"
  fi
}

"$CHECKER" "$MANIFEST"

missing_path_manifest="$TMP_DIR/missing-path.txt"
cp "$MANIFEST" "$missing_path_manifest"
printf '%s\n' 'hololive/hololive-shared/pkg/httpbody/body.go' >> "$missing_path_manifest"
expect_failure \
  "stale manifest path must fail closed" \
  "entry must be a regular non-symlink file: hololive/hololive-shared/pkg/httpbody/body.go" \
  "$missing_path_manifest"

symlink_root="$TMP_DIR/symlink-root"
mkdir -p "$symlink_root/scripts/deploy"
cp "$CHECKER" "$symlink_root/scripts/deploy/check-ap-rsync-manifest.sh"
printf '%s\n' fixture >"$symlink_root/regular-file"
ln -s regular-file "$symlink_root/linked-file"
printf '%s\n' linked-file >"$symlink_root/symlink-manifest.txt"
expect_failure \
  "symlink manifest path must fail closed" \
  "entry must be a regular non-symlink file: linked-file" \
  "$symlink_root/symlink-manifest.txt" \
  "$symlink_root/scripts/deploy/check-ap-rsync-manifest.sh"

missing_dependency_manifest="$TMP_DIR/missing-dependency.txt"
grep -vxF '../shared-go/pkg/panicguard/panicguard.go' "$MANIFEST" > "$missing_dependency_manifest"
expect_failure \
  "missing shared panicguard dependency must fail closed" \
  "../shared-go/pkg/panicguard/panicguard.go" \
  "$missing_dependency_manifest"

echo "[PASS] AP rsync manifest mutation checks"
