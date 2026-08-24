package readiness

import (
	"context"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/park285/shared-go/v2/pkg/ginjson"

	"github.com/kapu/hololive-shared/pkg/health"
	sharedreadiness "github.com/kapu/hololive-shared/pkg/readiness"
)

func PublicGinHandler(ctx context.Context, probe *sharedreadiness.Probe) gin.HandlerFunc {
	return func(c *gin.Context) {
		statusCode, payload := publicResponse(sharedreadiness.RequestContext(ctx, c), probe)
		ginjson.Respond(c, statusCode, payload)
	}
}

func InternalGinHandler(ctx context.Context, probe *sharedreadiness.Probe) gin.HandlerFunc {
	return func(c *gin.Context) {
		statusCode, payload := internalResponse(sharedreadiness.RequestContext(ctx, c), probe)
		ginjson.Respond(c, statusCode, payload)
	}
}

func internalResponse(ctx context.Context, probe *sharedreadiness.Probe) (statusCode int, payload map[string]any) {
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

func publicResponse(ctx context.Context, probe *sharedreadiness.Probe) (statusCode int, payload map[string]any) {
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
