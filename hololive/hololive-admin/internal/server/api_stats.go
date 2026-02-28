package server

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/health"
)

const systemStatsStreamInterval = 5 * time.Second

// GetStats: 봇 통계를 반환합니다. (성능 최적화를 위해 병렬 조회)
func (h *StatsAPIHandler) GetStats(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), constants.RequestTimeout.AdminRequest)
	defer cancel()

	var (
		members   []*domain.Member
		alarmKeys []*domain.AlarmEntry
		memberErr error
		alarmErr  error
		wg        sync.WaitGroup
	)

	// 병렬로 데이터 조회
	wg.Add(2)
	go func() {
		defer wg.Done()
		members, memberErr = h.repo.GetAllMembers(ctx)
	}()
	go func() {
		defer wg.Done()
		alarmKeys, alarmErr = h.alarm.GetAllAlarmKeys(ctx)
	}()
	wg.Wait()

	if memberErr != nil || alarmErr != nil {
		h.logger.Error("failed to collect stats",
			slog.Any("member_error", memberErr),
			slog.Any("alarm_error", alarmErr))
		c.JSON(500, gin.H{"error": "failed to collect stats"})
		return
	}

	// ACL 서비스에서 rooms 수 조회
	var roomCount int
	if h.acl != nil {
		_, rooms := h.acl.GetACLStatus()
		roomCount = len(rooms)
	}

	c.JSON(200, gin.H{
		"status":  "ok",
		"members": len(members),
		"alarms":  len(alarmKeys),
		"rooms":   roomCount,
		"version": health.GetVersion(),
		"uptime":  health.GetUptime(),
	})
}

// StreamSystemStats: WebSocket을 통해 시스템 리소스 사용량을 실시간 스트리밍합니다.
// 5초마다 CPU/메모리 통계를 전송합니다.
func (h *StatsAPIHandler) StreamSystemStats(c *gin.Context) {
	if h.systemStats == nil {
		c.JSON(400, gin.H{
			"status":  "error",
			"message": "System stats collector not available",
		})
		return
	}

	// WebSocket 업그레이드
	conn, err := wsUpgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		h.logger.Warn("failed to upgrade websocket", slog.Any("error", err))
		c.JSON(400, gin.H{"error": "failed to upgrade websocket connection"})
		return
	}
	defer func() {
		if closeErr := conn.Close(); closeErr != nil {
			h.logger.Warn("failed to close websocket connection", slog.Any("error", closeErr))
		}
	}()

	ctx := c.Request.Context()
	ticker := time.NewTicker(systemStatsStreamInterval)
	defer ticker.Stop()

	// 최초 1회 즉시 전송
	stats, err := h.systemStats.GetCurrentStats(ctx)
	if err != nil {
		h.logger.Error("failed to collect initial system stats", slog.Any("error", err))
		return
	}
	if err := conn.WriteJSON(stats); err != nil {
		h.logger.Warn("failed to write initial system stats", slog.Any("error", err))
		return
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			stats, err := h.systemStats.GetCurrentStats(ctx)
			if err != nil {
				h.logger.Error("failed to collect system stats", slog.Any("error", err))
				return
			}
			if err := conn.WriteJSON(stats); err != nil {
				h.logger.Warn("failed to write system stats", slog.Any("error", err))
				return
			}
		}
	}
}
