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
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/park285/iris-client-go/v4/iris"

	"github.com/kapu/hololive-api/internal/service/acl"
	"github.com/kapu/hololive-shared/pkg/server/ginjson"
	sharedserver "github.com/kapu/hololive-shared/pkg/server/httpserver"
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
	ginjson.Respond(c, http.StatusOK, roomListResponse{
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
		sharedserver.RespondError(c, http.StatusBadGateway, "Failed to list joined rooms", nil)

		return
	}

	if resp == nil {
		h.safeLogger().Error("Failed to list joined rooms from Iris", slog.String("error", "nil response"))
		sharedserver.RespondError(c, http.StatusBadGateway, "Failed to list joined rooms", nil)

		return
	}

	ginjson.Respond(c, http.StatusOK, joinedRoomListResponse{Status: "ok", Rooms: joinedRoomsFromIris(resp.Rooms)})
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

// roomChangeRequest의 mode는 호출자가 본 ACL 모드다. 현재 모드와 다르면 409 acl_mode_changed로 쓰지 않고 거절한다.
type roomChangeRequest struct {
	Room string `json:"room" binding:"required"`
	Mode string `json:"mode" binding:"required"`
}

// roomChangeResponse의 changed는 이번 요청이 목록을 바꿨는지다. 이미 원하는 상태(추가할 방이 있음, 제거할 방이 없음)면
// 200과 changed:false로 수렴하므로, 응답을 잃은 뒤 같은 요청을 다시 보내도 거절로 보이지 않는다.
type roomChangeResponse struct {
	Status  string `json:"status"`
	Message string `json:"message"`
	Changed bool   `json:"changed"`
}

const aclModeChangedCode = "acl_mode_changed"

func (h *RoomHandler) AddRoom(c *gin.Context) {
	if !h.requireACL(c) {
		return
	}

	h.changeRoom(c, roomChange{
		apply:            h.acl.AddRoom,
		failMessage:      "Failed to add room",
		activity:         "room_add",
		activityPrefix:   "Room added to ACL list: ",
		changedMessage:   "Room added successfully",
		unchangedMessage: "Room already exists",
	})
}

func (h *RoomHandler) RemoveRoom(c *gin.Context) {
	if !h.requireACL(c) {
		return
	}

	h.changeRoom(c, roomChange{
		apply:            h.acl.RemoveRoom,
		failMessage:      "Failed to remove room",
		activity:         "room_remove",
		activityPrefix:   "Room removed from ACL list: ",
		changedMessage:   "Room removed successfully",
		unchangedMessage: "Room not found",
	})
}

type roomChange struct {
	apply            func(context.Context, acl.ACLMode, string) (bool, error)
	failMessage      string
	activity         string
	activityPrefix   string
	changedMessage   string
	unchangedMessage string
}

func (h *RoomHandler) changeRoom(c *gin.Context, change roomChange) {
	var req roomChangeRequest

	if err := bindJSON(c, &req); err != nil {
		h.safeLogger().Warn("Invalid request body", slog.Any("error", err))
		sharedserver.RespondError(c, http.StatusBadRequest, "invalid request body", nil)

		return
	}

	mode, ok := h.parseACLMode(c, &req.Mode)
	if !ok {
		return
	}

	ctx := c.Request.Context()

	changed, err := change.apply(ctx, mode, req.Room)

	switch {
	case errors.Is(err, acl.ErrInvalidRoomChatID):
		// 방 이름 같은 비-chatID 값은 저장해도 어떤 방에도 적용되지 않으므로 입력 오류로 돌려준다.
		sharedserver.RespondError(c, http.StatusBadRequest, "room must be a chat ID (signed 64-bit integer)", nil)
	case errors.Is(err, acl.ErrACLModeChanged):
		sharedserver.RespondError(c, http.StatusConflict, "ACL mode changed", gin.H{"code": aclModeChangedCode})
	case err != nil:
		h.respondACLMutationError(ctx, c, err, change.failMessage, slog.String("room", req.Room))
	case !changed:
		ginjson.Respond(c, http.StatusOK, roomChangeResponse{Status: "ok", Message: change.unchangedMessage, Changed: false})
	default:
		h.logActivity(change.activity, change.activityPrefix+req.Room, map[string]any{"room": req.Room})
		ginjson.Respond(c, http.StatusOK, roomChangeResponse{Status: "ok", Message: change.changedMessage, Changed: true})
	}
}

// respondACLMutationError는 ACL mutation 오류를 500으로 응답한다.
func (h *RoomHandler) respondACLMutationError(ctx context.Context, c *gin.Context, err error, failMessage string, attrs ...slog.Attr) {
	attrs = append(attrs, slog.Any("error", err))

	h.safeLogger().LogAttrs(ctx, slog.LevelError, failMessage, attrs...)
	sharedserver.RespondError(c, http.StatusInternalServerError, failMessage, nil)
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
		sharedserver.RespondError(c, http.StatusBadRequest, "invalid request body", nil)

		return setACLRequest{}, false
	}

	if req.Enabled == nil && req.Mode == nil {
		sharedserver.RespondError(c, http.StatusBadRequest, "at least one of 'enabled' or 'mode' must be provided", nil)

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

	// 오류 응답을 보낸 뒤에도 이미 커밋된 부분(예: enabled만 저장되고 mode 저장 실패)은 감사 기록에 남긴다.
	// 호출자가 재시도하지 않으면 이 변경의 activity가 남지 않기 때문이다.
	h.logCommittedACLChange(beforeEnabled, beforeMode)

	return false
}

func (h *RoomHandler) logCommittedACLChange(beforeEnabled bool, beforeMode acl.ACLMode) {
	enabled, mode, _ := h.acl.GetACLStatus()
	if enabled == beforeEnabled && mode == beforeMode {
		return
	}

	h.logActivity("acl_update", fmt.Sprintf("Room ACL updated: enabled=%v, mode=%s (later step failed)", enabled, mode),
		map[string]any{"enabled": enabled, "mode": string(mode), "partial": true})
}

func (h *RoomHandler) parseACLMode(c *gin.Context, rawMode *string) (acl.ACLMode, bool) {
	if rawMode == nil {
		return "", true
	}

	mode, err := acl.ParseACLModeStrict(*rawMode)
	if err != nil {
		h.safeLogger().Warn("Invalid ACL mode", slog.String("mode", *rawMode), slog.Any("error", err))
		sharedserver.RespondError(c, http.StatusBadRequest, "invalid ACL mode", nil)

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
	ginjson.Respond(c, http.StatusOK, setACLResponse{
		Status:  "ok",
		Message: "ACL setting updated successfully",
		Enabled: enabled,
		Mode:    string(mode),
	})
}
