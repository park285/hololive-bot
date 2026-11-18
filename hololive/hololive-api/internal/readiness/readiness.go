package readiness

import (
	"context"

	"github.com/gin-gonic/gin"
	"github.com/park285/shared-go/v2/pkg/health"

	sharedreadiness "github.com/kapu/hololive-shared/pkg/readiness"
	"github.com/kapu/hololive-shared/pkg/server/ginjson"
)

func GinHandler(ctx context.Context, p *sharedreadiness.Probe) gin.HandlerFunc {
	return func(c *gin.Context) {
		statusCode, payload := evaluate(sharedreadiness.RequestContext(ctx, c), p)
		ginjson.Respond(c, statusCode, payload)
	}
}

func evaluate(ctx context.Context, p *sharedreadiness.Probe) (statusCode int, payload map[string]any) {
	base := health.Get()
	ready, dependencies := p.Evaluate(ctx)
	statusCode, status := sharedreadiness.HTTPStatus(ready)

	payload = sharedreadiness.BasePayload(base, status)
	payload["plane"] = p.Name()
	payload["dependencies"] = dependencies

	return statusCode, payload
}
