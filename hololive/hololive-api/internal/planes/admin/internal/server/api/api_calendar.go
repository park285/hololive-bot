package api

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
	sharedserver "github.com/kapu/hololive-shared/pkg/server"
	"github.com/kapu/hololive-shared/pkg/util"
	"github.com/park285/shared-go/pkg/ginjson"
)

type calendarResponse struct {
	Status  string                 `json:"status"`
	Month   int                    `json:"month"`
	Year    int                    `json:"year"`
	Entries []domain.CalendarEntry `json:"entries"`
}

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

	result, err := h.repository.FindMembersWithCelebrationsInMonth(ctx, month, year)
	if err != nil {
		h.safeLogger().Error("Failed to get calendar entries",
			slog.Int("month", month), slog.Int("year", year),
			slog.Any("error", err),
		)
		sharedserver.RespondError(c, http.StatusInternalServerError, "Failed to get calendar", nil)
		return
	}

	entries := result
	if entries == nil {
		entries = []domain.CalendarEntry{}
	}
	ginjson.Respond(c, http.StatusOK, calendarResponse{
		Status:  "ok",
		Month:   month,
		Year:    year,
		Entries: entries,
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

func parseIntQuery(c *gin.Context, key string, minValue, maxValue int) (int, error) {
	s := c.Query(key)
	if s == "" {
		return 0, nil
	}
	v, err := strconv.Atoi(s)
	if err != nil || v < minValue || v > maxValue {
		return 0, fmt.Errorf("invalid %s", key)
	}
	return v, nil
}
