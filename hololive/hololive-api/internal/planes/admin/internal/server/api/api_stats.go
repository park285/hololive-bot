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
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/park285/shared-go/v2/pkg/ginjson"
	"github.com/park285/shared-go/v2/pkg/panicguard"
	"github.com/park285/shared-go/v2/pkg/runtime/lifecycle"

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/health"
	sharedserver "github.com/kapu/hololive-shared/pkg/server/httpserver"
)

var systemStatsStreamInterval = 5 * time.Second

const systemStatsWriteTimeout = 10 * time.Second

var errSystemStatsStreamStopped = errors.New("system stats stream stopped")

type statsResponse struct {
	Status  string `json:"status"`
	Members int    `json:"members"`
	Alarms  int    `json:"alarms"`
	Rooms   int    `json:"rooms"`
	Version string `json:"version"`
	Uptime  string `json:"uptime"`
}

func (h *StatsHandler) collectStats(ctx context.Context) (members []*domain.Member, alarmKeys []*domain.AlarmEntry, memberErr, alarmErr error) {
	var wg sync.WaitGroup

	wg.Go(func() {
		panicguard.Run(h.safeLogger(), panicguard.BackgroundTask, "admin-stats-members", func() {
			memberErr = panicguard.RunE(h.safeLogger(), panicguard.BackgroundTask, "admin-stats-members", func() error {
				var err error

				members, err = h.repository.GetAllMembers(ctx)
				if err != nil {
					return fmt.Errorf("get all members: %w", err)
				}

				return nil
			})
		})
	})
	wg.Go(func() {
		panicguard.Run(h.safeLogger(), panicguard.BackgroundTask, "admin-stats-alarms", func() {
			alarmErr = panicguard.RunE(h.safeLogger(), panicguard.BackgroundTask, "admin-stats-alarms", func() error {
				var err error

				alarmKeys, err = h.alarm.GetAllAlarmKeys(ctx)
				if err != nil {
					return fmt.Errorf("get all alarm keys: %w", err)
				}

				return nil
			})
		})
	})

	wg.Wait()

	if memberErr != nil {
		memberErr = fmt.Errorf("collect member stats: %w", memberErr)
	}

	if alarmErr != nil {
		alarmErr = fmt.Errorf("collect alarm stats: %w", alarmErr)
	}

	return members, alarmKeys, memberErr, alarmErr
}

func (h *StatsHandler) GetStats(c *gin.Context) {
	if !h.requireStatsDeps(c) {
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), constants.RequestTimeout.AdminRequest)
	defer cancel()

	members, alarmKeys, memberErr, alarmErr := h.collectStats(ctx)
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

	ginjson.Respond(c, 200, statsResponse{
		Status:  "ok",
		Members: len(members),
		Alarms:  len(alarmKeys),
		Rooms:   roomCount,
		Version: health.GetVersion(),
		Uptime:  health.GetUptime(),
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

	if err := lifecycle.RunTickerLoop(ctx, systemStatsStreamInterval, func(ctx context.Context) error {
		if !h.writeSystemStats(ctx, conn, "failed to collect system stats", "failed to write system stats") {
			return errSystemStatsStreamStopped
		}

		return nil
	}); err != nil && !errors.Is(err, errSystemStatsStreamStopped) {
		h.safeLogger().Warn("system stats stream stopped", slog.Any("error", err))
	}
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

	if setErr := conn.SetWriteDeadline(time.Now().Add(systemStatsWriteTimeout)); setErr != nil {
		h.safeLogger().Warn(writeMessage, slog.Any("error", setErr))

		return false
	}

	payload, err := jsonv2.Marshal(stats)
	if err != nil {
		h.safeLogger().Warn(writeMessage, slog.Any("error", err))

		return false
	}

	if err := conn.WriteMessage(websocket.TextMessage, payload); err != nil {
		h.safeLogger().Warn(writeMessage, slog.Any("error", err))

		return false
	}

	return true
}
