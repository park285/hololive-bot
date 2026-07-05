package api

import (
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	sharedserver "github.com/kapu/hololive-shared/pkg/server"
)

func loggerOrDefault(logger *slog.Logger) *slog.Logger {
	if logger != nil {
		return logger
	}

	return slog.Default()
}

func (h *Handler) safeLogger() *slog.Logger {
	if h == nil {
		return slog.Default()
	}

	return loggerOrDefault(h.logger)
}

func (h *Handler) logActivity(entryType, summary string, details map[string]any) {
	if h != nil && h.activity != nil {
		h.activity.Log(entryType, summary, details)
	}
}

func respondServiceUnavailable(c *gin.Context, message string) {
	sharedserver.RespondError(c, http.StatusServiceUnavailable, message, nil)
}

func (h *AlarmHandler) requireAlarm(c *gin.Context) bool {
	if h == nil || h.Handler == nil || h.alarm == nil {
		respondServiceUnavailable(c, "alarm service not available")
		return false
	}

	return true
}

func (h *MemberHandler) requireMemberDeps(c *gin.Context) bool {
	if h == nil || h.Handler == nil || h.repository == nil || h.memberCache == nil {
		respondServiceUnavailable(c, "member service not available")
		return false
	}

	return true
}

func (h *RoomHandler) requireACL(c *gin.Context) bool {
	if h == nil || h.Handler == nil || h.acl == nil {
		respondServiceUnavailable(c, "ACL service not available")
		return false
	}

	return true
}

func (h *StatsHandler) requireStatsDeps(c *gin.Context) bool {
	if h == nil || h.Handler == nil || h.repository == nil || h.alarm == nil {
		respondServiceUnavailable(c, "stats dependencies not available")
		return false
	}

	return true
}

func (h *ProfileHandler) requireProfiles(c *gin.Context) bool {
	if h == nil || h.Handler == nil || h.profiles == nil {
		respondServiceUnavailable(c, "Profile service unavailable")
		return false
	}

	return true
}

func (h *TemplateHandler) requireTemplateAdmin(c *gin.Context) bool {
	if h == nil || h.Handler == nil || h.templateAdmin == nil {
		respondServiceUnavailable(c, "template service not available")
		return false
	}

	return true
}
