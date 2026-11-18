package api

import (
	"context"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

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

	ctx, cancel := context.WithTimeout(c.Request.Context(), adminRequestTimeout)

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
