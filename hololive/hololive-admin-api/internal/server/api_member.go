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
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
)

const aliasMaxLength = 100

// aliasRequest: 별칭 추가/삭제 요청을 위한 통합 구조체.
type aliasRequest struct {
	Type  string `json:"type" binding:"required,oneof=ko ja"`
	Alias string `json:"alias" binding:"required,min=1"`
}

func normalizeAliasInput(alias string) string {
	return strings.Join(strings.Fields(alias), " ")
}

// handleAliasOperation: 별칭 추가/삭제 작업을 공통 로직으로 처리함.
func (h *MemberAPIHandler) handleAliasOperation(
	c *gin.Context,
	repoFunc func(context.Context, int, string, string) error,
	operationName string,
) {
	memberID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		h.logger.Warn("Invalid member ID", slog.String("id", c.Param("id")), slog.Any("error", err))
		c.JSON(400, gin.H{"error": "Invalid member ID"})

		return
	}

	if memberID <= 0 {
		h.logger.Warn("Member ID must be positive", slog.Int("id", memberID))
		c.JSON(400, gin.H{"error": "Member ID must be positive"})

		return
	}

	var req aliasRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Warn("Invalid request body", slog.Any("error", err))
		c.JSON(400, gin.H{"error": "invalid request body"})

		return
	}

	req.Alias = normalizeAliasInput(req.Alias)
	if req.Alias == "" {
		h.logger.Warn("Alias must not be empty after normalization")
		c.JSON(400, gin.H{"error": "Alias must not be empty"})

		return
	}

	if utf8.RuneCountInString(req.Alias) > aliasMaxLength {
		h.logger.Warn("Alias exceeds max length", slog.Int("max", aliasMaxLength))
		c.JSON(400, gin.H{"error": fmt.Sprintf("Alias must be at most %d characters", aliasMaxLength)})

		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), constants.RequestTimeout.AdminRequest)
	defer cancel()

	if err := repoFunc(ctx, memberID, req.Type, req.Alias); err != nil {
		h.logger.Error("Failed to "+operationName+" alias",
			slog.Int("member_id", memberID),
			slog.String("type", req.Type),
			slog.String("alias", req.Alias),
			slog.Any("error", err),
		)
		c.JSON(500, gin.H{"error": "Failed to " + operationName + " alias"})

		return
	}

	if err := h.memberCache.InvalidateAliasCache(ctx, req.Alias); err != nil {
		h.logger.Error("Failed to invalidate alias cache", slog.Any("error", err))
		c.JSON(500, gin.H{"error": "Failed to synchronize member cache"})

		return
	}

	h.logger.Info("Alias "+operationName,
		slog.Int("member_id", memberID),
		slog.String("type", req.Type),
		slog.String("alias", req.Alias),
	)

	h.activity.Log("member_alias_"+operationName, fmt.Sprintf("Member alias %s: %s (ID: %d)", operationName, req.Alias, memberID), map[string]any{
		"member_id": memberID,
		"type":      req.Type,
		"alias":     req.Alias,
	})

	c.JSON(200, gin.H{
		"status":  "ok",
		"message": "Alias " + operationName + " successfully",
	})
}

func (h *MemberAPIHandler) AddAlias(c *gin.Context) {
	h.handleAliasOperation(c, h.repo.AddAlias, "add")
}

func (h *MemberAPIHandler) RemoveAlias(c *gin.Context) {
	h.handleAliasOperation(c, h.repo.RemoveAlias, "remove")
}

//

