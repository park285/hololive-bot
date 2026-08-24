package api

import (
	"context"
	"log/slog"

	"github.com/gin-gonic/gin"
	"github.com/park285/shared-go/v2/pkg/ginjson"

	"github.com/kapu/hololive-shared/pkg/constants"
	contractssettings "github.com/kapu/hololive-shared/pkg/contracts/settings"
	sharedserver "github.com/kapu/hololive-shared/pkg/server/httpserver"
)

func (h *SettingsHandler) UpdateLLMSettings(c *gin.Context) {
	req, ok := h.bindUpdateLLMSettingsRequest(c)
	if !ok {
		return
	}

	if !h.requireApplier(c) {
		return
	}

	if !req.validate(c) {
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), constants.RequestTimeout.AdminRequest)
	defer cancel()

	runtime := map[string]any{}

	if req.MemberNewsWeeklyRunNow != nil && *req.MemberNewsWeeklyRunNow {
		memberNewsResult := h.ApplyMemberNewsWeeklyRunNow(ctx)

		runtime[contractssettings.UpdateTypeMemberNewsRunNow] = memberNewsResult.AsMap()
	}

	h.logActivity("llm_settings_update", "LLM settings updated", map[string]any{
		contractssettings.UpdateTypeMemberNewsRunNow: req.MemberNewsWeeklyRunNow,
		"runtime": runtime,
	})

	ginjson.Respond(c, 200, llmSettingsResponse{
		Status:  "ok",
		Message: "LLM settings updated",
		Runtime: runtime,
	})
}

func (h *SettingsHandler) bindUpdateLLMSettingsRequest(c *gin.Context) (updateLLMSettingsRequest, bool) {
	var req updateLLMSettingsRequest

	if err := bindJSON(c, &req); err != nil {
		h.safeLogger().Warn("Invalid request body", slog.Any("error", err))
		sharedserver.RespondError(c, 400, "invalid request body", nil)

		return req, false
	}

	return req, true
}

func (req updateLLMSettingsRequest) validate(c *gin.Context) bool {
	if req.MemberNewsWeeklyRunNow == nil {
		sharedserver.RespondError(c, 400, "at least one llm setting field is required", nil)

		return false
	}

	if req.MemberNewsWeeklyRunNow != nil && !*req.MemberNewsWeeklyRunNow {
		sharedserver.RespondError(c, 400, "memberNewsWeeklyRunNow must be true when provided", nil)

		return false
	}

	return true
}
