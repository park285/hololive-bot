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

package app

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	membernewssvc "github.com/kapu/hololive-llm-sched/internal/service/membernews"

	membernewscontracts "github.com/kapu/hololive-shared/pkg/contracts/membernews"
	"github.com/kapu/hololive-shared/pkg/contracts/subscription"
	sharedserver "github.com/kapu/hololive-shared/pkg/server"
	"github.com/kapu/hololive-shared/pkg/server/middleware"
)

type memberNewsDigestRequest struct {
	RoomID string `json:"room_id"`
	Period string `json:"period"`
}

func registerMemberNewsInternalRoutes(router *gin.Engine, apiKey string, svc *membernewssvc.Service) {
	if router == nil || svc == nil {
		return
	}

	rg := router.Group(membernewscontracts.BasePath)
	rg.Use(middleware.APIKeyAuthMiddleware(apiKey))

	rg.GET(membernewscontracts.SubscriptionsRoute+"/:roomID", func(c *gin.Context) {
		roomID := strings.TrimSpace(c.Param("roomID"))
		if roomID == "" {
			sharedserver.RespondError(c, http.StatusBadRequest, "room_id_required", nil)
			return
		}

		subscribed, err := svc.IsRoomSubscribed(c.Request.Context(), roomID)
		if err != nil {
			sharedserver.RespondError(c, http.StatusInternalServerError, "subscription_check_failed", nil)
			return
		}

		c.JSON(http.StatusOK, subscription.SubscriptionStatusResponse{Subscribed: subscribed})
	})

	rg.POST(membernewscontracts.SubscriptionsRoute, func(c *gin.Context) {
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

		if err := svc.SubscribeRoom(c.Request.Context(), req.RoomID, req.RoomName); err != nil {
			sharedserver.RespondError(c, http.StatusInternalServerError, "subscribe_failed", nil)
			return
		}

		c.JSON(http.StatusOK, gin.H{"status": "subscribed"})
	})

	rg.DELETE(membernewscontracts.SubscriptionsRoute+"/:roomID", func(c *gin.Context) {
		roomID := strings.TrimSpace(c.Param("roomID"))
		if roomID == "" {
			sharedserver.RespondError(c, http.StatusBadRequest, "room_id_required", nil)
			return
		}

		if err := svc.UnsubscribeRoom(c.Request.Context(), roomID); err != nil {
			sharedserver.RespondError(c, http.StatusInternalServerError, "unsubscribe_failed", nil)
			return
		}

		c.JSON(http.StatusOK, gin.H{"status": "unsubscribed"})
	})

	rg.POST(membernewscontracts.DigestRoute, func(c *gin.Context) {
		var req memberNewsDigestRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			sharedserver.RespondError(c, http.StatusBadRequest, "invalid_request", nil)
			return
		}

		req.RoomID = strings.TrimSpace(req.RoomID)
		if req.RoomID == "" {
			sharedserver.RespondError(c, http.StatusBadRequest, "room_id_required", nil)
			return
		}

		period := membernewscontracts.NormalizePeriod(membernewscontracts.Period(req.Period))

		digest, err := svc.GenerateRoomDigest(c.Request.Context(), req.RoomID, membernewssvc.Period(period))
		if err != nil {
			if errors.Is(err, membernewssvc.ErrNoSubscribedMembers) {
				sharedserver.RespondError(c, http.StatusNotFound, "no_subscribed_members", nil)
				return
			}
			sharedserver.RespondError(c, http.StatusInternalServerError, "digest_generation_failed", nil)
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
