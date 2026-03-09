// Package server: HTTP 서버 및 라우팅
package server

import (
	"net/http"

	"github.com/gorilla/websocket"

	"github.com/park285/llm-kakao-bots/admin-dashboard/internal/config"
)

// newWSUpgrader: WebSocket Upgrader 생성 (Origin 검증 포함)
//
// 3상태 모드:
// - enforce: 잘못된 Origin 거부
// - monitor: 잘못된 Origin 로그만 남기고 허용
// - off: Origin 검증 건너뛰기
func (s *Server) newWSUpgrader() *websocket.Upgrader {
	return &websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin: func(r *http.Request) bool {
			origin := r.Header.Get("Origin")

			// off 모드: 검증 건너뛰기
			if s.securityCfg.WSOriginMode == config.SecurityModeOff {
				return true
			}

			// Origin 없는 요청은 브라우저 UI가 아님
			if origin == "" {
				if s.securityCfg.WSOriginMode == config.SecurityModeMonitor {
					s.logger.Warn("ws_origin_missing",
						"reason", "Origin 헤더 없음",
						"mode", "monitor",
					)
					return true
				}
				return false
			}

			// Origin 검증
			_, ok := s.allowedOriginsMap[origin]
			if !ok {
				if s.securityCfg.WSOriginMode == config.SecurityModeMonitor {
					s.logger.Warn("ws_origin_rejected",
						"origin", origin,
						"mode", "monitor",
						"allowed", s.allowedOriginsSlice,
					)
					return true
				}
				return false
			}

			return true
		},
	}
}
