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
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
	sharedserver "github.com/kapu/hololive-shared/pkg/server"
)

func (h *MemberHandler) parsePositiveMemberID(c *gin.Context) (int, bool) {
	memberID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		h.safeLogger().Warn("Invalid member ID", slog.String("id", c.Param("id")), slog.Any("error", err))
		sharedserver.RespondError(c, 400, "Invalid member ID", nil)

		return 0, false
	}

	if memberID <= 0 {
		h.safeLogger().Warn("Member ID must be positive", slog.Int("id", memberID))
		sharedserver.RespondError(c, 400, "Member ID must be positive", nil)

		return 0, false
	}

	return memberID, true
}

func (h *MemberHandler) SetGraduation(c *gin.Context) {
	if !h.requireMemberDeps(c) {
		return
	}

	memberID, ok := h.parsePositiveMemberID(c)
	if !ok {
		return
	}

	var req struct {
		IsGraduated bool `json:"isGraduated"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		h.safeLogger().Warn("Invalid request body", slog.Any("error", err))
		sharedserver.RespondError(c, 400, "invalid request body", nil)

		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), constants.RequestTimeout.AdminRequest)
	defer cancel()

	if err := h.repository.SetGraduation(ctx, memberID, req.IsGraduated); err != nil {
		h.safeLogger().Error("Failed to set graduation status",
			slog.Int("member_id", memberID),
			slog.Bool("is_graduated", req.IsGraduated),
			slog.Any("error", err),
		)
		sharedserver.RespondError(c, 500, "Failed to set graduation status", nil)

		return
	}

	if err := h.memberCache.Refresh(ctx); err != nil {
		h.invalidateMemberIndex()
		h.safeLogger().Error("Failed to refresh cache after graduation update", slog.Any("error", err))
		sharedserver.RespondError(c, 500, "Failed to synchronize member cache", nil)

		return
	}

	h.invalidateMemberIndex()

	h.respondGraduationSuccess(c, memberID, req.IsGraduated)
}

func (h *MemberHandler) respondGraduationSuccess(c *gin.Context, memberID int, isGraduated bool) {
	h.safeLogger().Info("Graduation status updated",
		slog.Int("member_id", memberID),
		slog.Bool("is_graduated", isGraduated),
	)

	statusStr := "graduated"

	if !isGraduated {
		statusStr = "active"
	}

	h.logActivity("member_graduation", fmt.Sprintf("Member status changed to %s (ID: %d)", statusStr, memberID), map[string]any{
		"member_id":    memberID,
		"is_graduated": isGraduated,
	})

	c.JSON(200, gin.H{
		"status":  "ok",
		"message": "Graduation status updated successfully",
	})
}

func (h *MemberHandler) GetMembers(c *gin.Context) {
	if !h.requireMemberDeps(c) {
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), constants.RequestTimeout.AdminRequest)
	defer cancel()

	members, err := h.repository.GetAllMembers(ctx)
	if err != nil {
		h.safeLogger().Error("Failed to get members", slog.Any("error", err))
		sharedserver.RespondError(c, 500, "Failed to get members", nil)

		return
	}

	c.JSON(200, gin.H{
		"status":  "ok",
		"members": members,
	})
}

func (h *MemberHandler) AddMember(c *gin.Context) {
	var req domain.Member
	if err := c.ShouldBindJSON(&req); err != nil {
		h.safeLogger().Warn("Invalid request body", slog.Any("error", err))
		sharedserver.RespondError(c, 400, "invalid request body", nil)

		return
	}

	if !h.requireMemberDeps(c) {
		return
	}

	ctx := c.Request.Context()
	if err := h.repository.CreateMember(ctx, &req); err != nil {
		h.safeLogger().Error("Failed to add member", slog.Any("error", err))
		sharedserver.RespondError(c, 500, "Failed to add member", nil)

		return
	}

	if err := h.memberCache.Refresh(ctx); err != nil {
		h.invalidateMemberIndex()
		h.safeLogger().Error("Failed to refresh member cache", slog.Any("error", err))
		sharedserver.RespondError(c, 500, "Failed to synchronize member cache", nil)

		return
	}

	h.invalidateMemberIndex()

	h.logActivity("member_add", "Member added: "+req.Name, map[string]any{"name": req.Name})

	c.JSON(201, gin.H{"status": "ok", "message": "Member added successfully"})
}
