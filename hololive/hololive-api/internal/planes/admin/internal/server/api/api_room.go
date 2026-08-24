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
	"context"
	"fmt"
	"log/slog"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/park285/iris-client-go/v2/iris"
	"github.com/park285/shared-go/v2/pkg/ginjson"

	sharedserver "github.com/kapu/hololive-shared/pkg/server/httpserver"
	"github.com/kapu/hololive-shared/pkg/service/acl"
	"github.com/kapu/hololive-shared/pkg/service/configsub"
)

type setACLRequest struct {
	Enabled *bool   `json:"enabled"`
	Mode    *string `json:"mode"`
}

type roomListResponse struct {
	Status     string   `json:"status"`
	Rooms      []string `json:"rooms"`
	ACLEnabled bool     `json:"aclEnabled"`
	ACLMode    string   `json:"aclMode"`
}

type setACLResponse struct {
	Status  string `json:"status"`
	Message string `json:"message"`
	Enabled bool   `json:"enabled"`
	Mode    string `json:"mode"`
}

func (h *RoomHandler) GetRooms(c *gin.Context) {
	if !h.requireACL(c) {
		return
	}

	aclEnabled, mode, rooms := h.acl.GetACLStatus()
	ginjson.Respond(c, 200, roomListResponse{
		Status:     "ok",
		Rooms:      rooms,
		ACLEnabled: aclEnabled,
		ACLMode:    string(mode),
	})
}

type joinedRoom struct {
	ChatID      string `json:"chatId"`
	Name        string `json:"name"`
	Type        string `json:"type"`
	MemberCount int    `json:"memberCount"`
}

type joinedRoomListResponse struct {
	Status string       `json:"status"`
	Rooms  []joinedRoom `json:"rooms"`
}

func (h *RoomHandler) GetJoinedRooms(c *gin.Context) {
	if !h.requireIris(c) {
		return
	}

	resp, err := h.iris.GetRooms(c.Request.Context())
	if err != nil {
		h.safeLogger().Error("Failed to list joined rooms from Iris", slog.Any("error", err))
		sharedserver.RespondError(c, 502, "Failed to list joined rooms", nil)

		return
	}

	if resp == nil {
		h.safeLogger().Error("Failed to list joined rooms from Iris", slog.String("error", "nil response"))
		sharedserver.RespondError(c, 502, "Failed to list joined rooms", nil)

		return
	}

	ginjson.Respond(c, 200, joinedRoomListResponse{Status: "ok", Rooms: joinedRoomsFromIris(resp.Rooms)})
}

func joinedRoomsFromIris(summaries []iris.RoomSummary) []joinedRoom {
	rooms := make([]joinedRoom, 0, len(summaries))
	for _, summary := range summaries {
		rooms = append(rooms, joinedRoomFromIris(summary))
	}

	return rooms
}

func joinedRoomFromIris(summary iris.RoomSummary) joinedRoom {
	room := joinedRoom{ChatID: strconv.FormatInt(summary.ChatID, 10)}
	if summary.LinkName != nil {
		room.Name = *summary.LinkName
	}

	if summary.Type != nil {
		room.Type = *summary.Type
	}

	if summary.ActiveMembersCount != nil {
		room.MemberCount = *summary.ActiveMembersCount
	}

	return room
}

func (h *RoomHandler) AddRoom(c *gin.Context) {
	if !h.requireACL(c) {
		return
	}

	var req struct {
		Room string `json:"room" binding:"required"`
	}

	if err := bindJSON(c, &req); err != nil {
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

	h.publishACLChange(ctx, "room_add", req.Room, "")

	ginjson.Respond(c, 200, statusMessageResponse{Status: "ok", Message: "Room added successfully"})

	h.logActivity("room_add", "Room added to ACL list: "+req.Room, map[string]any{"room": req.Room})
}

func (h *RoomHandler) RemoveRoom(c *gin.Context) {
	if !h.requireACL(c) {
		return
	}

	var req struct {
		Room string `json:"room" binding:"required"`
	}

	if err := bindJSON(c, &req); err != nil {
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

	h.publishACLChange(ctx, "room_remove", req.Room, "")

	ginjson.Respond(c, 200, statusMessageResponse{Status: "ok", Message: "Room removed successfully"})

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

	_, mode, _ := h.acl.GetACLStatus()
	h.publishACLChange(c.Request.Context(), "acl_update", "", string(mode))

	h.respondSetACL(c)
}

// 발행 실패는 요청을 실패시키지 않는다 — DB 반영은 이미 끝났고, 통지를 놓친 복제본은
// 다음 기동 때 DB에서 다시 읽어 수렴한다.
func (h *RoomHandler) publishACLChange(ctx context.Context, reason, room, mode string) {
	if h.valkeyCache == nil {
		return
	}

	if err := configsub.NewPublisher(h.valkeyCache.GetClient()).PublishACL(ctx, reason, room, mode); err != nil {
		h.safeLogger().Warn("Failed to publish ACL change",
			slog.String("reason", reason),
			slog.Any("error", err),
		)
	}
}

func (h *RoomHandler) bindSetACLRequest(c *gin.Context) (setACLRequest, bool) {
	var req setACLRequest

	if err := bindJSON(c, &req); err != nil {
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
	mode, ok := h.parseACLMode(c, req.Mode)

	if !ok {
		return false
	}

	if !h.setACLEnabled(ctx, c, req.Enabled) {
		return false
	}

	return h.setACLMode(ctx, c, req.Mode, mode)
}

func (h *RoomHandler) parseACLMode(c *gin.Context, rawMode *string) (acl.ACLMode, bool) {
	if rawMode == nil {
		return "", true
	}

	mode, err := acl.ParseACLModeStrict(*rawMode)
	if err != nil {
		h.safeLogger().Warn("Invalid ACL mode", slog.String("mode", *rawMode), slog.Any("error", err))
		sharedserver.RespondError(c, 400, "invalid ACL mode", nil)

		return "", false
	}

	return mode, true
}

func (h *RoomHandler) setACLEnabled(ctx context.Context, c *gin.Context, enabled *bool) bool {
	if enabled == nil {
		return true
	}

	if err := h.acl.SetEnabled(ctx, *enabled); err != nil {
		h.safeLogger().Error("Failed to set ACL enabled", slog.Bool("enabled", *enabled), slog.Any("error", err))
		sharedserver.RespondError(c, 500, "Failed to set ACL enabled", nil)

		return false
	}

	return true
}

func (h *RoomHandler) setACLMode(ctx context.Context, c *gin.Context, rawMode *string, mode acl.ACLMode) bool {
	if rawMode == nil {
		return true
	}

	if err := h.acl.SetMode(ctx, mode); err != nil {
		h.safeLogger().Error("Failed to set ACL mode", slog.String("mode", *rawMode), slog.Any("error", err))
		sharedserver.RespondError(c, 500, "Failed to set ACL mode", nil)

		return false
	}

	return true
}

func (h *RoomHandler) respondSetACL(c *gin.Context) {
	enabled, mode, _ := h.acl.GetACLStatus()
	h.safeLogger().Info("Room ACL updated", slog.Bool("enabled", enabled), slog.String("mode", string(mode)))

	h.logActivity("acl_update", fmt.Sprintf("Room ACL updated: enabled=%v, mode=%s", enabled, mode), map[string]any{"enabled": enabled, "mode": string(mode)})
	ginjson.Respond(c, 200, setACLResponse{
		Status:  "ok",
		Message: "ACL setting updated successfully",
		Enabled: enabled,
		Mode:    string(mode),
	})
}
