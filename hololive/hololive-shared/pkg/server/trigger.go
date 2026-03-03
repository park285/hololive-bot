package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	triggercontracts "github.com/kapu/hololive-shared/pkg/contracts/trigger"
)

// MajorEventScheduler: 대형 행사 주간 스케줄러 인터페이스
type MajorEventScheduler interface {
	SendWeeklyNotification(ctx context.Context) error
}

// MajorEventMonthlyScheduler: 대형 행사 월간 스케줄러 인터페이스
type MajorEventMonthlyScheduler interface {
	SendMonthlyNotification(ctx context.Context) error
}

// MemberNewsWeeklyScheduler: 구독 멤버 뉴스 주간 스케줄러 인터페이스
type MemberNewsWeeklyScheduler interface {
	SendWeeklyDigest(ctx context.Context) error
}

// TriggerHandler: 내부 트리거 API 핸들러 (admin-api에서 스케줄러 수동 실행용)
type TriggerHandler struct {
	majorEvent        MajorEventScheduler
	majorEventMonthly MajorEventMonthlyScheduler
	memberNewsWeekly  MemberNewsWeeklyScheduler
	logger            *slog.Logger
}

// NewTriggerHandler: TriggerHandler를 생성합니다.
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

// RegisterInternalRoutes: 내부 트리거 라우트를 등록합니다.
func (h *TriggerHandler) RegisterInternalRoutes(rg *gin.RouterGroup) {
	h.RegisterInternalRoutesWithAuth(rg, "")
}

// RegisterInternalRoutesWithAuth: 내부 트리거 라우트를 등록합니다.
// apiKey가 설정된 경우 X-API-Key 미들웨어를 강제합니다.
func (h *TriggerHandler) RegisterInternalRoutesWithAuth(rg *gin.RouterGroup, apiKey string) {
	internal := rg.Group("/internal/trigger")
	internal.Use(APIKeyAuthMiddleware(apiKey))
	internal.POST("/majorevent-weekly", h.TriggerWeeklyNotification)
	internal.POST("/majorevent-monthly", h.TriggerMonthlyNotification)
	internal.POST("/membernews-weekly", h.TriggerMemberNewsWeekly)
}

// TriggerWeeklyNotification: 대형 행사 주간 알림을 수동으로 트리거합니다.
func (h *TriggerHandler) TriggerWeeklyNotification(c *gin.Context) {
	if h.majorEvent == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "major event scheduler not initialized"})
		return
	}

	if err := h.majorEvent.SendWeeklyNotification(c.Request.Context()); err != nil {
		if errors.Is(err, triggercontracts.ErrNotificationInProgress) {
			c.JSON(http.StatusConflict, gin.H{"error": "notification already in progress"})
			return
		}
		h.logger.Error("Failed to trigger weekly notification", slog.Any("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "weekly notification sent"})
}

// TriggerMonthlyNotification: 대형 행사 월간 알림을 수동으로 트리거합니다.
func (h *TriggerHandler) TriggerMonthlyNotification(c *gin.Context) {
	if h.majorEventMonthly == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "major event monthly scheduler not initialized"})
		return
	}

	if err := h.majorEventMonthly.SendMonthlyNotification(c.Request.Context()); err != nil {
		if errors.Is(err, triggercontracts.ErrNotificationInProgress) {
			c.JSON(http.StatusConflict, gin.H{"error": "notification already in progress"})
			return
		}
		h.logger.Error("Failed to trigger monthly notification", slog.Any("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "monthly notification sent"})
}

// TriggerMemberNewsWeekly: 멤버 뉴스 주간 알림을 수동으로 트리거합니다.
func (h *TriggerHandler) TriggerMemberNewsWeekly(c *gin.Context) {
	if h.memberNewsWeekly == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "member news weekly scheduler not initialized"})
		return
	}

	if err := h.memberNewsWeekly.SendWeeklyDigest(c.Request.Context()); err != nil {
		h.logger.Error("Failed to trigger member news weekly", slog.Any("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "member news weekly digest sent"})
}
