package server

import (
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	sharedserver "github.com/kapu/hololive-shared/pkg/server"
)

func (h *APIHandler) safeLogger() *slog.Logger {
	if h != nil && h.logger != nil {
		return h.logger
	}

	return slog.Default()
}

func (h *APIHandler) logActivity(entryType, summary string, details map[string]any) {
	if h != nil && h.activity != nil {
		h.activity.Log(entryType, summary, details)
	}
}

func respondServiceUnavailable(c *gin.Context, message string) {
	sharedserver.RespondError(c, http.StatusServiceUnavailable, message, nil)
}

func (h *AlarmAPIHandler) requireAlarm(c *gin.Context) bool {
	if h == nil || h.APIHandler == nil || h.alarm == nil {
		respondServiceUnavailable(c, "alarm service not available")
		return false
	}

	return true
}

func (h *MemberAPIHandler) requireMemberDeps(c *gin.Context) bool {
	if h == nil || h.APIHandler == nil || h.repo == nil || h.memberCache == nil {
		respondServiceUnavailable(c, "member service not available")
		return false
	}

	return true
}

func (h *RoomAPIHandler) requireACL(c *gin.Context) bool {
	if h == nil || h.APIHandler == nil || h.acl == nil {
		respondServiceUnavailable(c, "ACL service not available")
		return false
	}

	return true
}

func (h *StatsAPIHandler) requireStatsDeps(c *gin.Context) bool {
	if h == nil || h.APIHandler == nil || h.repo == nil || h.alarm == nil {
		respondServiceUnavailable(c, "stats dependencies not available")
		return false
	}

	return true
}

func (h *ProfileAPIHandler) requireProfiles(c *gin.Context) bool {
	if h == nil || h.APIHandler == nil || h.profiles == nil {
		respondServiceUnavailable(c, "Profile service unavailable")
		return false
	}

	return true
}

func (h *MilestoneAPIHandler) requireStatsRepo(c *gin.Context) bool {
	if h == nil || h.APIHandler == nil || h.statsRepo == nil {
		respondServiceUnavailable(c, "Stats repository not available")
		return false
	}

	return true
}

func (h *TemplateAPIHandler) requireTemplateAdmin(c *gin.Context) bool {
	if h == nil || h.APIHandler == nil || h.templateAdmin == nil {
		respondServiceUnavailable(c, "template service not available")
		return false
	}

	return true
}
