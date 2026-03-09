// Package server: HTTP 서버 및 라우팅
package server

import (
	"log/slog"
	"net/http"
	"os"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/park285/llm-kakao-bots/admin-dashboard/internal/logs"
)

// ===== System Logs Handlers =====

// handleLogFiles godoc
// @Summary      List log files
// @Description  Get available system log files
// @Tags         logs
// @Accept       json
// @Produce      json
// @Security     SessionCookie
// @Success      200  {object}  LogFilesResponse
// @Router       /logs/files [get]
func (s *Server) handleLogFiles(c *gin.Context) {
	files := logs.ListLogFiles()
	c.JSON(http.StatusOK, gin.H{"status": "ok", "files": files})
}

// handleSystemLogs godoc
// @Summary      Get system logs
// @Description  Read last N lines from a system log file
// @Tags         logs
// @Accept       json
// @Produce      json
// @Security     SessionCookie
// @Param        file   query     string  false  "Log file key"  default(combined)
// @Param        lines  query     int     false  "Number of lines to fetch"  default(200)  maximum(1000)
// @Success      200    {object}  SystemLogsResponse
// @Failure      400    {object}  ErrorResponse  "Invalid log file"
// @Router       /logs [get]
func (s *Server) handleSystemLogs(c *gin.Context) {
	fileKey := c.Query("file")
	if fileKey == "" {
		fileKey = "combined"
	}

	logPath, ok := logs.GetLogFilePath(fileKey)
	if !ok {
		c.JSON(400, gin.H{"error": "Invalid log file", "allowed_keys": logs.GetLogFileKeys()})
		return
	}

	linesStr := c.Query("lines")
	lines := 200
	if linesStr != "" {
		if n, err := strconv.Atoi(linesStr); err == nil {
			lines = n
		}
	}
	if lines > logs.MaxLogLines {
		lines = logs.MaxLogLines
	}
	if lines < 1 {
		lines = 1
	}

	logLines, err := logs.TailFile(logPath, lines)
	if err != nil {
		if os.IsNotExist(err) {
			c.JSON(200, gin.H{"status": "ok", "file": fileKey, "lines": []string{}, "error": "Log file not found"})
			return
		}
		s.logger.Error("Failed to read log file", slog.String("file", logPath), slog.Any("error", err))
		c.JSON(500, gin.H{"error": "Failed to read log file"})
		return
	}

	c.JSON(200, gin.H{"status": "ok", "file": fileKey, "lines": logLines, "count": len(logLines)})
}
