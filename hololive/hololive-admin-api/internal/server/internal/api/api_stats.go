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

package api

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/health"
	sharedserver "github.com/kapu/hololive-shared/pkg/server"
	"github.com/park285/shared-go/pkg/runtime/loop"
)

var systemStatsStreamInterval = 5 * time.Second

var errSystemStatsStreamStopped = errors.New("system stats stream stopped")

func (h *StatsHandler) GetStats(c *gin.Context) {
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

		members, memberErr = h.repository.GetAllMembers(ctx)
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
func (h *StatsHandler) StreamSystemStats(c *gin.Context) {
	if h == nil || h.Handler == nil || h.systemStats == nil {
		sharedserver.RespondError(c, 400, "System stats collector not available", nil)

		return
	}

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
	h.streamSystemStats(ctx, conn)
}

func (h *StatsHandler) streamSystemStats(ctx context.Context, conn *websocket.Conn) {
	if !h.writeInitialSystemStats(ctx, conn) {
		return
	}

	_ = loop.RunTickerLoop(ctx, systemStatsStreamInterval, func(ctx context.Context) error {
		if !h.writeSystemStats(ctx, conn, "failed to collect system stats", "failed to write system stats") {
			return errSystemStatsStreamStopped
		}
		return nil
	})
}

func (h *StatsHandler) writeInitialSystemStats(ctx context.Context, conn *websocket.Conn) bool {
	return h.writeSystemStats(ctx, conn, "failed to collect initial system stats", "failed to write initial system stats")
}

func (h *StatsHandler) writeSystemStats(
	ctx context.Context,
	conn *websocket.Conn,
	collectMessage string,
	writeMessage string,
) bool {
	stats, err := h.systemStats.GetCurrentStats(ctx)
	if err != nil {
		h.safeLogger().Error(collectMessage, slog.Any("error", err))
		return false
	}

	if err := conn.WriteJSON(stats); err != nil {
		h.safeLogger().Warn(writeMessage, slog.Any("error", err))
		return false
	}

	return true
}
