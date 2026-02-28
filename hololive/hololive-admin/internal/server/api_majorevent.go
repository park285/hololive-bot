package server

import (
	"context"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/kapu/hololive-shared/pkg/service/majorevent"
)

// MajorEventScheduler: 대형 행사 주간 스케줄러 인터페이스
type MajorEventScheduler interface {
	SendWeeklyNotification(ctx context.Context) error
}

// MajorEventMonthlyScheduler: 대형 행사 월간 스케줄러 인터페이스
type MajorEventMonthlyScheduler interface {
	SendMonthlyNotification(ctx context.Context) error
}

// TriggerMajorEventNotification: 대형 행사 주간 알림을 수동으로 트리거합니다
func (h *APIHandler) TriggerMajorEventNotification(c *gin.Context) {
	if h.majorEventScheduler == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "major event scheduler not initialized"})
		return
	}

	if err := h.majorEventScheduler.SendWeeklyNotification(c.Request.Context()); err != nil {
		if errors.Is(err, majorevent.ErrNotificationInProgress) {
			c.JSON(http.StatusConflict, gin.H{"error": "notification already in progress"})
			return
		}
		h.respondInternalError(c, "failed to send notification", "failed to send weekly major event notification", err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "weekly notification sent"})
}

// TriggerMajorEventMonthlyNotification: 대형 행사 월간 알림을 수동으로 트리거합니다
func (h *APIHandler) TriggerMajorEventMonthlyNotification(c *gin.Context) {
	if h.majorEventMonthlyScheduler == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "major event monthly scheduler not initialized"})
		return
	}

	if err := h.majorEventMonthlyScheduler.SendMonthlyNotification(c.Request.Context()); err != nil {
		if errors.Is(err, majorevent.ErrNotificationInProgress) {
			c.JSON(http.StatusConflict, gin.H{"error": "notification already in progress"})
			return
		}
		h.respondInternalError(c, "failed to send notification", "failed to send monthly major event notification", err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "monthly notification sent"})
}
