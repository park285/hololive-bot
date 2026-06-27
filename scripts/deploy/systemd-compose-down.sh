#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="${HOLOLIVE_BOT_ROOT:-/opt/hololive-bot/compose/current}"
cd "$ROOT_DIR"

if [[ "${EUID:-$(id -u)}" -eq 0 ]]; then
  verifier="${ROOT_DIR}/scripts/deploy/verify-exec-tree-ownership.sh"

  # verifier 를 root 로 실행하기 전에, verifier 와 그 부모 경로가 root-owned·비root 쓰기 불가·
  # 심링크 아님을 repo verifier 에 의존하지 않고 직접 확인한다. 이 self-check 가 없으면
  # verifier 자체를 장악한 비root 사용자가 다음 root 실행에서 임의 코드를 실행한다(4d57f81c).
  bp="${verifier}"
  while :; do
    if [[ -L "${bp}" || ! -e "${bp}" ]]; then
      echo "[SECURITY] root verifier path is a symlink or missing: ${bp} (4d57f81c)." >&2
      exit 1
    fi
    bp_perms="$(printf '%04d' "$((10#$(stat -c '%a' -- "${bp}")))")"
    if [[ "$(stat -c '%u' -- "${bp}")" -ne 0 ]] \
       || (( ( ${bp_perms:3:1} & 2 ) != 0 )) \
       || { (( ( ${bp_perms:2:1} & 2 ) != 0 )) && [[ "$(stat -c '%g' -- "${bp}")" -ne 0 ]]; }; then
      echo "[SECURITY] root verifier is writable by a non-root user: ${bp} (4d57f81c)." >&2
      echo "           chown the deploy tree to root before running deploy units as root." >&2
      exit 1
    fi
    [[ "${bp}" == "/" ]] && break
    bp="$(dirname -- "${bp}")"
  done

  exec_tree=(
    "${verifier}"
    "${ROOT_DIR}/scripts/deploy/systemd-compose-down.sh"
    "${ROOT_DIR}/scripts/deploy/compose.sh"
    "${ROOT_DIR}/scripts/deploy/lib/compose-env.sh"
    "${ROOT_DIR}/scripts/deploy/lib/removed-runtimes.sh"
    "${ROOT_DIR}/scripts/deploy/lib/health-gate.sh"
  )
  while IFS= read -r yml; do
    exec_tree+=("${yml}")
  done < <(find "${ROOT_DIR}/deploy/compose" -maxdepth 1 -type f -name 'docker-compose*.yml' 2>/dev/null | sort)

  if ! "${verifier}" "${exec_tree[@]}"; then
    echo "[SECURITY] root-executed deploy tree is writable by a non-root user (03e6dca8)." >&2
    echo "           chown the tree to root (or run this unit as a constrained service user)." >&2
    exit 1
  fi
fi

export COMPOSE_ENV_FILE=/run/hololive-bot/compose.env

down_files=(
  -f deploy/compose/docker-compose.prod.yml
  -f deploy/compose/docker-compose.main-ap.yml
)
if [[ "${HOLOLIVE_ENABLE_LIVE_COMPAT:-}" == "1" ]]; then
  down_files=(
    -f deploy/compose/docker-compose.prod.yml
    -f deploy/compose/docker-compose.live-compat.yml
    -f deploy/compose/docker-compose.main-ap.yml
    -f deploy/compose/docker-compose.main-ap.live-compat.yml
  )
fi

COMPOSE_PROFILES=main-ap ./scripts/deploy/compose.sh "${down_files[@]}" down
