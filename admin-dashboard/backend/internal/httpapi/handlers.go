package httpapi

import (
	"context"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/kapu/admin-dashboard/internal/contract"
	"github.com/kapu/admin-dashboard/internal/httpx"
)

func (r *API) handleHealth(c *gin.Context) {
	httpx.Respond(c, http.StatusOK, statusResponse{Status: "ok"})
}

func (r *API) handleDockerHealth(c *gin.Context) {
	available := false

	if r.docker != nil {
		available = r.docker.Available(c.Request.Context())
	}

	httpx.Respond(c, http.StatusOK, dockerHealthResponse{Status: "ok", Available: available})
}

func (r *API) handleDockerContainers(c *gin.Context) {
	if r.docker == nil {
		httpx.Abort(c, contract.NewError(http.StatusServiceUnavailable, "Docker service not available"))

		return
	}

	containers, err := r.docker.ListContainers(c.Request.Context())
	if err != nil {
		httpx.Abort(c, err)

		return
	}

	httpx.Respond(c, http.StatusOK, dockerContainersResponse{Status: "ok", Containers: containers})
}

func (r *API) handleDockerRestart(c *gin.Context) { r.dockerAction(c, "restart") }
func (r *API) handleDockerStop(c *gin.Context)    { r.dockerAction(c, "stop") }
func (r *API) handleDockerStart(c *gin.Context)   { r.dockerAction(c, "start") }

func (r *API) dockerAction(c *gin.Context, action string) {
	if r.docker == nil {
		httpx.Abort(c, contract.NewError(http.StatusServiceUnavailable, "Docker service not available"))

		return
	}

	name := c.Param("name")
	if err := r.dockerExec(c.Request.Context(), action, name); err != nil {
		httpx.Abort(c, err)

		return
	}

	message := map[string]string{"restart": "restarted", "stop": "stopped", "start": "started"}[action]
	httpx.Respond(c, http.StatusOK, dockerActionResponse{Status: "ok", Message: "Container " + name + " " + message})
}

func (r *API) dockerExec(ctx context.Context, action, name string) error {
	operations := map[string]func(context.Context, string) error{
		"restart": r.docker.RestartContainer,
		"stop":    r.docker.StopContainer,
		"start":   r.docker.StartContainer,
	}
	operation, ok := operations[action]

	if !ok {
		return nil
	}

	if err := operation(ctx, name); err != nil {
		return fmt.Errorf("%s container: %w", action, err)
	}

	return nil
}

func (r *API) handleAggregatedStatus(c *gin.Context) {
	httpx.Respond(c, http.StatusOK, r.statusCollector.Collect(c.Request.Context()))
}

func (r *API) handleOpenAPI(c *gin.Context) {
	if !r.cfg.EnableOpenAPI && !r.cfg.EnableSwaggerUI {
		httpx.Abort(c, contract.NewError(http.StatusNotFound, "Not found"))

		return
	}

	c.Data(http.StatusOK, "application/json; charset=utf-8", r.openapiJSON)
}

func (r *API) handleDocs(c *gin.Context) {
	if !r.cfg.EnableSwaggerUI {
		httpx.Abort(c, contract.NewError(http.StatusNotFound, "Not found"))

		return
	}

	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(`<!doctype html><title>Admin API</title><h1>Admin Dashboard API</h1><p>OpenAPI JSON: <a href="/admin/api/openapi.json">/admin/api/openapi.json</a></p>`))
}
