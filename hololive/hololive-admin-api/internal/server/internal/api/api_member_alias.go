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
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/kapu/hololive-shared/pkg/constants"
	sharedserver "github.com/kapu/hololive-shared/pkg/server"
)

const aliasMaxLength = 100

type aliasRequest struct {
	Type  string `json:"type" binding:"required,oneof=ko ja"`
	Alias string `json:"alias" binding:"required,min=1"`
}

func normalizeAliasInput(alias string) string {
	return strings.Join(strings.Fields(alias), " ")
}

func (h *MemberAPIHandler) handleAliasOperation(
	c *gin.Context,
	repoFunc func(context.Context, int, string, string) error,
	operationName string,
) {
	memberID, ok := h.parsePositiveMemberID(c)
	if !ok {
		return
	}

	req, ok := h.bindAliasRequest(c)
	if !ok {
		return
	}

	if h == nil || h.APIHandler == nil || h.memberCache == nil || repoFunc == nil {
		respondServiceUnavailable(c, "member service not available")
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), constants.RequestTimeout.AdminRequest)
	defer cancel()

	if err := repoFunc(ctx, memberID, req.Type, req.Alias); err != nil {
		h.safeLogger().Error("Failed to "+operationName+" alias",
			slog.Int("member_id", memberID),
			slog.String("type", req.Type),
			slog.String("alias", req.Alias),
			slog.Any("error", err),
		)
		sharedserver.RespondError(c, 500, "Failed to "+operationName+" alias", nil)

		return
	}

	if err := h.memberCache.InvalidateAliasCache(ctx, req.Alias); err != nil {
		h.safeLogger().Error("Failed to invalidate alias cache", slog.Any("error", err))
		sharedserver.RespondError(c, 500, "Failed to synchronize member cache", nil)

		return
	}

	h.respondAliasOperationSuccess(c, memberID, req.Type, req.Alias, operationName)
}

func (h *MemberAPIHandler) bindAliasRequest(c *gin.Context) (aliasRequest, bool) {
	var req aliasRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.safeLogger().Warn("Invalid request body", slog.Any("error", err))
		sharedserver.RespondError(c, 400, "invalid request body", nil)

		return aliasRequest{}, false
	}

	req.Alias = normalizeAliasInput(req.Alias)
	if req.Alias == "" {
		h.safeLogger().Warn("Alias must not be empty after normalization")
		sharedserver.RespondError(c, 400, "Alias must not be empty", nil)

		return aliasRequest{}, false
	}

	if utf8.RuneCountInString(req.Alias) > aliasMaxLength {
		h.safeLogger().Warn("Alias exceeds max length", slog.Int("max", aliasMaxLength))
		sharedserver.RespondError(c, 400, fmt.Sprintf("Alias must be at most %d characters", aliasMaxLength), nil)

		return aliasRequest{}, false
	}

	return req, true
}

func (h *MemberAPIHandler) respondAliasOperationSuccess(c *gin.Context, memberID int, aliasType, alias, operationName string) {
	h.safeLogger().Info("Alias "+operationName,
		slog.Int("member_id", memberID),
		slog.String("type", aliasType),
		slog.String("alias", alias),
	)

	h.logActivity("member_alias_"+operationName, fmt.Sprintf("Member alias %s: %s (ID: %d)", operationName, alias, memberID), map[string]any{
		"member_id": memberID,
		"type":      aliasType,
		"alias":     alias,
	})

	c.JSON(200, gin.H{
		"status":  "ok",
		"message": "Alias " + operationName + " successfully",
	})
}

func (h *MemberAPIHandler) AddAlias(c *gin.Context) {
	if h == nil || h.APIHandler == nil || h.repository == nil {
		respondServiceUnavailable(c, "member service not available")
		return
	}

	h.handleAliasOperation(c, h.repository.AddAlias, "add")
}

func (h *MemberAPIHandler) RemoveAlias(c *gin.Context) {
	if h == nil || h.APIHandler == nil || h.repository == nil {
		respondServiceUnavailable(c, "member service not available")
		return
	}

	h.handleAliasOperation(c, h.repository.RemoveAlias, "remove")
}
