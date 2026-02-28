#!/bin/bash
# PGO (Profile-Guided Optimization) 빌드 스크립트
# 실행 프로파일을 수집하여 최적화된 바이너리 생성

set -e

PROJECT_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$PROJECT_ROOT"

PROFILE_DIR="pgo-profiles"
PROFILE_FILE="$PROFILE_DIR/default.pprof"
PGO_BINARY="bin/bot-pgo"

echo "=== PGO (Profile-Guided Optimization) Build ==="
echo ""

# === Step 1: 프로파일 수집용 바이너리 빌드 ===
echo "[1/4] Building profiling binary..."
CGO_ENABLED=0 go build -tags go_json -o bin/bot-profiling ./cmd/bot

echo "[2/4] Collecting CPU profile..."
echo "   ⚠️  봇을 1-2분간 실행하여 프로파일을 수집합니다."
echo "   ⚠️  실제 워크로드를 시뮬레이션하세요 (명령어 입력, API 호출 등)"
echo ""
echo "   실행 방법:"
echo "   1. 다른 터미널에서: ./bin/bot-profiling"
echo "   2. 봇이 시작되면 실제로 사용 (1-2분)"
echo "   3. Ctrl+C로 종료"
echo ""
echo "   또는 자동 프로파일링:"
read -p "   자동으로 30초간 프로파일링 하시겠습니까? (y/n): " -n 1 -r
echo

if [[ $REPLY =~ ^[Yy]$ ]]; then
    mkdir -p "$PROFILE_DIR"
    
    # CPU 프로파일 활성화하여 실행
    CPUPROFILE="$PROFILE_FILE" timeout 30s ./bin/bot-profiling 2>/dev/null || true
    
    if [ -f "$PROFILE_FILE" ]; then
        echo "   ✓ Profile collected: $PROFILE_FILE"
    else
        echo "   ✗ Failed to collect profile"
        exit 1
    fi
else
    echo ""
    echo "   수동 프로파일링 안내:"
    echo "   1. CPUPROFILE=$PROFILE_FILE ./bin/bot-profiling"
    echo "   2. 봇 사용 후 Ctrl+C"
    echo "   3. 다시 이 스크립트 실행"
    echo ""
    
    if [ ! -f "$PROFILE_FILE" ]; then
        echo "   ✗ Profile not found: $PROFILE_FILE"
        echo "   먼저 프로파일을 수집하세요."
        exit 1
    fi
fi

# === Step 3: 프로파일 분석 ===
echo ""
echo "[3/4] Analyzing profile..."
go tool pprof -top -cum "$PROFILE_FILE" 2>/dev/null | head -15 || echo "   (프로파일 내용 생략)"

# === Step 4: PGO 빌드 ===
echo ""
echo "[4/4] Building with PGO..."

# 프로파일을 default.pgo로 복사 (Go 1.21+ 자동 인식)
cp "$PROFILE_FILE" default.pgo

# PGO 활성화 빌드
time CGO_ENABLED=0 go build -tags netgo,go_json -ldflags="-s -w" -pgo=auto -o "$PGO_BINARY" ./cmd/bot

# === 완료 ===
echo ""
echo "✓ PGO build completed!"
echo ""
SIZE=$(du -h "$PGO_BINARY" | cut -f1)
echo "   Binary: $PGO_BINARY ($SIZE)"
echo ""
echo "예상 성능 향상:"
echo "   - CPU: 5-15% 감소"
echo "   - 처리량: 5-20% 증가"
echo "   - 레이턴시: 5-10% 감소"
echo ""
echo "사용 방법:"
echo "   cp $PGO_BINARY bin/bot"
echo "   ./scripts/restart-bot.sh"
