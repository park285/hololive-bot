package readiness

import (
	"context"
	"maps"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/kapu/hololive-shared/pkg/health"
	sharedreadiness "github.com/kapu/hololive-shared/pkg/readiness"
)

func Pick(probes ...*sharedreadiness.Probe) *sharedreadiness.Probe {
	for _, p := range probes {
		if p != nil {
			return p
		}
	}
	return nil
}

func GinHandler(ctx context.Context, p *sharedreadiness.Probe) gin.HandlerFunc {
	return func(c *gin.Context) {
		if p == nil {
			c.JSON(http.StatusOK, map[string]any{"status": "ready", "health": health.Get()})
			return
		}
		statusCode, payload := evaluate(sharedreadiness.RequestContext(ctx, c), p)
		c.JSON(statusCode, payload)
	}
}

func evaluate(ctx context.Context, p *sharedreadiness.Probe) (statusCode int, payload map[string]any) {
	base := health.Get()
	ready, groups := p.Evaluate(ctx)
	dependencies := map[string]bool{}
	for _, group := range []string{sharedreadiness.GroupDependencies, sharedreadiness.GroupEgressFlags} {
		maps.Copy(dependencies, groups[group])
	}
	statusCode, status := sharedreadiness.HTTPStatus(ready)
	payload = sharedreadiness.BasePayload(base, status)
	payload["plane"] = p.Name()
	payload["dependencies"] = dependencies
	return statusCode, payload
}
