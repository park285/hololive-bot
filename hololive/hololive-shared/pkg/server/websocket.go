package server

import (
	"net/http"
	"os"
	"strings"

	"github.com/gorilla/websocket"
)

var wsAllowedOrigins []string

var wsUpgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     checkOrigin,
}

// WSUpgrader: API WebSocket 업그레이더 기본 설정입니다.
var WSUpgrader = wsUpgrader

func init() {
	InitWSUpgrader()
}

func parseOrigins(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}

	parts := strings.Split(raw, ",")
	origins := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		origins = append(origins, trimmed)
	}

	if len(origins) == 0 {
		return nil
	}
	return origins
}

func checkOrigin(r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
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

// InitWSUpgrader: 환경변수 WEBSOCKET_ALLOWED_ORIGINS를 읽어 WebSocket Origin 허용 목록을 초기화합니다.
func InitWSUpgrader() {
	wsAllowedOrigins = parseOrigins(os.Getenv("WEBSOCKET_ALLOWED_ORIGINS"))
	wsUpgrader.CheckOrigin = checkOrigin
	WSUpgrader = wsUpgrader
}
