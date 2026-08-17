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
if [[ ! -r "$MANIFEST" ]]; then
  echo "[FAIL] manifest not readable: $MANIFEST" >&2
  exit 1
fi

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

SHARED_GO_DIR="$(cd "$ROOT_DIR/../shared-go" 2>/dev/null && pwd || true)"
build_targets=(./cmd/runtime/youtube-collector ./cmd/runtime/healthcheck)
missing="$(cd "$ROOT_DIR/hololive/hololive-youtube-collector" &&
  "$GO_CMD" list -deps -f '{{if and .Module (not .Standard)}}{{range .GoFiles}}{{$.Dir}}/{{.}}{{"\n"}}{{end}}{{range .EmbedFiles}}{{$.Dir}}/{{.}}{{"\n"}}{{end}}{{end}}' "${build_targets[@]}" 2>/dev/null |
  sed "s#^$ROOT_DIR/##; s#^$SHARED_GO_DIR/#../shared-go/#" |
  grep -E '^(hololive/|\.\./shared-go/)' |
  sort -u |
  while IFS= read -r f; do grep -qxF "$f" "$MANIFEST" || echo "$f"; done)"

if [[ -n "$missing" ]]; then
  echo "[FAIL] ap-rsync-files.txt missing youtube-collector build deps:" >&2
  echo "$missing" | sed 's/^/ - /' >&2
  exit 1
fi
echo "[PASS] ap-rsync-files.txt covers youtube-collector build deps"