func (h *MemberAPIHandler) SetGraduation(c *gin.Context) {
	memberID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		h.logger.Warn("Invalid member ID", slog.String("id", c.Param("id")), slog.Any("error", err))
		c.JSON(400, gin.H{"error": "Invalid member ID"})

		return
	}

	var req struct {
		IsGraduated bool `json:"isGraduated"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Warn("Invalid request body", slog.Any("error", err))
		c.JSON(400, gin.H{"error": "invalid request body"})

		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), constants.RequestTimeout.AdminRequest)
	defer cancel()

	if err := h.repo.SetGraduation(ctx, memberID, req.IsGraduated); err != nil {
		h.logger.Error("Failed to set graduation status",
			slog.Int("member_id", memberID),
			slog.Bool("is_graduated", req.IsGraduated),
			slog.Any("error", err),
		)
		c.JSON(500, gin.H{"error": "Failed to set graduation status"})

		return
	}

	if err := h.memberCache.Refresh(ctx); err != nil {
		h.invalidateMemberIndex()
		h.logger.Error("Failed to refresh cache after graduation update", slog.Any("error", err))
		c.JSON(500, gin.H{"error": "Failed to synchronize member cache"})

		return
	}

	h.invalidateMemberIndex()

	h.logger.Info("Graduation status updated",
		slog.Int("member_id", memberID),
		slog.Bool("is_graduated", req.IsGraduated),
	)

	statusStr := "graduated"

	if !req.IsGraduated {
		statusStr = "active"
	}

	h.activity.Log("member_graduation", fmt.Sprintf("Member status changed to %s (ID: %d)", statusStr, memberID), map[string]any{
		"member_id":    memberID,
		"is_graduated": req.IsGraduated,
	})

	c.JSON(200, gin.H{
		"status":  "ok",
		"message": "Graduation status updated successfully",
	})
}

//
//nolint:dupl // Similar patterns for different update operations
func (h *MemberAPIHandler) UpdateChannelID(c *gin.Context) {
	memberID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		h.logger.Warn("Invalid member ID", slog.String("id", c.Param("id")), slog.Any("error", err))
		c.JSON(400, gin.H{"error": "Invalid member ID"})

		return
	}

	var req struct {
		ChannelID string `json:"channelId" binding:"required,min=1"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Warn("Invalid request body", slog.Any("error", err))
		c.JSON(400, gin.H{"error": "invalid request body"})

		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), constants.RequestTimeout.AdminRequest)
	defer cancel()

	if err := h.repo.UpdateChannelID(ctx, memberID, req.ChannelID); err != nil {
		h.logger.Error("Failed to update channel ID",
			slog.Int("member_id", memberID),
			slog.String("channel_id", req.ChannelID),
			slog.Any("error", err),
		)
		c.JSON(500, gin.H{"error": "Failed to update channel ID"})

		return
	}

	if err := h.memberCache.Refresh(ctx); err != nil {
		h.invalidateMemberIndex()
		h.logger.Error("Failed to refresh cache after channel ID update", slog.Any("error", err))
		c.JSON(500, gin.H{"error": "Failed to synchronize member cache"})

		return
	}

	h.invalidateMemberIndex()

	h.logger.Info("Channel ID updated",
		slog.Int("member_id", memberID),
		slog.String("channel_id", req.ChannelID),
	)

	h.activity.Log("member_channel_update", fmt.Sprintf("Member channel ID updated to %s (ID: %d)", req.ChannelID, memberID), map[string]any{
		"member_id":  memberID,
		"channel_id": req.ChannelID,
	})

	c.JSON(200, gin.H{
		"status":  "ok",
		"message": "Channel ID updated successfully",
	})
}

//
//nolint:dupl // Similar patterns for different update operations
func (h *MemberAPIHandler) UpdateMemberName(c *gin.Context) {
	memberID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		h.logger.Warn("Invalid member ID", slog.String("id", c.Param("id")), slog.Any("error", err))
		c.JSON(400, gin.H{"error": "Invalid member ID"})

		return
	}

	var req struct {
		Name string `json:"name" binding:"required,min=1"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Warn("Invalid request body", slog.Any("error", err))
		c.JSON(400, gin.H{"error": "invalid request body"})

		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), constants.RequestTimeout.AdminRequest)
	defer cancel()

	if err := h.repo.UpdateMemberName(ctx, memberID, req.Name); err != nil {
		h.logger.Error("Failed to update member name",
			slog.Int("member_id", memberID),
			slog.String("name", req.Name),
			slog.Any("error", err),
		)
		c.JSON(500, gin.H{"error": "Failed to update member name"})

		return
	}

	if err := h.memberCache.Refresh(ctx); err != nil {
		h.invalidateMemberIndex()
		h.logger.Error("Failed to refresh cache after member name update", slog.Any("error", err))
		c.JSON(500, gin.H{"error": "Failed to synchronize member cache"})

		return
	}

	h.invalidateMemberIndex()

	h.logger.Info("Member name updated",
		slog.Int("member_id", memberID),
		slog.String("name", req.Name),
	)

	h.activity.Log("member_name_update", fmt.Sprintf("Member name updated to %s (ID: %d)", req.Name, memberID), map[string]any{
		"member_id": memberID,
		"name":      req.Name,
	})

	c.JSON(200, gin.H{
		"status":  "ok",
		"message": "Member name updated successfully",
	})
}

func (h *MemberAPIHandler) GetMembers(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), constants.RequestTimeout.AdminRequest)
	defer cancel()

	members, err := h.repo.GetAllMembers(ctx)
	if err != nil {
		h.logger.Error("Failed to get members", slog.Any("error", err))
		c.JSON(500, gin.H{"error": "Failed to get members"})

		return
	}

	c.JSON(200, gin.H{
		"status":  "ok",
		"members": members,
	})
}

func (h *MemberAPIHandler) AddMember(c *gin.Context) {
	var req domain.Member
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Warn("Invalid request body", slog.Any("error", err))
		c.JSON(400, gin.H{"error": "invalid request body"})

		return
	}

	ctx := c.Request.Context()
	if err := h.repo.CreateMember(ctx, &req); err != nil {
		h.logger.Error("Failed to add member", slog.Any("error", err))
		c.JSON(500, gin.H{"error": "Failed to add member"})

		return
	}

	if err := h.memberCache.Refresh(ctx); err != nil {
		h.invalidateMemberIndex()
		h.logger.Error("Failed to refresh member cache", slog.Any("error", err))
		c.JSON(500, gin.H{"error": "Failed to synchronize member cache"})

		return
	}

	h.invalidateMemberIndex()

	h.activity.Log("member_add", "Member added: "+req.Name, map[string]any{"name": req.Name})

	c.JSON(201, gin.H{"status": "ok", "message": "Member added successfully"})
}
