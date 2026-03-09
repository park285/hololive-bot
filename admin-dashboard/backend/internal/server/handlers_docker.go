// Package server: HTTP 서버 및 라우팅
package server

import (
	"context"
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"github.com/park285/llm-kakao-bots/admin-dashboard/internal/auth"
)

// ===== Docker Handlers =====

// handleDockerHealth godoc
// @Summary      Docker health check
// @Description  Check if Docker daemon is accessible
// @Tags         docker
// @Accept       json
// @Produce      json
// @Security     SessionCookie
// @Success      200  {object}  DockerHealthResponse
// @Router       /docker/health [get]
func (s *Server) handleDockerHealth(c *gin.Context) {
	if s.dockerSvc == nil {
		c.JSON(http.StatusOK, gin.H{"status": "unavailable", "available": false})
		return
	}
	available := s.dockerSvc.Available(c.Request.Context())
	dockerStatus := "ok"
	if !available {
		dockerStatus = "unavailable"
	}
	c.JSON(http.StatusOK, gin.H{"status": dockerStatus, "available": available})
}

// handleDockerContainers godoc
// @Summary      List containers
// @Description  Get all managed Docker containers with status and resource usage
// @Tags         docker
// @Accept       json
// @Produce      json
// @Security     SessionCookie
// @Success      200  {object}  ContainerListResponse
// @Failure      503  {object}  ErrorResponse  "Docker service unavailable"
// @Router       /docker/containers [get]
func (s *Server) handleDockerContainers(c *gin.Context) {
	if s.dockerSvc == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Docker service not available"})
		return
	}
	containers, err := s.dockerSvc.ListContainers(c.Request.Context())
	if err != nil {
		s.logger.Error("docker_list_failed", "err", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "An internal error occurred"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "containers": containers})
}

// handleDockerRestart godoc
// @Summary      Restart container
// @Description  Restart a managed Docker container by name
// @Tags         docker
// @Accept       json
// @Produce      json
// @Security     SessionCookie
// @Param        name  path      string  true  "Container name"
// @Success      200   {object}  StatusResponse
// @Failure      404   {object}  ErrorResponse  "Container not found"
// @Failure      503   {object}  ErrorResponse  "Docker service unavailable"
// @Router       /docker/containers/{name}/restart [post]
func (s *Server) handleDockerRestart(c *gin.Context) {
	if s.dockerSvc == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Docker service not available"})
		return
	}
	name := c.Param("name")
	if !s.dockerSvc.IsManaged(name) {
		c.JSON(http.StatusNotFound, gin.H{"error": "container not found"})
		return
	}
	if err := s.dockerSvc.RestartContainer(c.Request.Context(), name); err != nil {
		s.logger.Error("docker_restart_failed", "err", err, "container", name)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "An internal error occurred"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "message": "Container restart initiated"})
}

// handleDockerStop godoc
// @Summary      Stop container
// @Description  Stop a managed Docker container by name
// @Tags         docker
// @Accept       json
// @Produce      json
// @Security     SessionCookie
// @Param        name  path      string  true  "Container name"
// @Success      200   {object}  StatusResponse
// @Failure      404   {object}  ErrorResponse  "Container not found"
// @Failure      503   {object}  ErrorResponse  "Docker service unavailable"
// @Router       /docker/containers/{name}/stop [post]
func (s *Server) handleDockerStop(c *gin.Context) {
	if s.dockerSvc == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Docker service not available"})
		return
	}
	name := c.Param("name")
	if !s.dockerSvc.IsManaged(name) {
		c.JSON(http.StatusNotFound, gin.H{"error": "container not found"})
		return
	}
	if err := s.dockerSvc.StopContainer(c.Request.Context(), name); err != nil {
		s.logger.Error("docker_stop_failed", "err", err, "container", name)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "An internal error occurred"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "message": "Container stopped"})
}

// handleDockerStart godoc
// @Summary      Start container
// @Description  Start a stopped Docker container by name
// @Tags         docker
// @Accept       json
// @Produce      json
// @Security     SessionCookie
// @Param        name  path      string  true  "Container name"
// @Success      200   {object}  StatusResponse
// @Failure      404   {object}  ErrorResponse  "Container not found"
// @Failure      503   {object}  ErrorResponse  "Docker service unavailable"
// @Router       /docker/containers/{name}/start [post]
func (s *Server) handleDockerStart(c *gin.Context) {
	if s.dockerSvc == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Docker service not available"})
		return
	}
	name := c.Param("name")
	if !s.dockerSvc.IsManaged(name) {
		c.JSON(http.StatusNotFound, gin.H{"error": "container not found"})
		return
	}
	if err := s.dockerSvc.StartContainer(c.Request.Context(), name); err != nil {
		s.logger.Error("docker_start_failed", "err", err, "container", name)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "An internal error occurred"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "message": "Container started"})
}

func (s *Server) handleDockerLogStream(c *gin.Context) {
	if s.dockerSvc == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Docker service not available"})
		return
	}

	name := c.Param("name")
	if !s.dockerSvc.IsManaged(name) {
		c.JSON(http.StatusNotFound, gin.H{"error": "container not found"})
		return
	}

	// 세션 ID 추출 (per-session 제한용)
	sessionID := "anonymous"
	if signedSessionID, err := c.Cookie("admin_session"); err == nil && signedSessionID != "" {
		if sid, valid := auth.ValidateSessionSignature(signedSessionID, s.cfg.AdminSecretKey); valid {
			sessionID = sid
		}
	}

	// 동시 연결 제한: StreamLimiter 사용
	allowed, result := s.streamLimiter.TryAcquire(sessionID)
	if !allowed {
		errMsg := "Too many log stream connections"
		if result.PerSessionHitCnt > 0 {
			errMsg = "Too many streams for this session"
		}
		c.JSON(http.StatusTooManyRequests, gin.H{"error": errMsg})
		return
	}
	defer s.streamLimiter.Release(sessionID)

	// WebSocket 업그레이드
	upgrader := s.newWSUpgrader()
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	defer func() { _ = conn.Close() }()

	// 시간 제한: 무제한 follow로 인한 goroutine/FD 고갈(DoS)을 방지
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Minute)
	defer cancel()

	logReader, err := s.dockerSvc.GetLogStream(ctx, name)
	if err != nil {
		s.logger.Error("docker_logstream_failed", "err", err, "container", name)
		_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
		_ = conn.WriteJSON(gin.H{"error": "An internal error occurred"})
		return
	}
	defer func() { _ = logReader.Close() }()

	streamDockerLogPayloads(ctx, conn, logReader)
}

// streamDockerLogPayloads: Docker log multiplexed 스트림을 WebSocket으로 전달
// - ctx timeout/취소를 준수
// - write deadline으로 slow client를 방어
func streamDockerLogPayloads(ctx context.Context, conn *websocket.Conn, logReader io.Reader) {
	header := make([]byte, 8)
	buf := make([]byte, 4096)
	const maxLogChunkSize = 1 << 20

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		if _, err := io.ReadFull(logReader, header); err != nil {
			return
		}

		size := int(header[4])<<24 | int(header[5])<<16 | int(header[6])<<8 | int(header[7])
		if size <= 0 {
			continue
		}
		if size > maxLogChunkSize {
			_, _ = io.CopyN(io.Discard, logReader, int64(size))
			continue
		}
		if size > cap(buf) {
			buf = make([]byte, size)
		}
		payload := buf[:size]

		n, err := io.ReadFull(logReader, payload)
		if err != nil {
			return
		}

		_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
		if err := conn.WriteMessage(websocket.TextMessage, payload[:n]); err != nil {
			return
		}
	}
}
