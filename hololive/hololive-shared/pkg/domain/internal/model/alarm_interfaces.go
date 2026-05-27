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

package model

import (
	"context"
	"time"
)

type AddAlarmRequest struct {
	Ctx        context.Context
	RoomID     string
	UserID     string
	ChannelID  string
	MemberName string
	RoomName   string
	UserName   string
	AlarmTypes AlarmTypes
}

type AlarmEntry struct {
	RoomID     string `json:"roomId"`
	RoomName   string `json:"roomName"`
	ChannelID  string `json:"channelId"`
	MemberName string `json:"memberName"`
}

type AlarmListView struct {
	ChannelID  string
	MemberName string
	AlarmTypes AlarmTypes
	NextStream *NextStreamInfo
}

type AlarmRepository interface {
	AddAlarm(ctx context.Context, req AddAlarmRequest) (bool, error)
	RemoveAlarm(ctx context.Context, roomID, channelID string, alarmTypes AlarmTypes) (bool, error)
	GetRoomAlarms(ctx context.Context, roomID string) ([]string, error)
	GetRoomAlarmsWithTypes(ctx context.Context, roomID string) ([]*Alarm, error)
	ListRoomAlarmsView(ctx context.Context, roomID string) ([]AlarmListView, error)
	ClearRoomAlarms(ctx context.Context, roomID string) (int, error)
	GetAllAlarmKeys(ctx context.Context) ([]*AlarmEntry, error)
}

type AlarmCache interface {
	WarmCacheFromDB(ctx context.Context) error
	SetRoomName(ctx context.Context, roomID, roomName string) error
	SetUserName(ctx context.Context, userID, userName string) error
}

type AlarmStateManager interface {
	GetNextStreamInfo(ctx context.Context, channelID string) (*NextStreamInfo, error)
	UpdateAlarmAdvanceMinutes(ctx context.Context, minutes int) []int
	GetTargetMinutes() []int
}

type AlarmCRUD interface {
	AlarmRepository
	AlarmCache
	AlarmStateManager
}

type AlarmDispatchState interface {
	MarkAsNotified(ctx context.Context, streamID string, startScheduled time.Time, minutesUntil int) error
	MarkUpcomingEventNotified(ctx context.Context, roomID, channelID string, stream *Stream) error
	// GetDistinctRooms: 마일스톤 알람 발송 대상 방 ID 목록 조회
	GetDistinctRooms(ctx context.Context) ([]string, error)
}
