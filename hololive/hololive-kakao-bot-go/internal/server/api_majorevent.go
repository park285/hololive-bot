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
	"net/http"

	"github.com/gin-gonic/gin"
	triggercontracts "github.com/kapu/hololive-shared/pkg/contracts/trigger"
)

// MajorEventScheduler: 대형 행사 주간 스케줄러 인터페이스.
type MajorEventScheduler interface {
	SendWeeklyNotification(ctx context.Context) error
}

// MajorEventMonthlyScheduler: 대형 행사 월간 스케줄러 인터페이스.
type MajorEventMonthlyScheduler interface {
	SendMonthlyNotification(ctx context.Context) error
}

// TriggerMajorEventNotification: 대형 행사 주간 알림을 수동으로 트리거합니다.
func (h *MajorEventAPIHandler) TriggerMajorEventNotification(c *gin.Context) {
	if h.majorEventScheduler == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "major event scheduler not initialized"})
		return
	}

	if err := h.majorEventScheduler.SendWeeklyNotification(c.Request.Context()); err != nil {
		if errors.Is(err, triggercontracts.ErrNotificationInProgress) {
			c.JSON(http.StatusConflict, gin.H{"error": "notification already in progress"})
			return
		}

		h.respondInternalError(c, "failed to send notification", "failed to send weekly major event notification", err)

		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "weekly notification sent"})
}

// TriggerMajorEventMonthlyNotification: 대형 행사 월간 알림을 수동으로 트리거합니다.
func (h *MajorEventAPIHandler) TriggerMajorEventMonthlyNotification(c *gin.Context) {
	if h.majorEventMonthlyScheduler == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "major event monthly scheduler not initialized"})
		return
	}

	if err := h.majorEventMonthlyScheduler.SendMonthlyNotification(c.Request.Context()); err != nil {
		if errors.Is(err, triggercontracts.ErrNotificationInProgress) {
			c.JSON(http.StatusConflict, gin.H{"error": "notification already in progress"})
			return
		}

		h.respondInternalError(c, "failed to send notification", "failed to send monthly major event notification", err)

		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "monthly notification sent"})
}
