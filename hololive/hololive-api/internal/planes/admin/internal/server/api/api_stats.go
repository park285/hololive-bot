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
	"fmt"
	"log/slog"
	"net/http"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/park285/shared-go/v2/pkg/health"
	"github.com/park285/shared-go/v2/pkg/panicguard"

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/panicscope"
	"github.com/kapu/hololive-shared/pkg/server/ginjson"
	sharedserver "github.com/kapu/hololive-shared/pkg/server/httpserver"
)

type statsResponse struct {
	Status  string `json:"status"`
	Members int    `json:"members"`
	Alarms  int    `json:"alarms"`
	Rooms   int    `json:"rooms"`
	Version string `json:"version"`
	Uptime  string `json:"uptime"`
}

func (h *StatsHandler) collectStats(ctx context.Context) (members, alarmKeys int, memberErr, alarmErr error) {
	var wg sync.WaitGroup

	wg.Go(func() {
		memberErr = panicguard.RunE(h.safeLogger(), panicscope.BackgroundTask, "admin-stats-members", func() error {
			var err error

			members, err = h.repository.CountMembers(ctx)
			if err != nil {
				return fmt.Errorf("count members: %w", err)
			}

			return nil
		})
	})
	wg.Go(func() {
		alarmErr = panicguard.RunE(h.safeLogger(), panicscope.BackgroundTask, "admin-stats-alarms", func() error {
			var err error

			alarmKeys, err = h.alarm.CountAlarmEntries(ctx)
			if err != nil {
				return fmt.Errorf("count alarm entries: %w", err)
			}

			return nil
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
		sharedserver.RespondError(c, http.StatusInternalServerError, "failed to collect stats", nil)

		return
	}

	// ACL 서비스에서 rooms 수 조회
	var roomCount int

	if h.acl != nil {
		_, _, rooms := h.acl.GetACLStatus()

		roomCount = len(rooms)
	}

	ginjson.Respond(c, http.StatusOK, statsResponse{
		Status:  "ok",
		Members: members,
		Alarms:  alarmKeys,
		Rooms:   roomCount,
		Version: health.GetVersion(),
		Uptime:  health.GetUptime(),
	})
}
