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
	ctx, cancel := context.WithTimeout(c.Request.Context(), adminRequestTimeout)
	defer cancel()

	members, alarmKeys, memberErr, alarmErr := h.collectStats(ctx)
	if memberErr != nil || alarmErr != nil {
		h.safeLogger().Error("failed to collect stats",
			slog.Any("member_error", memberErr),
			slog.Any("alarm_error", alarmErr))
		sharedserver.RespondError(c, http.StatusInternalServerError, "failed to collect stats", nil)

		return
	}

	_, _, rooms := h.acl.GetACLStatus()

	ginjson.Respond(c, http.StatusOK, statsResponse{
		Status:  "ok",
		Members: members,
		Alarms:  alarmKeys,
		Rooms:   len(rooms),
		Version: health.GetVersion(),
		Uptime:  health.GetUptime(),
	})
}
