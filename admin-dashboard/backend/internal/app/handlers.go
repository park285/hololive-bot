package app

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/park285/shared-go/pkg/ginjson"

	"github.com/kapu/admin-dashboard/internal/httpx"
	"github.com/kapu/admin-dashboard/internal/status"
)

func (r *Runtime) handleHealth(c *gin.Context) {
	ginjson.Respond(c, http.StatusOK, statusResponse{Status: "ok"})
}

func (r *Runtime) handleDockerHealth(c *gin.Context) {
	available := false
	if r.docker != nil {
		available = r.docker.Available(c.Request.Context())
	}
	ginjson.Respond(c, http.StatusOK, dockerHealthResponse{Status: "ok", Available: available})
}

func (r *Runtime) handleDockerContainers(c *gin.Context) {
	if r.docker == nil {
		httpx.Abort(c, httpx.NewError(http.StatusServiceUnavailable, "Docker service not available"))
		return
	}
	containers, err := r.docker.ListContainers(c.Request.Context())
	if err != nil {
		httpx.Abort(c, err)
		return
	}
	ginjson.Respond(c, http.StatusOK, dockerContainersResponse{Status: "ok", Containers: containers})
}

func (r *Runtime) handleDockerRestart(c *gin.Context) { r.dockerAction(c, "restart") }
func (r *Runtime) handleDockerStop(c *gin.Context)    { r.dockerAction(c, "stop") }
func (r *Runtime) handleDockerStart(c *gin.Context)   { r.dockerAction(c, "start") }

func (r *Runtime) dockerAction(c *gin.Context, action string) {
	if r.docker == nil {
		httpx.Abort(c, httpx.NewError(http.StatusServiceUnavailable, "Docker service not available"))
		return
	}
	name := c.Param("name")
	if err := r.dockerExec(c.Request.Context(), action, name); err != nil {
		httpx.Abort(c, err)
		return
	}
	r.logger.Info("docker container action", slog.String("action", action), slog.String("container", name))
	message := map[string]string{"restart": "restarted", "stop": "stopped", "start": "started"}[action]
	ginjson.Respond(c, http.StatusOK, dockerActionResponse{Status: "ok", Message: "Container " + name + " " + message})
}

func (r *Runtime) dockerExec(ctx context.Context, action, name string) error {
	switch action {
	case "restart":
		return r.docker.RestartContainer(ctx, name)
	case "stop":
		return r.docker.StopContainer(ctx, name)
	case "start":
		return r.docker.StartContainer(ctx, name)
	}
	return nil
}

func (r *Runtime) handleAggregatedStatus(c *gin.Context) {
	ginjson.Respond(c, http.StatusOK, r.statusCollector.Collect(c.Request.Context()))
}

func (r *Runtime) handleSystemStatsWS(c *gin.Context) {
	origin := c.Request.Header.Get("Origin")
	if err := r.verifyWSOrigin(origin); err != nil {
		httpx.Abort(c, err)
		return
	}
	select {
	case r.wsStreams <- struct{}{}:
		defer func() { <-r.wsStreams }()
	default:
		httpx.Abort(c, &httpx.AppError{Status: http.StatusTooManyRequests, Body: httpx.ErrorResponse{Error: "Too many active system stats streams", Details: map[string]int{"limit": maxSystemStatsStreams}}})
		return
	}
	upgrader := websocket.Upgrader{
		HandshakeTimeout: 5 * time.Second,
		ReadBufferSize:   1024,
		WriteBufferSize:  4096,
		CheckOrigin: func(req *http.Request) bool {
			return req.Context().Err() == nil
		},
	}
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	defer closeConn(conn)
	r.streamSystemStats(conn)
}

func (r *Runtime) streamSystemStats(conn *websocket.Conn) {
	history, updates, unsubscribe := r.statsHub.Subscribe()
	defer unsubscribe()

	peerGone := watchPeer(conn, r.wsPongWait)

	for _, stats := range history {
		if !writeSystemStatsFrame(conn, stats) {
			return
		}
	}

	r.pumpSystemStats(conn, updates, peerGone)
}

func (r *Runtime) pumpSystemStats(conn *websocket.Conn, updates <-chan status.SystemStats, peerGone <-chan struct{}) {
	ping := time.NewTicker(r.wsPingPeriod)
	defer ping.Stop()

	for {
		if !pumpSystemStatsOnce(conn, updates, peerGone, ping.C) {
			return
		}
	}
}

func pumpSystemStatsOnce(conn *websocket.Conn, updates <-chan status.SystemStats, peerGone <-chan struct{}, tick <-chan time.Time) bool {
	select {
	case <-peerGone:
		return false
	case <-tick:
		return writePing(conn)
	case stats, ok := <-updates:
		return ok && writeSystemStatsFrame(conn, stats)
	}
}

func watchPeer(conn *websocket.Conn, pongWait time.Duration) <-chan struct{} {
	gone := make(chan struct{})
	conn.SetReadLimit(512)
	if err := conn.SetReadDeadline(time.Now().Add(pongWait)); err != nil {
		close(gone)
		return gone
	}
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(pongWait))
	})
	go func() {
		defer close(gone)
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()
	return gone
}

func writePing(conn *websocket.Conn) bool {
	if err := conn.SetWriteDeadline(time.Now().Add(wsWriteWait)); err != nil {
		return false
	}
	return conn.WriteMessage(websocket.PingMessage, nil) == nil
}

func writeSystemStatsFrame(conn *websocket.Conn, stats any) bool {
	if err := setWriteDeadline(conn); err != nil {
		return false
	}
	return conn.WriteJSON(stats) == nil
}

func closeConn(conn *websocket.Conn) {
	if err := conn.Close(); err != nil {
		return
	}
}

func setWriteDeadline(conn *websocket.Conn) error {
	return conn.SetWriteDeadline(time.Now().Add(wsWriteWait))
}

func (r *Runtime) handleOpenAPI(c *gin.Context) {
	if !r.cfg.EnableOpenAPI && !r.cfg.EnableSwaggerUI {
		ginjson.Respond(c, http.StatusNotFound, httpx.ErrorResponse{Error: "Not found"})
		return
	}
	c.Data(http.StatusOK, "application/json; charset=utf-8", r.openapiJSON)
}

func (r *Runtime) handleDocs(c *gin.Context) {
	if !r.cfg.EnableSwaggerUI {
		ginjson.Respond(c, http.StatusNotFound, httpx.ErrorResponse{Error: "Not found"})
		return
	}
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(`<!doctype html><title>Admin API</title><h1>Admin Dashboard API</h1><p>OpenAPI JSON: <a href="/admin/api/openapi.json">/admin/api/openapi.json</a></p>`))
}
