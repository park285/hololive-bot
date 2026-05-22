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

package api

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/gin-gonic/gin"
	"github.com/kapu/hololive-shared/pkg/constants"
	sharedserver "github.com/kapu/hololive-shared/pkg/server"
)

type updateChannelIDRequest struct {
	ChannelID string `json:"channelId" binding:"required,min=1"`
}

type updateMemberNameRequest struct {
	Name string `json:"name" binding:"required,min=1"`
}

type memberFieldUpdateSpec[T any] struct {
	value             func(T) string
	update            func(context.Context, *MemberAPIHandler, int, T) error
	logFieldKey       string
	repoErrorLog      string
	repoErrorResponse string
	cacheErrorLog     string
	successLog        string
	activityType      string
	activityMessage   func(string, int) string
	activityValueKey  string
	successMessage    string
}

func updateMemberField[T any](h *MemberAPIHandler, c *gin.Context, spec memberFieldUpdateSpec[T]) {
	if !h.requireMemberDeps(c) {
		return
	}

	memberID, ok := h.parsePositiveMemberID(c)
	if !ok {
		return
	}

	var req T
	if err := c.ShouldBindJSON(&req); err != nil {
		h.safeLogger().Warn("Invalid request body", slog.Any("error", err))
		sharedserver.RespondError(c, 400, "invalid request body", nil)

		return
	}

	value := spec.value(req)
	ctx, cancel := context.WithTimeout(c.Request.Context(), constants.RequestTimeout.AdminRequest)
	defer cancel()

	if err := spec.update(ctx, h, memberID, req); err != nil {
		h.safeLogger().Error(spec.repoErrorLog,
			slog.Int("member_id", memberID),
			slog.String(spec.logFieldKey, value),
			slog.Any("error", err),
		)
		sharedserver.RespondError(c, 500, spec.repoErrorResponse, nil)

		return
	}

	if err := h.memberCache.Refresh(ctx); err != nil {
		h.invalidateMemberIndex()
		h.safeLogger().Error(spec.cacheErrorLog, slog.Any("error", err))
		sharedserver.RespondError(c, 500, "Failed to synchronize member cache", nil)

		return
	}

	h.invalidateMemberIndex()

	h.safeLogger().Info(spec.successLog,
		slog.Int("member_id", memberID),
		slog.String(spec.logFieldKey, value),
	)

	h.logActivity(spec.activityType, spec.activityMessage(value, memberID), map[string]any{
		"member_id":           memberID,
		spec.activityValueKey: value,
	})

	c.JSON(200, gin.H{
		"status":  "ok",
		"message": spec.successMessage,
	})
}

func (h *MemberAPIHandler) UpdateChannelID(c *gin.Context) {
	updateMemberField(h, c, memberFieldUpdateSpec[updateChannelIDRequest]{
		value: func(req updateChannelIDRequest) string {
			return req.ChannelID
		},
		update: func(ctx context.Context, h *MemberAPIHandler, memberID int, req updateChannelIDRequest) error {
			return h.repository.UpdateChannelID(ctx, memberID, req.ChannelID)
		},
		logFieldKey:       "channel_id",
		repoErrorLog:      "Failed to update channel ID",
		repoErrorResponse: "Failed to update channel ID",
		cacheErrorLog:     "Failed to refresh cache after channel ID update",
		successLog:        "Channel ID updated",
		activityType:      "member_channel_update",
		activityMessage: func(value string, memberID int) string {
			return fmt.Sprintf("Member channel ID updated to %s (ID: %d)", value, memberID)
		},
		activityValueKey: "channel_id",
		successMessage:   "Channel ID updated successfully",
	})
}

func (h *MemberAPIHandler) UpdateMemberName(c *gin.Context) {
	updateMemberField(h, c, memberFieldUpdateSpec[updateMemberNameRequest]{
		value: func(req updateMemberNameRequest) string {
			return req.Name
		},
		update: func(ctx context.Context, h *MemberAPIHandler, memberID int, req updateMemberNameRequest) error {
			return h.repository.UpdateMemberName(ctx, memberID, req.Name)
		},
		logFieldKey:       "name",
		repoErrorLog:      "Failed to update member name",
		repoErrorResponse: "Failed to update member name",
		cacheErrorLog:     "Failed to refresh cache after member name update",
		successLog:        "Member name updated",
		activityType:      "member_name_update",
		activityMessage: func(value string, memberID int) string {
			return fmt.Sprintf("Member name updated to %s (ID: %d)", value, memberID)
		},
		activityValueKey: "name",
		successMessage:   "Member name updated successfully",
	})
}
