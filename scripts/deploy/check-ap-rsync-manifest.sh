#!/usr/bin/env bash
# youtube-producer 빌드에 필요한 hololive-bot 내부 .go 파일이 ap-rsync 매니페스트에
# 모두 포함되는지 go list -deps로 검증한다. 새 패키지 추가 시 매니페스트 누락을
# 배포 전에 잡는다(과거 d86cb826/226977ef 누락 이력). go가 없으면 경고 후 skip하고
# 원격 빌드를 최종 안전망으로 둔다.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
MANIFEST="${1:-$ROOT_DIR/scripts/deploy/ap-rsync-files.txt}"
if [[ "$MANIFEST" != /* ]]; then
  MANIFEST="$PWD/$MANIFEST"
fi

if ! command -v go >/dev/null 2>&1; then
  echo "[WARN] go not found; skipping ap-rsync manifest completeness check" >&2
  exit 0
fi
if [[ ! -r "$MANIFEST" ]]; then
  echo "[FAIL] manifest not readable: $MANIFEST" >&2
  exit 1
fi

# Dockerfile의 로컬 replace는 런타임 import 그래프에 없더라도 go mod download 전에
# 대상 모듈의 metadata가 원격 build context에 존재해야 한다.
required_context_files=(
  hololive/hololive-dbtest/go.mod
  hololive/hololive-dbtest/go.sum
)
for path in "${required_context_files[@]}"; do
  if ! grep -qxF "$path" "$MANIFEST"; then
    echo "[FAIL] ap-rsync-files.txt missing Docker build context dependency: $path" >&2
    exit 1
  fi
done

# Dockerfile이 실제 빌드하는 런타임 타겟만 검사한다. 운영 CLI(cmd/ops/...)는
# 원격 AP 컨테이너에 포함되지 않으므로 매니페스트 대상이 아니다.
# shared-go는 replace(../../../shared-go)로 로컬 워크스페이스를 쓰므로 그 의존 파일도
# ../shared-go/ 경로로 매니페스트에 있어야 원격 빌드가 성립한다.
SHARED_GO_DIR="$(cd "$ROOT_DIR/../shared-go" 2>/dev/null && pwd || true)"
build_targets=(./cmd/runtime/youtube-producer ./cmd/runtime/healthcheck)
missing="$(cd "$ROOT_DIR/hololive/hololive-youtube-producer" &&
  go list -deps -f '{{if and .Module (not .Standard)}}{{range .GoFiles}}{{$.Dir}}/{{.}}{{"\n"}}{{end}}{{range .EmbedFiles}}{{$.Dir}}/{{.}}{{"\n"}}{{end}}{{end}}' "${build_targets[@]}" 2>/dev/null |
  sed "s#^$ROOT_DIR/##; s#^$SHARED_GO_DIR/#../shared-go/#" |
  grep -E '^(hololive/|\.\./shared-go/)' |
  sort -u |
  while IFS= read -r f; do grep -qxF "$f" "$MANIFEST" || echo "$f"; done)"

if [[ -n "$missing" ]]; then
  echo "[FAIL] ap-rsync-files.txt missing youtube-producer build deps:" >&2
  echo "$missing" | sed 's/^/ - /' >&2
  exit 1
fi
echo "[PASS] ap-rsync-files.txt covers youtube-producer build deps"
