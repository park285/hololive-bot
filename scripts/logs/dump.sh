#!/usr/bin/env bash
# Loki 로그 → logs/ 디렉토리 주기적 덤프
# cron: 0 */2 * * * /home/kapu/gemini/hololive-bot/scripts/logs/dump.sh >> /home/kapu/gemini/hololive-bot/logs/dump-cron.log 2>&1
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
LOG_DIR="${REPO_ROOT}/logs"

# --- 서비스명 → container_name 매핑 ---
declare -A SERVICE_MAP=(
  [bot]="hololive-bot"
  [admin]="admin-api"
  [dispatcher]="alarm-dispatcher"
  [ingester]="stream-ingester"
  [llm]="llm-scheduler"
  [alarm]="hololive-alarm"
  [scraper]="hololive-scraper"
  [rust-dispatcher]="rust-dispatcher"
)

LOKI_PORT=3100
LOKI_ADDR="http://127.0.0.1:${LOKI_PORT}"
NAMESPACE="hololive"
PF_PID=""

SINCE="2h"
LIMIT=10000
# 로테이션 임계값 (100MB)
ROTATE_BYTES=$((100 * 1024 * 1024))
# 보관 기간 (30일)
RETENTION_DAYS=30

cleanup() {
  if [[ -n "${PF_PID}" ]] && kill -0 "${PF_PID}" 2>/dev/null; then
    kill "${PF_PID}" 2>/dev/null || true
    wait "${PF_PID}" 2>/dev/null || true
  fi
}
trap cleanup EXIT INT TERM

# --- logcli 사전 조건 확인 ---
if ! command -v logcli &>/dev/null; then
  echo "ERROR: logcli 미설치" >&2
  exit 1
fi

# --- logs 디렉토리 생성 ---
mkdir -p "${LOG_DIR}"

# --- port-forward 시작 (이미 열려있으면 스킵) ---
if ! curl -sf "${LOKI_ADDR}/ready" &>/dev/null; then
  echo "$(date '+%Y-%m-%d %H:%M:%S') port-forward 시작: svc/loki ${LOKI_PORT}:${LOKI_PORT}" >&2
  kubectl port-forward -n "${NAMESPACE}" svc/loki "${LOKI_PORT}:${LOKI_PORT}" &>/dev/null &
  PF_PID=$!
  for i in $(seq 1 20); do
    if curl -sf "${LOKI_ADDR}/ready" &>/dev/null; then break; fi
    if [[ $i -eq 20 ]]; then
      echo "ERROR: Loki port-forward 연결 실패" >&2
      exit 1
    fi
    sleep 0.5
  done
  echo "$(date '+%Y-%m-%d %H:%M:%S') port-forward 준비 완료" >&2
fi

# --- 서비스별 로그 덤프 ---
DUMP_COUNT=0
for SVC in "${!SERVICE_MAP[@]}"; do
  CONTAINER="${SERVICE_MAP[${SVC}]}"
  LOG_FILE="${LOG_DIR}/${SVC}.log"
  QUERY="{kubernetes_namespace_name=\"${NAMESPACE}\", kubernetes_container_name=\"${CONTAINER}\"}"

  # 로테이션: 100MB 초과 시 .log.1로 이동
  if [[ -f "${LOG_FILE}" ]]; then
    FILE_SIZE=$(stat -c%s "${LOG_FILE}" 2>/dev/null || echo 0)
    if [[ ${FILE_SIZE} -gt ${ROTATE_BYTES} ]]; then
      mv -f "${LOG_FILE}" "${LOG_FILE}.1"
      echo "$(date '+%Y-%m-%d %H:%M:%S') 로테이션: ${SVC}.log → ${SVC}.log.1 (${FILE_SIZE} bytes)" >&2
    fi
  fi

  # logcli query → append
  LINE_COUNT=$(LOKI_ADDR="${LOKI_ADDR}" logcli query \
    --since="${SINCE}" \
    --limit="${LIMIT}" \
    --output=raw \
    --quiet \
    "${QUERY}" 2>/dev/null | tee -a "${LOG_FILE}" | wc -l)

  DUMP_COUNT=$((DUMP_COUNT + LINE_COUNT))
  echo "$(date '+%Y-%m-%d %H:%M:%S') ${SVC}: ${LINE_COUNT} lines" >&2
done

# --- 오래된 로테이션 파일 삭제 (30일 초과) ---
DELETED=$(find "${LOG_DIR}" -name "*.log.1" -mtime +${RETENTION_DAYS} -delete -print | wc -l)
if [[ ${DELETED} -gt 0 ]]; then
  echo "$(date '+%Y-%m-%d %H:%M:%S') 정리: ${DELETED}개 로테이션 파일 삭제 (${RETENTION_DAYS}일 초과)" >&2
fi

echo "$(date '+%Y-%m-%d %H:%M:%S') 덤프 완료: 총 ${DUMP_COUNT} lines (${#SERVICE_MAP[@]} services)" >&2
