package server

import (
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestStatsAPIHandler_StreamSystemStats_CollectorUnavailable(t *testing.T) {
	gin.SetMode(gin.TestMode)

	handler := &StatsAPIHandler{APIHandler: &APIHandler{logger: newDiscardLogger()}}
	ctx, rec := newAPITestContext(http.MethodGet, "/api/holo/stats/system", nil)

	handler.StreamSystemStats(ctx)

	assertErrorResponse(t, rec, http.StatusBadRequest, "System stats collector not available")
}
