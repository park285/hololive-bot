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
	"errors"
	"fmt"
	"log/slog"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/park285/iris-client-go/v3/iris"
	"github.com/park285/shared-go/v2/pkg/ginjson"

	"github.com/kapu/hololive-api/internal/service/acl"
	sharedserver "github.com/kapu/hololive-shared/pkg/server/httpserver"
)

// aclBotResyncFailedCode는 같은 프로세스의 봇 plane ACL 재동기화(PG reload)가 실패했음을 뜻하는 안정 오류 코드다
// (acl.ErrACLPropagation). 요청한 변경은 PostgreSQL에 저장됐을 수도 있고(첫 요청), 이미 같은 상태라 쓰기 없이
// 재동기화만 시도했을 수도 있다(재시도·중복 요청). 같은 요청을 다시 보내면 PG 쓰기 없이 재동기화를 다시 시도하고
// 원래 결과(200 no-op/404/409)를 돌려준다.
const aclBotResyncFailedCode = "acl_bot_resync_failed"

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
	if errors.Is(err, acl.ErrInvalidRoomChatID) {
		// 방 이름 같은 비-chatID 값은 저장해도 어떤 방에도 적용되지 않으므로 입력 오류로 돌려준다.
		sharedserver.RespondError(c, 400, "room must be a chat ID (signed 64-bit integer)", nil)

		return
	}

	if added {
		// 봇 plane 적용이 실패해도 PG에는 커밋됐으므로 활동 로그는 남긴다(재시도는 409라 다시 남지 않는다).
		h.logActivity("room_add", "Room added to ACL list: "+req.Room, map[string]any{"room": req.Room})
	}

	if err != nil {
		h.respondACLMutationError(ctx, c, err, "Failed to add room", slog.String("room", req.Room))

		return
	}

	if !added {
		sharedserver.RespondError(c, 409, "Room already exists", nil)

		return
	}

	ginjson.Respond(c, 200, statusMessageResponse{Status: "ok", Message: "Room added successfully"})
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
	if removed {
		// AddRoom과 같다: PG 커밋 뒤 봇 plane 적용 실패여도 활동 로그는 남긴다.
		h.logActivity("room_remove", "Room removed from ACL list: "+req.Room, map[string]any{"room": req.Room})
	}

	if err != nil {
		h.respondACLMutationError(ctx, c, err, "Failed to remove room", slog.String("room", req.Room))

		return
	}

	if !removed {
		sharedserver.RespondError(c, 404, "Room not found", nil)

		return
	}

	ginjson.Respond(c, 200, statusMessageResponse{Status: "ok", Message: "Room removed successfully"})
}

// respondACLMutationError는 ACL mutation 오류를 500으로 응답한다. 봇 plane 재동기화 실패는 저장 실패와 구분되는
// aclBotResyncFailedCode로 알려, 호출자가 같은 요청 재시도로 수렴시키게 한다.
func (h *RoomHandler) respondACLMutationError(ctx context.Context, c *gin.Context, err error, failMessage string, attrs ...slog.Attr) {
	attrs = append(attrs, slog.Any("error", err))

	if errors.Is(err, acl.ErrACLPropagation) {
		h.safeLogger().LogAttrs(ctx, slog.LevelError, "ACL bot-plane resync failed", attrs...)
		sharedserver.RespondError(c, 500, aclBotResyncFailedCode, gin.H{
			"message": "the bot has not applied the current ACL (the requested change may already be saved); retry the same request",
		})

		return
	}

	h.safeLogger().LogAttrs(ctx, slog.LevelError, failMessage, attrs...)
	sharedserver.RespondError(c, 500, failMessage, nil)
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

	beforeEnabled, beforeMode, _ := h.acl.GetACLStatus()

	if h.setACLEnabled(ctx, c, req.Enabled) && h.setACLMode(ctx, c, req.Mode, mode) {
		return true
	}

	// 오류 응답을 보낸 뒤에도 이미 커밋된 부분(예: enabled만 저장되고 mode 저장·재동기화 실패)은 감사 기록에 남긴다.
	// 호출자가 재시도하지 않으면 이 변경의 activity가 남지 않기 때문이다.
	h.logCommittedACLChange(beforeEnabled, beforeMode)

	return false
}

func (h *RoomHandler) logCommittedACLChange(beforeEnabled bool, beforeMode acl.ACLMode) {
	enabled, mode, _ := h.acl.GetACLStatus()
	if enabled == beforeEnabled && mode == beforeMode {
		return
	}

	h.logActivity("acl_update", fmt.Sprintf("Room ACL updated: enabled=%v, mode=%s (bot resync or later step failed)", enabled, mode),
		map[string]any{"enabled": enabled, "mode": string(mode), "partial": true})
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
		h.respondACLMutationError(ctx, c, err, "Failed to set ACL enabled", slog.Bool("enabled", *enabled))

		return false
	}

	return true
}

func (h *RoomHandler) setACLMode(ctx context.Context, c *gin.Context, rawMode *string, mode acl.ACLMode) bool {
	if rawMode == nil {
		return true
	}

	if err := h.acl.SetMode(ctx, mode); err != nil {
		h.respondACLMutationError(ctx, c, err, "Failed to set ACL mode", slog.String("mode", *rawMode))

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
