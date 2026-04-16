// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package server

import (
	"fmt"
	"log/slog"

	"github.com/gin-gonic/gin"

	"github.com/kapu/hololive-shared/pkg/service/acl"
)

func (h *RoomAPIHandler) GetRooms(c *gin.Context) {
	if h.acl == nil {
		c.JSON(503, gin.H{"error": "ACL service not available"})
		return
	}

	aclEnabled, mode, rooms := h.acl.GetACLStatus()
	c.JSON(200, gin.H{
		"status":     "ok",
		"rooms":      rooms,
		"aclEnabled": aclEnabled,
		"aclMode":    string(mode),
	})
}

//
//nolint:dupl // AddRoom/RemoveRoom은 구조적으로 유사하나 비즈니스 로직이 다름
func (h *RoomAPIHandler) AddRoom(c *gin.Context) {
	if h.acl == nil {
		c.JSON(503, gin.H{"error": "ACL service not available"})
		return
	}

	var req struct {
		Room string `json:"room" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Warn("Invalid request body", slog.Any("error", err))
		c.JSON(400, gin.H{"error": "invalid request body"})

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
		c.JSON(409, gin.H{"error": "Room already exists"})
		return
	}

	c.JSON(200, gin.H{
		"status":  "ok",
		"message": "Room added successfully",
	})

	h.activity.Log("room_add", "Room added to ACL list: "+req.Room, map[string]any{"room": req.Room})
}

//
//nolint:dupl // AddRoom/RemoveRoom은 구조적으로 유사하나 비즈니스 로직이 다름
func (h *RoomAPIHandler) RemoveRoom(c *gin.Context) {
	if h.acl == nil {
		c.JSON(503, gin.H{"error": "ACL service not available"})
		return
	}

	var req struct {
		Room string `json:"room" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Warn("Invalid request body", slog.Any("error", err))
		c.JSON(400, gin.H{"error": "invalid request body"})

		return
	}

	ctx := c.Request.Context()

	removed, err := h.acl.RemoveRoom(ctx, req.Room)
	if err != nil {
		h.logger.Error("Failed to remove room", slog.String("room", req.Room), slog.Any("error", err))
		c.JSON(500, gin.H{"error": "Failed to remove room"})

		return
	}

	if !removed {
		c.JSON(404, gin.H{"error": "Room not found"})
		return
	}

	c.JSON(200, gin.H{
		"status":  "ok",
		"message": "Room removed successfully",
	})

	h.activity.Log("room_remove", "Room removed from ACL list: "+req.Room, map[string]any{"room": req.Room})
}

func (h *RoomAPIHandler) SetACL(c *gin.Context) {
	if h.acl == nil {
		c.JSON(503, gin.H{"error": "ACL service not available"})
		return
	}

	var req struct {
		Enabled *bool   `json:"enabled"`
		Mode    *string `json:"mode"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Warn("Invalid request body", slog.Any("error", err))
		c.JSON(400, gin.H{"error": "invalid request body"})

		return
	}

	if req.Enabled == nil && req.Mode == nil {
		c.JSON(400, gin.H{"error": "at least one of 'enabled' or 'mode' must be provided"})
		return
	}

	ctx := c.Request.Context()

	// enabled 변경
	if req.Enabled != nil {
		if err := h.acl.SetEnabled(ctx, *req.Enabled); err != nil {
			h.logger.Error("Failed to set ACL enabled", slog.Bool("enabled", *req.Enabled), slog.Any("error", err))
			c.JSON(500, gin.H{"error": "Failed to set ACL enabled"})

			return
		}
	}

	// mode 변경
	if req.Mode != nil {
		mode := acl.ParseACLMode(*req.Mode)
		if err := h.acl.SetMode(ctx, mode); err != nil {
			h.logger.Error("Failed to set ACL mode", slog.String("mode", *req.Mode), slog.Any("error", err))
			c.JSON(500, gin.H{"error": "Failed to set ACL mode"})

			return
		}
	}

	// 최종 상태 조회
	enabled, mode, _ := h.acl.GetACLStatus()
	h.logger.Info("Room ACL updated", slog.Bool("enabled", enabled), slog.String("mode", string(mode)))

	h.activity.Log("acl_update", fmt.Sprintf("Room ACL updated: enabled=%v, mode=%s", enabled, mode), map[string]any{"enabled": enabled, "mode": string(mode)})
	c.JSON(200, gin.H{
		"status":  "ok",
		"message": "ACL setting updated successfully",
		"enabled": enabled,
		"mode":    string(mode),
	})
}
