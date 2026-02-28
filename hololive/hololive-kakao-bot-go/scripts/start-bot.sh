#!/bin/bash
# Hololive KakaoTalk Bot (Go) 시작 스크립트 v1.0

set -e

PROJECT_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$PROJECT_ROOT"

PID_FILE=".bot.pid"
LOG_DIR="logs"
NOHUP_LOG="$LOG_DIR/nohup.log"
CONTAINER_CLI="${CONTAINER_CLI:-docker}"

case "${CONTAINER_CLI}" in
  docker|podman) ;;
  *)
    echo "[ERROR] Unsupported CONTAINER_CLI: ${CONTAINER_CLI}"
    echo "Allowed values: docker, podman"
    exit 1
    ;;
esac

echo "[START] Starting Hololive KakaoTalk Bot (Go)..."

# === 1. 중복 실행 방지 ===
if [ -f "$PID_FILE" ]; then
  OLD_PID=$(cat "$PID_FILE")
  if ps -p "$OLD_PID" > /dev/null 2>&1; then
    echo "[WARN] Bot is already running (PID: $OLD_PID)"
    echo "Use './scripts/stop-bot.sh' to stop first"
    exit 1
  else
    echo "[WARN] Stale PID file found, removing..."
    rm -f "$PID_FILE"
  fi
fi

# Step 2: 실제 프로세스 검색
RUNNING_PIDS=$(pgrep -f "bin/bot" 2>/dev/null | while read pid; do
  dir=$(readlink -f /proc/$pid/cwd 2>/dev/null || echo "")
  if [ "$dir" = "$PROJECT_ROOT" ]; then
    echo "$pid"
  fi
done)

if [ -n "$RUNNING_PIDS" ]; then
  echo "[ERROR] Bot is already running without PID file!"
  echo "Running PIDs: $RUNNING_PIDS"
  echo "Use './scripts/stop-bot.sh' to stop"
  exit 1
fi

# === 2. 환경 파일 검증 ===
if [ ! -f ".env" ]; then
  echo "[ERROR] .env file not found!"
  echo "Copy .env.example to .env and configure it"
  exit 1
fi

# 필수 환경변수 체크
echo "[CHECK] Validating environment variables..."
REQUIRED_VARS="IRIS_BASE_URL HOLODEX_API_KEY_1 CACHE_HOST"
for var in $REQUIRED_VARS; do
  if ! grep -q "^${var}=" .env; then
    echo "[ERROR] Required variable missing: $var"
    exit 1
  fi

  VALUE=$(grep "^${var}=" .env | cut -d'=' -f2)
  if ! echo "$VALUE" | grep -q .; then
    echo "[ERROR] Required variable is empty: $var"
    exit 1
  fi
done
echo "[OK] Environment variables validated"

# === 3. 바이너리 확인 ===
if [ ! -f "bin/bot" ]; then
  echo "[BUILD] Binary not found, building..."
  CGO_ENABLED=0 go build -tags go_json -o bin/bot ./cmd/bot || {
    echo "[ERROR] Build failed"
    exit 1
  }
fi

# === 4. 로그 디렉토리 준비 ===
mkdir -p "$LOG_DIR"

# 기존 nohup.log 백업 (10MB 이상)
if [ -f "$NOHUP_LOG" ]; then
  LOG_SIZE=$(stat -c%s "$NOHUP_LOG" 2>/dev/null || echo 0)
  if [ "$LOG_SIZE" -gt 10485760 ]; then
    BACKUP_NAME="$LOG_DIR/nohup.log.$(date +%Y%m%d-%H%M%S)"
    mv "$NOHUP_LOG" "$BACKUP_NAME"
    echo "[INFO] Backed up large nohup.log to $BACKUP_NAME"
  fi
fi

# === 5. Redis 연결 확인 ===
echo "[CHECK] Checking Redis connection..."
if ! command -v "${CONTAINER_CLI}" >/dev/null 2>&1; then
  echo "[ERROR] Container CLI not found: ${CONTAINER_CLI}"
  echo "Set CONTAINER_CLI=docker or CONTAINER_CLI=podman"
  exit 1
fi

if ! "${CONTAINER_CLI}" ps | grep "holo-valkey" | grep -q "Up"; then
  echo "[WARN] Valkey container (holo-valkey) is not running!"
  echo "Start it with: ${CONTAINER_CLI} start holo-valkey"
  exit 1
fi

if ! timeout 3 "${CONTAINER_CLI}" exec holo-valkey valkey-cli ping > /dev/null 2>&1; then
  echo "[WARN] Valkey container is running but not responding"
  exit 1
fi
echo "[OK] Valkey connection verified"

# === 6. Iris 서버 확인 ===
echo "[CHECK] Checking Iris server..."
IRIS_PORT=$(grep "^IRIS_BASE_URL=" .env | cut -d'=' -f2 | grep -oP ':\K\d+' || echo "3000")
if ! ss -tuln | grep -q ":$IRIS_PORT "; then
  echo "[WARN] Iris server is not running on port $IRIS_PORT!"
  echo "Make sure Iris server is started"
  exit 1
fi
echo "[OK] Iris server detected on port $IRIS_PORT"

# === 7. 봇 시작 ===
echo "[RUN] Starting bot with optimized GC settings..."
GOGC=60 nohup ./bin/bot > "$NOHUP_LOG" 2>&1 &
BOT_PID=$!

# PID 저장
echo "$BOT_PID" > "$PID_FILE"

echo "Waiting for initialization..."
sleep 4

# === 8. 시작 확인 ===
if ps -p "$BOT_PID" > /dev/null 2>&1; then
  echo "[OK] Bot started successfully"
  echo "   PID: $BOT_PID"
  echo "   Logs:"
  echo "     - Application: $LOG_DIR/bot.log (zap)"
  echo "     - Process: $NOHUP_LOG (stdout/stderr)"
  echo ""
  echo "   Commands:"
  echo "     Status:  ./scripts/status.sh"
  echo "     Stop:    ./scripts/stop-bot.sh"
  echo "     Restart: ./scripts/restart-bot.sh"
else
  echo "[ERROR] Bot failed to start, check logs:"
  echo "   - $NOHUP_LOG"
  echo "   - $LOG_DIR/bot.log"
  tail -30 "$NOHUP_LOG" 2>/dev/null || true
  rm -f "$PID_FILE"
  exit 1
fi
