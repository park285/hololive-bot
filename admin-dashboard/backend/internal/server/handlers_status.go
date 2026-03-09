// Package server: HTTP 서버 및 라우팅
package server

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/park285/llm-kakao-bots/admin-dashboard/internal/status"
)

// ===== Status Handlers =====

// handleAggregatedStatus godoc
// @Summary      통합 시스템 상태
// @Description  모든 서비스(Admin, Holo Bot, LLM Server)의 상태를 집계하여 반환
// @Tags         status
// @Accept       json
// @Produce      json
// @Security     SessionCookie
// @Success      200  {object}  status.AggregatedStatus
// @Router       /status [get]
func (s *Server) handleAggregatedStatus(c *gin.Context) {
	if s.statusCollector == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Status collector not initialized"})
		return
	}

	result := s.statusCollector.GetAggregatedStatus(c.Request.Context())
	c.JSON(http.StatusOK, result)
}

// handleSystemStatsStream: WebSocket을 통해 시스템 리소스 사용량을 실시간 스트리밍합니다.
// 2초마다 CPU/메모리/고루틴 통계를 전송합니다.
func (s *Server) handleSystemStatsStream(c *gin.Context) {
	if s.statusCollector == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Status collector not initialized"})
		return
	}

	// WebSocket 업그레이드
	upgrader := s.newWSUpgrader()
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	defer func() { _ = conn.Close() }()

	ctx := c.Request.Context()
	statsChan := make(chan *status.SystemStats, 1)

	// 별도 goroutine에서 stats 스트리밍
	go s.statusCollector.StreamSystemStats(ctx, statsChan)

	for {
		select {
		case <-ctx.Done():
			return
		case stats, ok := <-statsChan:
			if !ok {
				return
			}
			if err := conn.WriteJSON(stats); err != nil {
				return
			}
		}
	}
}
