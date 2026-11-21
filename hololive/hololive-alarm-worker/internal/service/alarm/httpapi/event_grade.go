package httpapi

import (
	"context"
	jsonv2 "encoding/json/v2"
	"net/http"

	"github.com/gin-gonic/gin"

	contract "github.com/kapu/hololive-shared/pkg/contracts/alarm"
	"github.com/kapu/hololive-shared/pkg/server/ginjson"
)

type EventGradeRegistrar interface {
	RegisterEventGrade(context.Context, contract.EventGradeRequest) (contract.EventGradeResponse, error)
}

func registerGradeRoute(group *gin.RouterGroup, service EventGradeRegistrar) {
	group.POST(contract.EventGradePath, func(c *gin.Context) {
		var req contract.EventGradeRequest

		body := http.MaxBytesReader(c.Writer, c.Request.Body, 1024)

		if jsonv2.UnmarshalRead(body, &req, jsonv2.RejectUnknownMembers(true)) != nil || !req.Valid() {
			ginjson.Respond(c, http.StatusBadRequest, alarmAPIError("invalid_event_grade", "invalid event grade request"))

			return
		}

		resp, err := service.RegisterEventGrade(c.Request.Context(), req)
		if err != nil {
			ginjson.Respond(c, http.StatusServiceUnavailable, alarmAPIError("event_grade_unavailable", "registration unavailable"))

			return
		}

		ginjson.Respond(c, http.StatusOK, contract.Envelope[contract.EventGradeResponse]{Success: true, Data: resp})
	})
}
