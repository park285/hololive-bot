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
grep -vxF 'hololive/hololive-youtube-collector/cmd/runtime/youtube-collector/main.go' "$MANIFEST" > "$missing_dependency_manifest"
expect_failure \
  "missing collector entrypoint dependency must fail closed" \
  "hololive/hololive-youtube-collector/cmd/runtime/youtube-collector/main.go" \
  "$missing_dependency_manifest"

echo "[PASS] AP rsync manifest mutation checks"

GO_CMD=/bin/false expect_failure "unusable Go shim must fail" "Go executable preflight failed" "$MANIFEST"
cat >"$TMP_DIR/go-shim" <<'EOF'
#!/usr/bin/env bash
[[ "$GOWORK" == off ]] || exit 92
if [[ "$1" == version ]]; then echo 'go version fixture'; exit 0; fi
echo 'fixture dependency enumeration failure' >&2
exit 91
EOF
chmod +x "$TMP_DIR/go-shim"
GO_CMD="$TMP_DIR/go-shim" expect_failure "dependency error must retain stderr" "fixture dependency enumeration failure" "$MANIFEST"
GO_CMD="$TMP_DIR/go-shim" expect_failure "dependency error must diagnose action" "Go dependency enumeration failed" "$MANIFEST"
SHARED_GO_WORKSPACE_PATH="$TMP_DIR/missing" expect_failure "missing workspace must fail" "shared-go workspace missing" "$MANIFEST"
