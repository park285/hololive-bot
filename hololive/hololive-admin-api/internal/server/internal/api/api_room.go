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

package api

import (
	"fmt"
	sharedserver "github.com/kapu/hololive-shared/pkg/server"
	"log/slog"

	"github.com/gin-gonic/gin"

	"github.com/kapu/hololive-shared/pkg/service/acl"
)

type setACLRequest struct {
	Enabled *bool   `json:"enabled"`
	Mode    *string `json:"mode"`
}

func (h *RoomHandler) GetRooms(c *gin.Context) {
	if !h.requireACL(c) {
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
func (h *RoomHandler) AddRoom(c *gin.Context) {
	if !h.requireACL(c) {
		return
	}

	var req struct {
		Room string `json:"room" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		h.safeLogger().Warn("Invalid request body", slog.Any("error", err))
		sharedserver.RespondError(c, 400, "invalid request body", nil)

		return
	}

	ctx := c.Request.Context()

	added, err := h.acl.AddRoom(ctx, req.Room)
	if err != nil {
		h.safeLogger().Error("Failed to add room", slog.String("room", req.Room), slog.Any("error", err))
		sharedserver.RespondError(c, 500, "Failed to add room", nil)

		return
	}

	if !added {
		sharedserver.RespondError(c, 409, "Room already exists", nil)
		return
	}

	c.JSON(200, gin.H{
		"status":  "ok",
		"message": "Room added successfully",
	})

	h.logActivity("room_add", "Room added to ACL list: "+req.Room, map[string]any{"room": req.Room})
}

//
//nolint:dupl // AddRoom/RemoveRoom은 구조적으로 유사하나 비즈니스 로직이 다름
func (h *RoomHandler) RemoveRoom(c *gin.Context) {
	if !h.requireACL(c) {
		return
	}

	var req struct {
		Room string `json:"room" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		h.safeLogger().Warn("Invalid request body", slog.Any("error", err))
		sharedserver.RespondError(c, 400, "invalid request body", nil)

		return
	}

	ctx := c.Request.Context()

	removed, err := h.acl.RemoveRoom(ctx, req.Room)
	if err != nil {
		h.safeLogger().Error("Failed to remove room", slog.String("room", req.Room), slog.Any("error", err))
		sharedserver.RespondError(c, 500, "Failed to remove room", nil)

		return
	}

	if !removed {
		sharedserver.RespondError(c, 404, "Room not found", nil)
		return
	}

	c.JSON(200, gin.H{
		"status":  "ok",
		"message": "Room removed successfully",
	})

	h.logActivity("room_remove", "Room removed from ACL list: "+req.Room, map[string]any{"room": req.Room})
}

func (h *RoomHandler) SetACL(c *gin.Context) {
	if !h.requireACL(c) {
		return
	}

	req, ok := h.bindSetACLRequest(c)
	if !ok {
		return
	}

	if !h.applyACLSettings(c, req) {
		return
	}

	h.respondSetACL(c)
}

func (h *RoomHandler) bindSetACLRequest(c *gin.Context) (setACLRequest, bool) {
	var req setACLRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.safeLogger().Warn("Invalid request body", slog.Any("error", err))
		sharedserver.RespondError(c, 400, "invalid request body", nil)

		return setACLRequest{}, false
	}

	if req.Enabled == nil && req.Mode == nil {
		sharedserver.RespondError(c, 400, "at least one of 'enabled' or 'mode' must be provided", nil)
		return setACLRequest{}, false
	}

	return req, true
}

func (h *RoomHandler) applyACLSettings(c *gin.Context, req setACLRequest) bool {
	ctx := c.Request.Context()
	if req.Enabled != nil {
		if err := h.acl.SetEnabled(ctx, *req.Enabled); err != nil {
			h.safeLogger().Error("Failed to set ACL enabled", slog.Bool("enabled", *req.Enabled), slog.Any("error", err))
			sharedserver.RespondError(c, 500, "Failed to set ACL enabled", nil)

			return false
		}
	}

	if req.Mode != nil {
		mode := acl.ParseACLMode(*req.Mode)
		if err := h.acl.SetMode(ctx, mode); err != nil {
			h.safeLogger().Error("Failed to set ACL mode", slog.String("mode", *req.Mode), slog.Any("error", err))
			sharedserver.RespondError(c, 500, "Failed to set ACL mode", nil)

			return false
		}
	}

	return true
}

func (h *RoomHandler) respondSetACL(c *gin.Context) {
	enabled, mode, _ := h.acl.GetACLStatus()
	h.safeLogger().Info("Room ACL updated", slog.Bool("enabled", enabled), slog.String("mode", string(mode)))

	h.logActivity("acl_update", fmt.Sprintf("Room ACL updated: enabled=%v, mode=%s", enabled, mode), map[string]any{"enabled": enabled, "mode": string(mode)})
	c.JSON(200, gin.H{
		"status":  "ok",
		"message": "ACL setting updated successfully",
		"enabled": enabled,
		"mode":    string(mode),
	})
}
