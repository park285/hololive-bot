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
  local output

  if output="$("$CHECKER" "$manifest" 2>&1)"; then
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
  "contains missing local path: hololive/hololive-shared/pkg/httpbody/body.go" \
  "$missing_path_manifest"

missing_dependency_manifest="$TMP_DIR/missing-dependency.txt"
grep -vxF '../shared-go/pkg/panicguard/panicguard.go' "$MANIFEST" > "$missing_dependency_manifest"
expect_failure \
  "missing shared panicguard dependency must fail closed" \
  "../shared-go/pkg/panicguard/panicguard.go" \
  "$missing_dependency_manifest"

echo "[PASS] AP rsync manifest mutation checks"
