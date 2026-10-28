package api

import (
	jsonv2 "encoding/json/v2"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"

	sharedserver "github.com/kapu/hololive-shared/pkg/server/httpserver"
)

func bindJSON(c *gin.Context, destination any, options ...jsonv2.Options) error {
	if err := jsonv2.UnmarshalRead(c.Request.Body, destination, options...); err != nil {
		return fmt.Errorf("unmarshal read: %w", err)
	}

	if binding.Validator == nil {
		return nil
	}

	if err := binding.Validator.ValidateStruct(destination); err != nil {
		return fmt.Errorf("validate struct: %w", err)
	}

	return nil
}

func loggerOrDefault(logger *slog.Logger) *slog.Logger {
	if logger != nil {
		return logger
	}

	return slog.Default()
}

func respondServiceUnavailable(c *gin.Context, message string) {
	sharedserver.RespondError(c, http.StatusServiceUnavailable, message, nil)
}

func (h *AlarmHandler) requireAlarm(c *gin.Context) bool {
	if h == nil || h.alarm == nil {
		respondServiceUnavailable(c, "alarm service not available")

		return false
	}

	return true
}

func (h *MemberHandler) requireMemberDeps(c *gin.Context) bool {
	if h == nil || h.repository == nil || h.service == nil || h.service.repository == nil || h.service.cache == nil {
		respondServiceUnavailable(c, "member service not available")

		return false
	}

	return true
}

func (h *RoomHandler) requireACL(c *gin.Context) bool {
	if h == nil || h.acl == nil {
		respondServiceUnavailable(c, "ACL service not available")

		return false
	}

	return true
}

func (h *RoomHandler) requireIris(c *gin.Context) bool {
	if h == nil || h.iris == nil {
		respondServiceUnavailable(c, "iris room listing not available")

		return false
	}

	return true
}

func (h *StatsHandler) requireStatsDeps(c *gin.Context) bool {
	if h == nil || h.repository == nil || h.alarm == nil {
		respondServiceUnavailable(c, "stats dependencies not available")

		return false
	}

	return true
}
