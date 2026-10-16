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
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"github.com/kapu/hololive-shared/pkg/constants"
	sharedserver "github.com/kapu/hololive-shared/pkg/server/httpserver"
)

type updateChannelIDRequest struct {
	ChannelID string `json:"channelId" binding:"required,min=1"`
}

type updateMemberNameRequest struct {
	Name string `json:"name" binding:"required,min=1"`
}

func (h *MemberHandler) updateMemberField(c *gin.Context, value string, maxLength int, update func(context.Context, int, string) (MemberMutationResult, error), message string) {
	memberID, ok := h.parsePositiveMemberID(c)
	if !ok {
		return
	}

	value = strings.TrimSpace(value)
	if value == "" || utf8.RuneCountInString(value) > maxLength {
		sharedserver.RespondError(c, http.StatusBadRequest, "invalid request body", nil)

		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), constants.RequestTimeout.AdminRequest)

	defer cancel()

	result, err := update(ctx, memberID, value)
	if err != nil {
		sharedserver.RespondInternalError(h.safeLogger(), c, "Failed to update member", "Failed to update member", err)

		return
	}

	h.respondMemberMutation(c, http.StatusOK, message, result)
}

func (h *MemberHandler) UpdateChannelID(c *gin.Context) {
	if !h.requireMemberDeps(c) {
		return
	}

	var req updateChannelIDRequest

	if err := bindJSON(c, &req); err != nil {
		sharedserver.RespondError(c, http.StatusBadRequest, "invalid request body", nil)

		return
	}

	h.updateMemberField(c, req.ChannelID, 64, h.service.UpdateChannelID, "Channel ID updated successfully")
}

func (h *MemberHandler) UpdateMemberName(c *gin.Context) {
	if !h.requireMemberDeps(c) {
		return
	}

	var req updateMemberNameRequest

	if err := bindJSON(c, &req); err != nil {
		sharedserver.RespondError(c, http.StatusBadRequest, "invalid request body", nil)

		return
	}

	h.updateMemberField(c, req.Name, 200, h.service.UpdateMemberName, "Member name updated successfully")
}
