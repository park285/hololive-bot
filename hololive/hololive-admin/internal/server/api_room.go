package server

import (
	"fmt"
	"log/slog"

	"github.com/gin-gonic/gin"
)

// GetRooms: 설정된 방 목록을 반환합니다.
func (h *APIHandler) GetRooms(c *gin.Context) {
	if h.acl == nil {
		c.JSON(503, gin.H{"error": "ACL service not available"})
		return
	}
	aclEnabled, rooms := h.acl.GetACLStatus()
	c.JSON(200, gin.H{
		"status":     "ok",
		"rooms":      rooms,
		"aclEnabled": aclEnabled,
	})
}

// AddRoom: 화이트리스트에 새로운 방을 추가합니다.
func (h *APIHandler) AddRoom(c *gin.Context) {
	if h.acl == nil {
		c.JSON(503, gin.H{"error": "ACL service not available"})
		return
	}

	var req struct {
		Room string `json:"room" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	ctx := c.Request.Context()
	added, err := h.acl.AddRoom(ctx, req.Room)
	if err != nil {
		h.logger.Error("Failed to add room", slog.String("room", req.Room), slog.Any("error", err))
		c.JSON(500, gin.H{"error": "Failed to add room"})
		return
	}

	if !added {
		c.JSON(200, gin.H{"status": "ok", "message": "Room already exists"})
		return
	}

	c.JSON(200, gin.H{
		"status":  "ok",
		"message": "Room added successfully",
	})

	h.activity.Log("room_add", "Room added to whitelist: "+req.Room, map[string]any{"room": req.Room})
}

// RemoveRoom: 화이트리스트에서 방을 제거합니다.
func (h *APIHandler) RemoveRoom(c *gin.Context) {
	if h.acl == nil {
		c.JSON(503, gin.H{"error": "ACL service not available"})
		return
	}

	var req struct {
		Room string `json:"room" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	ctx := c.Request.Context()
	_, err := h.acl.RemoveRoom(ctx, req.Room)
	if err != nil {
		h.logger.Error("Failed to remove room", slog.String("room", req.Room), slog.Any("error", err))
		c.JSON(500, gin.H{"error": "Failed to remove room"})
		return
	}

	c.JSON(200, gin.H{
		"status":  "ok",
		"message": "Room removed successfully",
	})

	h.activity.Log("room_remove", "Room removed from whitelist: "+req.Room, map[string]any{"room": req.Room})
}

// SetACL: 방 ACL을 활성화 또는 비활성화합니다.
func (h *APIHandler) SetACL(c *gin.Context) {
	if h.acl == nil {
		c.JSON(503, gin.H{"error": "ACL service not available"})
		return
	}

	var req struct {
		Enabled bool `json:"enabled"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	ctx := c.Request.Context()
	if err := h.acl.SetEnabled(ctx, req.Enabled); err != nil {
		h.logger.Error("Failed to set ACL", slog.Bool("enabled", req.Enabled), slog.Any("error", err))
		c.JSON(500, gin.H{"error": "Failed to set ACL"})
		return
	}

	h.logger.Info("Room ACL updated", slog.Bool("enabled", req.Enabled))

	h.activity.Log("acl_update", fmt.Sprintf("Room ACL state changed to %v", req.Enabled), map[string]any{"enabled": req.Enabled})
	c.JSON(200, gin.H{
		"status":  "ok",
		"message": "ACL setting updated successfully",
		"enabled": req.Enabled,
	})
}
