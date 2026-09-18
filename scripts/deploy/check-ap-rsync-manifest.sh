#!/usr/bin/env bash
# youtube-collector 빌드에 필요한 hololive-bot 내부 .go 파일이 ap-rsync 매니페스트에
# 모두 포함되는지 go list -deps로 검증한다.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
MANIFEST="${1:-$ROOT_DIR/scripts/deploy/ap-rsync-files.txt}"
GO_CMD="${GO_CMD:-go}"
if [[ "$MANIFEST" != /* ]]; then
  MANIFEST="$PWD/$MANIFEST"
fi

if ! command -v "$GO_CMD" >/dev/null 2>&1; then
  echo "[FAIL] required Go command not found: $GO_CMD" >&2
  exit 1
fi
if ! GOWORK=off "$GO_CMD" version; then
  echo "[FAIL] Go executable preflight failed: $GO_CMD version" >&2
  exit 1
fi
if [[ ! -r "$MANIFEST" ]]; then
  echo "[FAIL] manifest not readable: $MANIFEST" >&2
  exit 1
fi

while IFS= read -r path; do
  [[ -n "$path" ]] || continue
  if [[ ! -f "$ROOT_DIR/$path" || -L "$ROOT_DIR/$path" ]]; then
    echo "[FAIL] ap-rsync-files.txt entry must be a regular non-symlink file: $path" >&2
    exit 1
  fi
done < "$MANIFEST"

while IFS= read -r path; do
  if ! grep -qxF "$path" "$MANIFEST"; then
    echo "[FAIL] ap-rsync-files.txt missing Compose wrapper dependency: $path" >&2
    exit 1
  fi
done < <(rg -o 'scripts/deploy/lib/[[:alnum:]_.-]+\.sh' "$ROOT_DIR/scripts/deploy/compose.sh" | sort -u)

required_context_files=(
  hololive/hololive-dbtest/go.mod
  hololive/hololive-dbtest/go.sum
  scripts/build/build-youtube-collector-go.sh
)
for path in "${required_context_files[@]}"; do
  if ! grep -qxF "$path" "$MANIFEST"; then
    echo "[FAIL] ap-rsync-files.txt missing Docker build context dependency: $path" >&2
    exit 1
  fi
done

SHARED_GO_DIR="${SHARED_GO_WORKSPACE_PATH:-$ROOT_DIR/../shared-go}"
if [[ ! -d "$SHARED_GO_DIR" ]]; then
  echo "[FAIL] shared-go workspace missing: $SHARED_GO_DIR" >&2
  exit 1
fi
SHARED_GO_DIR="$(cd "$SHARED_GO_DIR" && pwd)"
build_targets=(./cmd/runtime/youtube-collector ./cmd/runtime/healthcheck)
if ! dependencies="$(cd "$ROOT_DIR/hololive/hololive-youtube-collector" &&
  GOWORK=off "$GO_CMD" list -deps -f '{{if and .Module (not .Standard)}}{{range .GoFiles}}{{$.Dir}}/{{.}}{{"\n"}}{{end}}{{range .EmbedFiles}}{{$.Dir}}/{{.}}{{"\n"}}{{end}}{{end}}' "${build_targets[@]}")"; then
  echo "[FAIL] Go dependency enumeration failed: $GO_CMD list -deps (GOWORK=off)" >&2
  exit 1
fi
missing="$(printf '%s\n' "$dependencies" |
  sed "s#^$ROOT_DIR/##; s#^$SHARED_GO_DIR/#../shared-go/#" |
  grep -E '^(hololive/|\.\./shared-go/)' |
  sort -u |
  while IFS= read -r f; do grep -qxF "$f" "$MANIFEST" || echo "$f"; done)"

if [[ -n "$missing" ]]; then
  echo "[FAIL] ap-rsync-files.txt missing youtube-collector build deps:" >&2
  while IFS= read -r missing_file; do printf ' - %s\n' "$missing_file" >&2; done <<< "$missing"
  exit 1
fi
echo "[PASS] ap-rsync-files.txt covers youtube-collector build deps"
