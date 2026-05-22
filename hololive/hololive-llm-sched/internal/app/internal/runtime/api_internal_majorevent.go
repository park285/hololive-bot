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

package runtime

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/kapu/hololive-llm-sched/internal/service/majorevent"

	majoreventcontracts "github.com/kapu/hololive-shared/pkg/contracts/majorevent"
	"github.com/kapu/hololive-shared/pkg/contracts/subscription"
	sharedserver "github.com/kapu/hololive-shared/pkg/server"
	"github.com/kapu/hololive-shared/pkg/server/middleware"
)

func registerMajorEventInternalRoutes(router *gin.Engine, apiKey string, repository *majorevent.Repository) {
	if router == nil || repository == nil {
		return
	}

	rg := router.Group(majoreventcontracts.BasePath)
	rg.Use(middleware.APIKeyAuthMiddleware(apiKey))
	rg.GET(majoreventcontracts.SubscriptionsRoute+"/:roomID", getMajorEventSubscriptionHandler(repository))
	rg.POST(majoreventcontracts.SubscriptionsRoute, subscribeMajorEventHandler(repository))
	rg.DELETE(majoreventcontracts.SubscriptionsRoute+"/:roomID", unsubscribeMajorEventHandler(repository))
}

func getMajorEventSubscriptionHandler(repository *majorevent.Repository) gin.HandlerFunc {
	return func(c *gin.Context) {
		roomID := strings.TrimSpace(c.Param("roomID"))
		if roomID == "" {
			sharedserver.RespondError(c, http.StatusBadRequest, "room_id_required", nil)
			return
		}

		subscribed, err := repository.IsSubscribed(c.Request.Context(), roomID)
		if err != nil {
			sharedserver.RespondError(c, http.StatusInternalServerError, "subscription_check_failed", nil)
			return
		}

		c.JSON(http.StatusOK, subscription.SubscriptionStatusResponse{Subscribed: subscribed})
	}
}

func subscribeMajorEventHandler(repository *majorevent.Repository) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req subscription.SubscribeRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			sharedserver.RespondError(c, http.StatusBadRequest, "invalid_request", nil)
			return
		}
		req.RoomID = strings.TrimSpace(req.RoomID)
		req.RoomName = strings.TrimSpace(req.RoomName)
		if req.RoomID == "" {
			sharedserver.RespondError(c, http.StatusBadRequest, "room_id_required", nil)
			return
		}

		if err := repository.Subscribe(c.Request.Context(), req.RoomID, req.RoomName); err != nil {
			sharedserver.RespondError(c, http.StatusInternalServerError, "subscribe_failed", nil)
			return
		}

		c.JSON(http.StatusOK, gin.H{"status": "subscribed"})
	}
}

func unsubscribeMajorEventHandler(repository *majorevent.Repository) gin.HandlerFunc {
	return func(c *gin.Context) {
		roomID := strings.TrimSpace(c.Param("roomID"))
		if roomID == "" {
			sharedserver.RespondError(c, http.StatusBadRequest, "room_id_required", nil)
			return
		}

		if err := repository.Unsubscribe(c.Request.Context(), roomID); err != nil {
			sharedserver.RespondError(c, http.StatusInternalServerError, "unsubscribe_failed", nil)
			return
		}

		c.JSON(http.StatusOK, gin.H{"status": "unsubscribed"})
	}
}
