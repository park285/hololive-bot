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
	"net/url"

	keyspkg "github.com/kapu/hololive-shared/pkg/service/alarm/keys"
)

const (
	BasePath = "/internal/alarm"

	AddRoute        = "/add"
	RemoveRoute     = "/remove"
	RoomRoute       = "/room/:id"
	RoomViewRoute   = "/room/:id/view"
	ClearRoute      = "/clear"
	NextStreamRoute = "/next-stream/:id"
	SettingsRoute   = "/settings"
	RoomNameRoute   = "/room-name"
	UserNameRoute   = "/user-name"
	KeysRoute       = "/keys"

	AddPath      = BasePath + AddRoute
	RemovePath   = BasePath + RemoveRoute
	ClearPath    = BasePath + ClearRoute
	SettingsPath = BasePath + SettingsRoute
	RoomNamePath = BasePath + RoomNameRoute
	UserNamePath = BasePath + UserNameRoute
	KeysPath     = BasePath + KeysRoute
)

const (
	DispatchQueueKey      = keyspkg.DispatchQueueKey
	DispatchRetryQueueKey = keyspkg.DispatchRetryQueueKey
	DispatchDLQKey        = keyspkg.DispatchDLQKey

	NotifyClaimKeyPrefix        = keyspkg.NotifyClaimKeyPrefix
	NotifyLogicalClaimKeyPrefix = keyspkg.NotifyLogicalClaimKeyPrefix

	QueueEnvelopeVersionV1 uint8 = 1
)

func RoomAlarmsPath(roomID string) string {
	return BasePath + "/room/" + url.PathEscape(roomID)
}

func RoomAlarmsViewPath(roomID string) string {
	return BasePath + "/room/" + url.PathEscape(roomID) + "/view"
}

func NextStreamPath(channelID string) string {
	return BasePath + "/next-stream/" + url.PathEscape(channelID)
}
