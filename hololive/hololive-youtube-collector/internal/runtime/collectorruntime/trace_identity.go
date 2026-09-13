package collectorruntime

import (
	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type collectorTraceIdentity string

func (instanceID collectorTraceIdentity) handle(c *gin.Context) {
	// 공통 service.name을 유지하면서 한 AP의 수신이 다른 AP의 장애를 가리지 않게 한다.
	trace.SpanFromContext(c.Request.Context()).SetAttributes(
		attribute.String("youtube.collector.instance_id", string(instanceID)),
	)
	c.Next()
}
