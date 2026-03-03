package app

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	membernewssvc "github.com/kapu/hololive-llm-sched/internal/service/membernews"

	membernewscontracts "github.com/kapu/hololive-shared/pkg/contracts/membernews"
	sharedserver "github.com/kapu/hololive-shared/pkg/server"
)

type memberNewsSubscribeRequest struct {
	RoomID   string `json:"room_id"`
	RoomName string `json:"room_name"`
}

type memberNewsDigestRequest struct {
	RoomID string `json:"room_id"`
	Period string `json:"period"`
}

func registerMemberNewsInternalRoutes(router *gin.Engine, apiKey string, svc *membernewssvc.Service) {
	if router == nil || svc == nil {
		return
	}

	rg := router.Group(membernewscontracts.BasePath)
	rg.Use(sharedserver.APIKeyAuthMiddleware(apiKey))

	rg.GET(membernewscontracts.SubscriptionsRoute+"/:roomID", func(c *gin.Context) {
		roomID := strings.TrimSpace(c.Param("roomID"))
		if roomID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "room_id_required"})
			return
		}

		subscribed, err := svc.IsRoomSubscribed(c.Request.Context(), roomID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "subscription_check_failed"})
			return
		}

		c.JSON(http.StatusOK, subscriptionStatusResponse{Subscribed: subscribed})
	})

	rg.POST(membernewscontracts.SubscriptionsRoute, func(c *gin.Context) {
		var req memberNewsSubscribeRequest
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

		if err := svc.SubscribeRoom(c.Request.Context(), req.RoomID, req.RoomName); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "subscribe_failed"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"status": "subscribed"})
	})

	rg.DELETE(membernewscontracts.SubscriptionsRoute+"/:roomID", func(c *gin.Context) {
		roomID := strings.TrimSpace(c.Param("roomID"))
		if roomID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "room_id_required"})
			return
		}

		if err := svc.UnsubscribeRoom(c.Request.Context(), roomID); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "unsubscribe_failed"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"status": "unsubscribed"})
	})

	rg.POST(membernewscontracts.DigestRoute, func(c *gin.Context) {
		var req memberNewsDigestRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request"})
			return
		}

		req.RoomID = strings.TrimSpace(req.RoomID)
		if req.RoomID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "room_id_required"})
			return
		}

		period := membernewscontracts.NormalizePeriod(membernewscontracts.Period(req.Period))

		digest, err := svc.GenerateRoomDigest(c.Request.Context(), req.RoomID, membernewssvc.Period(period))
		if err != nil {
			if errors.Is(err, membernewssvc.ErrNoSubscribedMembers) {
				c.JSON(http.StatusNotFound, gin.H{"error": "no_subscribed_members"})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": "digest_generation_failed"})
			return
		}

		c.JSON(http.StatusOK, convertMemberNewsDigest(digest))
	})
}

func convertMemberNewsDigest(digest *membernewssvc.Digest) *membernewscontracts.Digest {
	if digest == nil {
		return nil
	}

	items := make([]membernewscontracts.SummaryItem, 0, len(digest.TopItems))
	for _, item := range digest.TopItems {
		items = append(items, membernewscontracts.SummaryItem{
			Member:    item.Member,
			Category:  item.Category,
			Title:     item.Title,
			DateText:  item.DateText,
			Summary:   item.Summary,
			SourceURL: item.SourceURL,
		})
	}

	return &membernewscontracts.Digest{
		Period:       membernewscontracts.Period(digest.Period),
		Headline:     digest.Headline,
		TopItems:     items,
		MoreSummary:  digest.MoreSummary,
		OmittedCount: digest.OmittedCount,
		TotalCount:   digest.TotalCount,
	}
}
