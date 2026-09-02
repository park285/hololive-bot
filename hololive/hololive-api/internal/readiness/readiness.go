package readiness

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/park285/shared-go/v2/pkg/ginjson"
	"github.com/park285/shared-go/v2/pkg/health"

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
			ginjson.Respond(c, http.StatusOK, map[string]any{"status": "ready", "health": health.Get()})

			return
		}

		statusCode, payload := evaluate(sharedreadiness.RequestContext(ctx, c), p)
		ginjson.Respond(c, statusCode, payload)
	}
}

func evaluate(ctx context.Context, p *sharedreadiness.Probe) (statusCode int, payload map[string]any) {
	base := health.Get()
	ready, groups := p.Evaluate(ctx)
	dependencies := groups[sharedreadiness.GroupDependencies]
	statusCode, status := sharedreadiness.HTTPStatus(ready)

	payload = sharedreadiness.BasePayload(base, status)
	payload["plane"] = p.Name()
	payload["dependencies"] = dependencies

	return statusCode, payload
}
