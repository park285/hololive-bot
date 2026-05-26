package api

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/kapu/hololive-shared/pkg/constants"
	sharedserver "github.com/kapu/hololive-shared/pkg/server"
	"github.com/kapu/hololive-shared/pkg/util"
)

func (h *MemberHandler) GetCalendar(c *gin.Context) {
	if !h.requireMemberDeps(c) {
		return
	}

	month, year, ok := parseCalendarParams(c)
	if !ok {
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), constants.RequestTimeout.AdminRequest)
	defer cancel()

	entries, err := h.repository.FindMembersWithCelebrationsInMonth(ctx, month, year)
	if err != nil {
		h.safeLogger().Error("Failed to get calendar entries",
			slog.Int("month", month), slog.Int("year", year),
			slog.Any("error", err),
		)
		sharedserver.RespondError(c, http.StatusInternalServerError, "Failed to get calendar", nil)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status": "ok", "month": month, "year": year, "entries": entries,
	})
}

func parseCalendarParams(c *gin.Context) (month, year int, ok bool) {
	now := util.NowKST()
	month = int(now.Month())
	year = now.Year()

	if monthStr := c.Query("month"); monthStr != "" {
		m, err := strconv.Atoi(monthStr)
		if err != nil || m < 1 || m > 12 {
			sharedserver.RespondError(c, http.StatusBadRequest, "Invalid month parameter (1-12)", nil)
			return 0, 0, false
		}
		month = m
	}
	if yearStr := c.Query("year"); yearStr != "" {
		y, err := strconv.Atoi(yearStr)
		if err != nil || y < 2000 || y > 2100 {
			sharedserver.RespondError(c, http.StatusBadRequest, "Invalid year parameter", nil)
			return 0, 0, false
		}
		year = y
	}
	return month, year, true
}
