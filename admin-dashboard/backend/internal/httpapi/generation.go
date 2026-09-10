package httpapi

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/kapu/admin-dashboard/internal/contract"
	"github.com/kapu/admin-dashboard/internal/httpx"
)

func handleMetadata(c *gin.Context) {
	httpx.Respond(c, http.StatusOK, contract.AdminMetadata{ClientGeneration: contract.Generation})
}

func isAdminAPI(path string) bool {
	return path == "/admin/api" || strings.HasPrefix(path, "/admin/api/")
}

func (r *API) contractGeneration() gin.HandlerFunc {
	return func(c *gin.Context) {
		path := c.Request.URL.Path
		if path == "/admin/meta.json" || isAdminAPI(path) {
			c.Header("X-Admin-Server-Generation", contract.Generation)
		}

		if isAdminAPI(path) && path != "/admin/api/ws/system-stats" && c.GetHeader("X-Admin-Client-Generation") != contract.Generation {
			httpx.Abort(c, &contract.AppError{Status: http.StatusConflict, Body: contract.ErrorResponse{
				Code: "CLIENT_GENERATION_MISMATCH", Error: "Reload the administrator application",
			}})

			return
		}

		c.Next()
	}
}
