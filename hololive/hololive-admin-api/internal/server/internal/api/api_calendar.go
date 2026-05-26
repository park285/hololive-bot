package api

import (
	"context"
	"fmt"
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

	if m, err := parseIntQuery(c, "month", 1, 12); err != nil {
		sharedserver.RespondError(c, http.StatusBadRequest, "Invalid month parameter (1-12)", nil)
		return 0, 0, false
	} else if m > 0 {
		month = m
	}
	if y, err := parseIntQuery(c, "year", 2000, 2100); err != nil {
		sharedserver.RespondError(c, http.StatusBadRequest, "Invalid year parameter", nil)
		return 0, 0, false
	} else if y > 0 {
		year = y
	}
	return month, year, true
}

func parseIntQuery(c *gin.Context, key string, min, max int) (int, error) {
	s := c.Query(key)
	if s == "" {
		return 0, nil
	}
	v, err := strconv.Atoi(s)
	if err != nil || v < min || v > max {
		return 0, fmt.Errorf("invalid %s", key)
	}
	return v, nil
}
