package app

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/kapu/hololive-llm-sched/internal/service/majorevent"

	majoreventcontracts "github.com/kapu/hololive-shared/pkg/contracts/majorevent"
	"github.com/kapu/hololive-shared/pkg/contracts/subscription"
	"github.com/kapu/hololive-shared/pkg/server/middleware"
)

func registerMajorEventInternalRoutes(router *gin.Engine, apiKey string, repo *majorevent.Repository) {
	if router == nil || repo == nil {
		return
	}

	rg := router.Group(majoreventcontracts.BasePath)
	rg.Use(middleware.APIKeyAuthMiddleware(apiKey))

	rg.GET(majoreventcontracts.SubscriptionsRoute+"/:roomID", func(c *gin.Context) {
		roomID := strings.TrimSpace(c.Param("roomID"))
		if roomID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "room_id_required"})
			return
		}

		subscribed, err := repo.IsSubscribed(c.Request.Context(), roomID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "subscription_check_failed"})
			return
		}

		c.JSON(http.StatusOK, subscription.SubscriptionStatusResponse{Subscribed: subscribed})
	})

	rg.POST(majoreventcontracts.SubscriptionsRoute, func(c *gin.Context) {
		var req subscription.SubscribeRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request"})
			return
		}
		req.RoomID = strings.TrimSpace(req.RoomID)
		req.RoomName = strings.TrimSpace(req.RoomName)
		if req.RoomID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "room_id_required"})
			return
		}

		if err := repo.Subscribe(c.Request.Context(), req.RoomID, req.RoomName); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "subscribe_failed"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"status": "subscribed"})
	})

	rg.DELETE(majoreventcontracts.SubscriptionsRoute+"/:roomID", func(c *gin.Context) {
		roomID := strings.TrimSpace(c.Param("roomID"))
		if roomID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "room_id_required"})
			return
		}

		if err := repo.Unsubscribe(c.Request.Context(), roomID); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "unsubscribe_failed"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"status": "unsubscribed"})
	})
}
