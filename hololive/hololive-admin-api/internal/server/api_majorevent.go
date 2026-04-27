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
	sharedserver "github.com/kapu/hololive-shared/pkg/server"
)

type MajorEventScheduler interface {
	SendWeeklyNotification(ctx context.Context) error
}

type MajorEventMonthlyScheduler interface {
	SendMonthlyNotification(ctx context.Context) error
}

func (h *MajorEventAPIHandler) TriggerMajorEventNotification(c *gin.Context) {
	if h == nil || h.APIHandler == nil || h.majorEventScheduler == nil {
		sharedserver.RespondError(c, http.StatusServiceUnavailable, "major event scheduler not initialized", nil)
		return
	}

	if err := h.majorEventScheduler.SendWeeklyNotification(c.Request.Context()); err != nil {
		if errors.Is(err, triggercontracts.ErrNotificationInProgress) {
			sharedserver.RespondError(c, http.StatusConflict, "notification already in progress", nil)
			return
		}

		sharedserver.RespondInternalError(h.safeLogger(), c, "failed to send notification", "failed to send weekly major event notification", err)

		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "weekly notification sent"})
}

func (h *MajorEventAPIHandler) TriggerMajorEventMonthlyNotification(c *gin.Context) {
	if h == nil || h.APIHandler == nil || h.majorEventMonthlyScheduler == nil {
		sharedserver.RespondError(c, http.StatusServiceUnavailable, "major event monthly scheduler not initialized", nil)
		return
	}

	if err := h.majorEventMonthlyScheduler.SendMonthlyNotification(c.Request.Context()); err != nil {
		if errors.Is(err, triggercontracts.ErrNotificationInProgress) {
			sharedserver.RespondError(c, http.StatusConflict, "notification already in progress", nil)
			return
		}

		sharedserver.RespondInternalError(h.safeLogger(), c, "failed to send notification", "failed to send monthly major event notification", err)

		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "monthly notification sent"})
}
