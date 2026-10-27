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
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/server/ginjson"
	sharedserver "github.com/kapu/hololive-shared/pkg/server/httpserver"
	"github.com/kapu/hololive-shared/pkg/service/member"
)

type memberListResponse struct {
	Status  string           `json:"status"`
	Members []*domain.Member `json:"members"`
}

func (h *MemberHandler) parsePositiveMemberID(c *gin.Context) (int, bool) {
	memberID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		h.safeLogger().Warn("Invalid member ID", slog.String("id", c.Param("id")), slog.Any("error", err))
		sharedserver.RespondError(c, http.StatusBadRequest, "Invalid member ID", nil)

		return 0, false
	}

	if memberID <= 0 {
		h.safeLogger().Warn("Member ID must be positive", slog.Int("id", memberID))
		sharedserver.RespondError(c, http.StatusBadRequest, "Member ID must be positive", nil)

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
		IsGraduated *bool `json:"isGraduated"`
	}

	if err := bindJSON(c, &req); err != nil {
		h.safeLogger().Warn("Invalid request body", slog.Any("error", err))
		sharedserver.RespondError(c, http.StatusBadRequest, "invalid request body", nil)

		return
	}

	if req.IsGraduated == nil {
		h.safeLogger().Warn("Missing graduation status")
		sharedserver.RespondError(c, http.StatusBadRequest, "isGraduated is required", nil)

		return
	}

	isGraduated := *req.IsGraduated

	ctx, cancel := context.WithTimeout(c.Request.Context(), constants.RequestTimeout.AdminRequest)
	defer cancel()

	result, err := h.service.SetGraduation(ctx, memberID, isGraduated)
	if err != nil {
		h.safeLogger().Error("Failed to set graduation status",
			slog.Int("member_id", memberID),
			slog.Bool("is_graduated", isGraduated),
			slog.Any("error", err),
		)
		sharedserver.RespondError(c, http.StatusInternalServerError, "Failed to set graduation status", nil)

		return
	}

	h.respondMemberMutation(c, http.StatusOK, "Graduation status updated successfully", result)
}

func (h *MemberHandler) respondMemberMutation(c *gin.Context, status int, message string, result MemberMutationResult) {
	if !result.CacheSynced {
		message = "Member changes saved, but cache synchronization failed; cached views may be stale"
	}

	ginjson.Respond(c, status, struct {
		MemberMutationResult

		Status  string `json:"status"`
		Message string `json:"message"`
	}{Status: "ok", Message: message, MemberMutationResult: result})
}

func (h *MemberHandler) GetMembers(c *gin.Context) {
	if !h.requireMemberDeps(c) {
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), constants.RequestTimeout.AdminRequest)
	defer cancel()

	members, err := h.repository.GetAllMembers(ctx)
	if err != nil {
		sharedserver.RespondInternalError(h.safeLogger(), c, "Failed to get members", "Failed to get members", err)

		return
	}

	ginjson.Respond(c, http.StatusOK, memberListResponse{Status: "ok", Members: members})
}

func (h *MemberHandler) AddMember(c *gin.Context) {
	var req CreateMemberRequest

	if err := bindJSON(c, &req); err != nil {
		h.safeLogger().Warn("Invalid request body", slog.Any("error", err))
		sharedserver.RespondError(c, http.StatusBadRequest, "invalid request body", nil)

		return
	}

	m, err := req.member()
	if err != nil {
		sharedserver.RespondError(c, http.StatusBadRequest, err.Error(), nil)

		return
	}

	if !h.requireMemberDeps(c) {
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), constants.RequestTimeout.AdminRequest)
	defer cancel()

	result, err := h.service.create(ctx, m)
	if errors.Is(err, member.ErrCreateConflict) {
		sharedserver.RespondError(c, http.StatusConflict, "Member already exists with different values", nil)

		return
	}

	if err != nil {
		sharedserver.RespondInternalError(h.safeLogger(), c, "Failed to add member", "Failed to add member", err)

		return
	}

	h.respondMemberMutation(c, http.StatusCreated, "Member added successfully", result)
}
