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

package alarm

import (
	"errors"
	"net/url"
	"strings"
	"unicode/utf8"
)

const (
	BasePath = "/internal/alarm"

	AddRoute      = "/add"
	RemoveRoute   = "/remove"
	RoomRoute     = "/room/:id"
	RoomViewRoute = "/room/:id/view"
	ClearRoute    = "/clear"
	SettingsRoute = "/settings"
	RoomNameRoute = "/room-name"
	KeysRoute     = "/keys"
	CountRoute    = "/count"

	AddPath      = BasePath + AddRoute
	RemovePath   = BasePath + RemoveRoute
	ClearPath    = BasePath + ClearRoute
	SettingsPath = BasePath + SettingsRoute
	RoomNamePath = BasePath + RoomNameRoute
	KeysPath     = BasePath + KeysRoute
	CountPath    = BasePath + CountRoute
)

// EntryCount는 관리 목록과 같은 서로 다른 방·채널 쌍의 개수다. 개별 host 구독 수와 구분한다.
type EntryCount struct {
	Count *int `json:"count"`
}

const QueueEnvelopeVersionV1 uint8 = 1

// 방 이름 요청 폭은 alarms.room_id VARCHAR(100)·alarm_room_display_names.display_name VARCHAR(255)를 따른다.
// PG varchar 폭은 문자 수라 rune으로 센다.
const (
	MaxRoomIDLength   = 100
	MaxRoomNameLength = 255
)

var (
	ErrRoomIDRequired  = errors.New("room id is required")
	ErrRoomIDTooLong   = errors.New("room id exceeds 100 characters")
	ErrRoomNameTooLong = errors.New("room name exceeds 255 characters")
)

// NormalizeRoomName은 방 이름 설정 요청의 앞뒤 공백을 제거하고 저장 폭을 넘는 값을 거절한다.
// 공백뿐인 이름은 빈 문자열(관리자 지정 해제)로 돌려준다.
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

func RoomAlarmsPath(roomID string) string {
	return BasePath + "/room/" + url.PathEscape(roomID)
}

func RoomAlarmsViewPath(roomID string) string {
	return BasePath + "/room/" + url.PathEscape(roomID) + "/view"
}
