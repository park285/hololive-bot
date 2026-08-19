package readiness

import (
	"context"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/kapu/hololive-shared/pkg/health"
	sharedreadiness "github.com/kapu/hololive-shared/pkg/readiness"
)

func PublicGinHandler(ctx context.Context, probe *sharedreadiness.Probe) gin.HandlerFunc {
	return func(c *gin.Context) {
		statusCode, payload := publicResponse(probe, sharedreadiness.RequestContext(ctx, c))
		c.JSON(statusCode, payload)
	}
}

func InternalGinHandler(ctx context.Context, probe *sharedreadiness.Probe) gin.HandlerFunc {
	return func(c *gin.Context) {
		statusCode, payload := internalResponse(probe, sharedreadiness.RequestContext(ctx, c))
		c.JSON(statusCode, payload)
	}
}

func internalResponse(probe *sharedreadiness.Probe, ctx context.Context) (statusCode int, payload map[string]any) {
	base := health.Get()
	if probe == nil {
		return http.StatusServiceUnavailable, runtimePayload(base, "not_ready", "")
	}

	ready, groups := probe.Evaluate(ctx)
	statusCode, status := sharedreadiness.HTTPStatus(ready)
	payload = runtimePayload(base, status, probe.Name())
	for group, checks := range groups {
		payload[group] = checks
	}
	return statusCode, payload
}

func publicResponse(probe *sharedreadiness.Probe, ctx context.Context) (statusCode int, payload map[string]any) {
	base := health.Get()
	if probe == nil {
		return http.StatusServiceUnavailable, runtimePayload(base, "not_ready", "")
	}
	ready, _ := probe.Evaluate(ctx)
	statusCode, status := sharedreadiness.HTTPStatus(ready)
	return statusCode, runtimePayload(base, status, probe.Name())
}

func runtimePayload(base health.Response, status, runtimeName string) map[string]any {
	payload := sharedreadiness.BasePayload(base, status)
	if strings.TrimSpace(runtimeName) != "" {
		payload["runtime"] = strings.TrimSpace(runtimeName)
	}
	return payload
}
