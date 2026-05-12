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
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	triggercontracts "github.com/kapu/hololive-shared/pkg/contracts/trigger"
	"github.com/kapu/hololive-shared/pkg/server/middleware"
)

type MajorEventScheduler interface {
	SendWeeklyNotification(ctx context.Context) error
}

type MajorEventMonthlyScheduler interface {
	SendMonthlyNotification(ctx context.Context) error
}

type MemberNewsWeeklyScheduler interface {
	SendWeeklyDigest(ctx context.Context) error
}

type TriggerHandler struct {
	majorEvent        MajorEventScheduler
	majorEventMonthly MajorEventMonthlyScheduler
	memberNewsWeekly  MemberNewsWeeklyScheduler
	logger            *slog.Logger
}

func NewTriggerHandler(
	majorEvent MajorEventScheduler,
	majorEventMonthly MajorEventMonthlyScheduler,
	memberNewsWeekly MemberNewsWeeklyScheduler,
	logger *slog.Logger,
) *TriggerHandler {
	return &TriggerHandler{
		majorEvent:        majorEvent,
		majorEventMonthly: majorEventMonthly,
		memberNewsWeekly:  memberNewsWeekly,
		logger:            logger,
	}
}

func (h *TriggerHandler) RegisterInternalRoutes(rg *gin.RouterGroup) {
	h.RegisterInternalRoutesWithAuth(rg, "")
}

// apiKey가 설정된 경우 X-API-Key 미들웨어를 강제합니다.
func (h *TriggerHandler) RegisterInternalRoutesWithAuth(rg *gin.RouterGroup, apiKey string) {
	internal := rg.Group(triggercontracts.BasePath)
	internal.Use(middleware.APIKeyAuthMiddleware(apiKey))
	internal.POST(triggercontracts.MajorEventWeeklyRoute, h.TriggerWeeklyNotification)
	internal.POST(triggercontracts.MajorEventMonthlyRoute, h.TriggerMonthlyNotification)
	internal.POST(triggercontracts.MemberNewsWeeklyRoute, h.TriggerMemberNewsWeekly)
}

func (h *TriggerHandler) TriggerWeeklyNotification(c *gin.Context) {
	if h.majorEvent == nil {
		RespondError(c, http.StatusServiceUnavailable, "major_event_scheduler_unavailable", gin.H{"message": "major event scheduler not initialized"})
		return
	}

	if err := h.majorEvent.SendWeeklyNotification(c.Request.Context()); err != nil {
		if errors.Is(err, triggercontracts.ErrNotificationInProgress) {
			RespondError(c, http.StatusConflict, "notification_in_progress", gin.H{"message": "notification already in progress"})
			return
		}
		RespondInternalError(
			h.logger,
			c,
			"internal_server_error",
			"Failed to trigger weekly notification",
			err,
		)
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "weekly notification sent"})
}

func (h *TriggerHandler) TriggerMonthlyNotification(c *gin.Context) {
	if h.majorEventMonthly == nil {
		RespondError(c, http.StatusServiceUnavailable, "major_event_monthly_scheduler_unavailable", gin.H{"message": "major event monthly scheduler not initialized"})
		return
	}

	if err := h.majorEventMonthly.SendMonthlyNotification(c.Request.Context()); err != nil {
		if errors.Is(err, triggercontracts.ErrNotificationInProgress) {
			RespondError(c, http.StatusConflict, "notification_in_progress", gin.H{"message": "notification already in progress"})
			return
		}
		RespondInternalError(
			h.logger,
			c,
			"internal_server_error",
			"Failed to trigger monthly notification",
			err,
		)
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "monthly notification sent"})
}

func (h *TriggerHandler) TriggerMemberNewsWeekly(c *gin.Context) {
	if h.memberNewsWeekly == nil {
		RespondError(c, http.StatusServiceUnavailable, "member_news_weekly_scheduler_unavailable", gin.H{"message": "member news weekly scheduler not initialized"})
		return
	}

	if err := h.memberNewsWeekly.SendWeeklyDigest(c.Request.Context()); err != nil {
		RespondInternalError(
			h.logger,
			c,
			"internal_server_error",
			"Failed to trigger member news weekly",
			err,
		)
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "member news weekly digest sent"})
}
