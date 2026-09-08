package app

import (
	"crypto/rand"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	sharedlogging "github.com/park285/shared-go/v2/pkg/logging"
	"github.com/park285/shared-go/v2/pkg/stringutil"
)

func (r *Runtime) requestContext() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 외부 요청 ID를 그대로 신뢰하면 공격자가 감사 이벤트의 상관관계를 위조할 수 있으므로
		// 서버 내부 감사 상관관계용 값을 매 요청마다 새로 만든다.
		requestID := rand.Text()

		c.Request = c.Request.WithContext(sharedlogging.WithRequestID(c.Request.Context(), requestID))

		c.Next()
	}
}

func (r *Runtime) auditMutation() gin.HandlerFunc {
	return func(c *gin.Context) {
		started := time.Now()

		c.Next()

		status := c.Writer.Status()
		attrs := r.auditRequestAttrs(c, status)

		attrs = append(attrs, sharedlogging.SinceMS(started))
		sharedlogging.Log(c.Request.Context(), r.logger, auditLevel(status), "admin.mutation", "admin mutation completed", attrs...)
	}
}

func (r *Runtime) auditRequestAttrs(c *gin.Context, status int) []slog.Attr {
	actor := "unauthenticated"

	if _, ok := sessionFrom(c); ok {
		actor = r.cfg.AdminUser
	}

	attrs := []slog.Attr{
		slog.String("actor", actor),
		slog.String("client_ip", r.clientIP(c.Request)),
		slog.String("action", c.Request.Method),
		slog.String("route", stringutil.TruncateString(c.FullPath(), 256)),
		slog.String("result", auditResult(status)),
		slog.Int("status", status),
	}

	if target := auditTarget(c); target != "" {
		attrs = append(attrs, slog.String("target", target))
	}

	return attrs
}

func auditTarget(c *gin.Context) string {
	for _, name := range []string{"name", "id"} {
		if value := strings.TrimSpace(c.Param(name)); value != "" {
			return stringutil.TruncateString(value, 128)
		}
	}

	return ""
}

func auditResult(status int) string {
	switch {
	case status >= http.StatusOK && status < http.StatusBadRequest:
		return "success"
	case status == http.StatusUnauthorized || status == http.StatusForbidden || status == http.StatusTooManyRequests:
		return "denied"
	default:
		return "failed"
	}
}

func auditLevel(status int) slog.Level {
	if status >= http.StatusBadRequest {
		return slog.LevelWarn
	}

	return slog.LevelInfo
}

func (r *Runtime) auditSecurityRejection(c *gin.Context, event string) {
	sharedlogging.Warn(c.Request.Context(), r.logger, event, "admin security request rejected", r.auditRequestAttrs(c, c.Writer.Status())...)
}

func (r *Runtime) auditLogin(c *gin.Context, result string, status int) {
	actor := "unauthenticated"

	if result == "success" {
		actor = r.cfg.AdminUser
	}

	sharedlogging.Log(c.Request.Context(), r.logger, auditLevel(status), "admin.login", "admin login completed",
		slog.String("actor", actor),
		slog.String("client_ip", r.clientIP(c.Request)),
		slog.String("action", "login"),
		slog.String("result", result),
		slog.Int("status", status),
	)
}
