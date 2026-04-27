// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

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
	sharedserver "github.com/kapu/hololive-shared/pkg/server"
)

const systemStatsStreamInterval = 5 * time.Second

func (h *StatsAPIHandler) GetStats(c *gin.Context) {
	if !h.requireStatsDeps(c) {
		return
	}

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
		h.safeLogger().Error("failed to collect stats",
			slog.Any("member_error", memberErr),
			slog.Any("alarm_error", alarmErr))
		sharedserver.RespondError(c, 500, "failed to collect stats", nil)

		return
	}

	// ACL 서비스에서 rooms 수 조회
	var roomCount int

	if h.acl != nil {
		_, _, rooms := h.acl.GetACLStatus()

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

// 5초마다 CPU/메모리 통계를 전송합니다.
func (h *StatsAPIHandler) StreamSystemStats(c *gin.Context) {
	if h == nil || h.APIHandler == nil || h.systemStats == nil {
		sharedserver.RespondError(c, 400, "System stats collector not available", nil)

		return
	}

	// WebSocket 업그레이드
	conn, err := sharedserver.WSUpgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		h.safeLogger().Warn("failed to upgrade websocket", slog.Any("error", err))
		sharedserver.RespondError(c, 400, "failed to upgrade websocket connection", nil)

		return
	}

	defer func() {
		if closeErr := conn.Close(); closeErr != nil {
			h.safeLogger().Warn("failed to close websocket connection", slog.Any("error", closeErr))
		}
	}()

	ctx := c.Request.Context()

	ticker := time.NewTicker(systemStatsStreamInterval)
	defer ticker.Stop()

	// 최초 1회 즉시 전송
	stats, err := h.systemStats.GetCurrentStats(ctx)
	if err != nil {
		h.safeLogger().Error("failed to collect initial system stats", slog.Any("error", err))
		return
	}

	if err := conn.WriteJSON(stats); err != nil {
		h.safeLogger().Warn("failed to write initial system stats", slog.Any("error", err))
		return
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			stats, err := h.systemStats.GetCurrentStats(ctx)
			if err != nil {
				h.safeLogger().Error("failed to collect system stats", slog.Any("error", err))
				return
			}

			if err := conn.WriteJSON(stats); err != nil {
				h.safeLogger().Warn("failed to write system stats", slog.Any("error", err))
				return
			}
		}
	}
}
