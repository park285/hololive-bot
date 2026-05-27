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
