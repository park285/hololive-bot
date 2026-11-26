package alarm

import (
	"errors"
	"net/url"
	"strings"
	"unicode/utf8"
)

const (
	BasePath = "/internal/alarm"

	AddRoute       = "/add"
	RemoveRoute    = "/remove"
	RoomViewRoute  = "/room/:id/view"
	ClearRoute     = "/clear"
	SettingsRoute  = "/settings"
	RoomNameRoute  = "/room-name"
	KeysRoute      = "/keys"
	CountRoute     = "/count"
	LiveRoomsRoute = "/channel/:id/live-rooms"

	AddPath      = BasePath + AddRoute
	RemovePath   = BasePath + RemoveRoute
	ClearPath    = BasePath + ClearRoute
	SettingsPath = BasePath + SettingsRoute
	RoomNamePath = BasePath + RoomNameRoute
	KeysPath     = BasePath + KeysRoute
	CountPath    = BasePath + CountRoute
)

type EntryCount struct {
	Count *int `json:"count"`
}

const QueueEnvelopeVersionV1 uint8 = 1

const (
	MaxRoomIDLength   = 100
	MaxRoomNameLength = 255
)

var (
	ErrRoomIDRequired  = errors.New("room id is required")
	ErrRoomIDTooLong   = errors.New("room id exceeds 100 characters")
	ErrRoomNameTooLong = errors.New("room name exceeds 255 characters")
)

func NormalizeRoomName(roomID, roomName string) (string, string, error) {
	roomID = strings.TrimSpace(roomID)
	if roomID == "" {
		return "", "", ErrRoomIDRequired
	}

	if utf8.RuneCountInString(roomID) > MaxRoomIDLength {
		return "", "", ErrRoomIDTooLong
	}

	roomName = strings.TrimSpace(roomName)
	if utf8.RuneCountInString(roomName) > MaxRoomNameLength {
		return "", "", ErrRoomNameTooLong
	}

	return roomID, roomName, nil
}

type LiveRoomsResponse struct {
	RoomIDs []string `json:"room_ids"`
}

func LiveRoomsPath(channelID string) string {
	return BasePath + "/channel/" + url.PathEscape(channelID) + "/live-rooms"
}

func RoomAlarmsViewPath(roomID string) string {
	return BasePath + "/room/" + url.PathEscape(roomID) + "/view"
}
