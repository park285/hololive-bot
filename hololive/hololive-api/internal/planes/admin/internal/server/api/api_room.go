package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/park285/iris-client-go/v6/iris"

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

type roomChangeRequest struct {
	Room string `json:"room" binding:"required"`
	Mode string `json:"mode" binding:"required"`
}

type roomChangeResponse struct {
	Status  string `json:"status"`
	Message string `json:"message"`
	Changed bool   `json:"changed"`
}

const aclModeChangedCode = "acl_mode_changed"

func (h *RoomHandler) AddRoom(c *gin.Context) {
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
		sharedserver.RespondError(c, http.StatusBadRequest, "room must be a chat ID (signed 64-bit integer)", nil)
	case errors.Is(err, acl.ErrACLModeChanged):
		sharedserver.RespondError(c, http.StatusConflict, "ACL mode changed", gin.H{"code": aclModeChangedCode})
	case err != nil:
		h.respondACLMutationError(ctx, c, err, change.failMessage, slog.String("room", req.Room))
	case !changed:
		ginjson.Respond(c, http.StatusOK, roomChangeResponse{Status: "ok", Message: change.unchangedMessage, Changed: false})
	default:
		h.logActivity(change.activity, change.activityPrefix, []slog.Attr{slog.String("room", req.Room)})
		ginjson.Respond(c, http.StatusOK, roomChangeResponse{Status: "ok", Message: change.changedMessage, Changed: true})
	}
}

func (h *RoomHandler) respondACLMutationError(ctx context.Context, c *gin.Context, err error, failMessage string, attrs ...slog.Attr) {
	attrs = append(attrs, slog.Any("error", err))

	h.safeLogger().LogAttrs(ctx, slog.LevelError, failMessage, attrs...)
	sharedserver.RespondError(c, http.StatusInternalServerError, failMessage, nil)
}

func (h *RoomHandler) SetACL(c *gin.Context) {
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

	h.logCommittedACLChange(beforeEnabled, beforeMode)

	return false
}

func (h *RoomHandler) logCommittedACLChange(beforeEnabled bool, beforeMode acl.ACLMode) {
	enabled, mode, _ := h.acl.GetACLStatus()
	if enabled == beforeEnabled && mode == beforeMode {
		return
	}

	h.logActivity("acl_update", "Room ACL updated before later step failed",
		[]slog.Attr{slog.Bool("enabled", enabled), slog.String("mode", string(mode)), slog.Bool("partial", true)})
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

	h.logActivity("acl_update", "Room ACL updated", []slog.Attr{slog.Bool("enabled", enabled), slog.String("mode", string(mode))})
	ginjson.Respond(c, http.StatusOK, setACLResponse{
		Status:  "ok",
		Message: "ACL setting updated successfully",
		Enabled: enabled,
		Mode:    string(mode),
	})
}
