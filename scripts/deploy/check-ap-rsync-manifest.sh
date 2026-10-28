#!/usr/bin/env bash
# Compose AP(ap-deploy.sh)로 보내는 source 파일 목록을 검증한다.
# AP는 이미지를 빌드하지 않고 kapu가 빌드한 collector·issuer 이미지 아카이브를 받는다. 그래서 목록에는
# AP가 ~/hololive-bot과 배포 백업의 source-candidate에서 읽거나 실행하는 Compose 파일, wrapper와 원격
# 스크립트, runtime VERSION만 둔다. Go 소스·모듈 파일·Docker 빌드 컨텍스트는 대상이 아니다.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
MANIFEST="${1:-$ROOT_DIR/scripts/deploy/ap-rsync-files.txt}"

fail() {
  echo "[FAIL] $*" >&2
  exit 1
}

[[ -r "$MANIFEST" ]] || fail "manifest not readable: $MANIFEST"

declare -A shipped=()
while IFS= read -r path; do
  [[ -n "$path" ]] || continue
  case "$path" in
    /*|../*|*/../*) fail "ap-rsync-files.txt entry must stay inside the repository: $path" ;;
  esac
  [[ -f "$ROOT_DIR/$path" && ! -L "$ROOT_DIR/$path" ]] ||
    fail "ap-rsync-files.txt entry must be a regular non-symlink file: $path"
  # source snapshot(ap-source-snapshot.py)은 중복 경로가 있는 목록을 거절한다.
  [[ -z "${shipped[$path]+x}" ]] || fail "ap-rsync-files.txt has a duplicate entry: $path"
  # 비밀·데이터·문서·로그·백업과 중앙 전용 alarm-worker 트리는 보내지 않는다.
  # alarm-worker에서는 compose.sh가 이미지 버전을 맞추려고 읽는 VERSION만 예외다.
  if [[ "$path" != hololive/hololive-alarm-worker/VERSION &&
    "$path" =~ (^|/)(\.env[^/]*|[^/]*\.key|[^/]*\.pem|hololive-alarm-worker|[^/]*_test\.go|data|docs|logs|runtime-config|backups|artifacts)(/|$) ]]; then
    fail "ap-rsync-files.txt entry is outside the deployment scope: $path"
  fi
  shipped[$path]=1
done < "$MANIFEST"

require() {
  [[ -n "${shipped[$1]+x}" ]] || fail "ap-rsync-files.txt missing $2: $1"
}

# kapu에서 보내는 원격 명령이 AP tree에서 직접 읽거나 실행하는 파일이다. 호출자는 ap-deploy.sh,
# ap-rollback.sh(source-candidate의 po-ap-rollback-remote.sh), ap-completion-check.sh,
# scripts/logs/ap-status.sh, scripts/logs/ap-smoke.sh다.
for path in \
  deploy/compose/docker-compose.collector.yml \
  deploy/compose/docker-compose.prod.yml \
  hololive/hololive-api/VERSION \
  scripts/deploy/compose.sh \
  scripts/deploy/lib/ap-collector-readiness.sh \
  scripts/deploy/lib/po-ap-rollback-remote.sh \
  scripts/deploy/lib/po-sandbox-image.sh \
  scripts/deploy/lib/require-quic-udp-buffer.sh \
  scripts/deploy/lib/worker-profile-pair.py \
  scripts/deploy/lib/youtubejs-node-version.sh; do
  require "$path" "AP remote command input"
done

# Compose runtime AP host의 원격 명령은 그 host의 overlay를 compose.sh에 넘긴다.
for conf in "$ROOT_DIR"/scripts/deploy/ap-hosts/*.conf; do
  IFS="|" read -r mode compose_file < <(
    AP_RUNTIME_MODE=""
    AP_COMPOSE_FILE=""
    # shellcheck disable=SC1090
    . "$conf"
    printf '%s|%s\n' "$AP_RUNTIME_MODE" "$AP_COMPOSE_FILE"
  )
  case "$mode" in
    compose) ;;
    native) continue ;;
    *) fail "AP_RUNTIME_MODE must be compose or native in ${conf##*/}" ;;
  esac
  require "$compose_file" "Compose AP overlay of ${conf##*/}"
done

# 보내는 스크립트가 참조하는 저장소 파일(lib, 정책 표, runtime VERSION)도 보내야 AP에서 source·실행할 수 있다.
for path in "${!shipped[@]}"; do
  case "$path" in
    *.sh|*.py) ;;
    *) continue ;;
  esac
  while IFS= read -r dependency; do
    require "$dependency" "dependency of $path"
  done < <(grep -oE '(scripts/[[:alnum:]_./-]+\.(sh|py|tsv)|hololive/hololive-[[:alnum:]-]+/VERSION)' "$ROOT_DIR/$path" | sort -u)
done

echo "[PASS] ap-rsync-files.txt covers the Compose AP runtime inputs"
