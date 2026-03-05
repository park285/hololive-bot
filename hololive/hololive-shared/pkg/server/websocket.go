package server

import (
	"net/http"
	"os"
	"strings"

	"github.com/gorilla/websocket"
)

// wsAllowedOrigins: 환경변수 WEBSOCKET_ALLOWED_ORIGINS에서 로드된 허용 오리진 목록
var wsAllowedOrigins []string

func init() {
	InitWSUpgrader()
}

// InitWSUpgrader: WEBSOCKET_ALLOWED_ORIGINS 환경변수에서 허용 오리진을 로드하고 업그레이더를 초기화합니다.
// 비어있으면 모든 WebSocket 연결을 거부합니다 (secure default).
func InitWSUpgrader() {
	raw := os.Getenv("WEBSOCKET_ALLOWED_ORIGINS")
	wsAllowedOrigins = parseOrigins(raw)

	WSUpgrader = websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin:     checkOrigin,
	}
}

// parseOrigins: 쉼표 구분 문자열을 파싱하여 공백 제거 후 오리진 슬라이스 반환
func parseOrigins(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	origins := make([]string, 0, len(parts))
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			origins = append(origins, trimmed)
		}
	}
	return origins
}

// checkOrigin: 요청의 Origin 헤더가 허용 목록에 있는지 검증합니다.
// 허용 목록이 비어있으면 모든 연결을 거부합니다.
func checkOrigin(r *http.Request) bool {
	if len(wsAllowedOrigins) == 0 {
		return false
	}

	origin := r.Header.Get("Origin")
	if origin == "" {
		return false
	}

	for _, allowed := range wsAllowedOrigins {
		if strings.EqualFold(origin, allowed) {
			return true
		}
	}

	return false
}

// WSUpgrader: API WebSocket 업그레이더 기본 설정입니다.
// InitWSUpgrader()를 호출하여 환경변수에서 허용 오리진을 로드해야 합니다.
var WSUpgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     checkOrigin,
}
