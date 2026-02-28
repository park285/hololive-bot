package domain

import (
	"context"
	"time"
)

// AddAlarmRequest: 알람 등록 요청 DTO
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

// AlarmEntry: 관리자 대시보드용 알람 엔트리
type AlarmEntry struct {
	RoomID     string `json:"roomId"`
	RoomName   string `json:"roomName"`
	ChannelID  string `json:"channelId"`
	MemberName string `json:"memberName"`
}

// AlarmCRUD: 커맨드와 Admin API에서 사용하는 알람 CRUD 인터페이스
type AlarmCRUD interface {
	AddAlarm(ctx context.Context, req AddAlarmRequest) (bool, error)
	RemoveAlarm(ctx context.Context, roomID, channelID string, alarmTypes AlarmTypes) (bool, error)
	GetRoomAlarms(ctx context.Context, roomID string) ([]string, error)
	GetRoomAlarmsWithTypes(ctx context.Context, roomID string) ([]*Alarm, error)
	ClearRoomAlarms(ctx context.Context, roomID string) (int, error)
	GetNextStreamInfo(ctx context.Context, channelID string) (*NextStreamInfo, error)
	GetMemberNameWithFallback(ctx context.Context, channelID string) string
	UpdateAlarmAdvanceMinutes(minutes int) []int
	GetTargetMinutes() []int
	SetRoomName(ctx context.Context, roomID, roomName string) error
	SetUserName(ctx context.Context, userID, userName string) error
	GetAllAlarmKeys(ctx context.Context) ([]*AlarmEntry, error)
	WarmCacheFromDB(ctx context.Context) error
}

// AlarmDispatchState: 큐 디스패처에서 사용하는 발송 상태 인터페이스
type AlarmDispatchState interface {
	MarkAsNotified(ctx context.Context, streamID string, startScheduled time.Time, minutesUntil int) error
	MarkUpcomingEventNotified(ctx context.Context, roomID, channelID string, stream *Stream) error
	// GetDistinctRooms: 마일스톤 알람 발송 대상 방 ID 목록 조회
	GetDistinctRooms(ctx context.Context) ([]string, error)
}

// StreamProvider: 커맨드에서 사용하는 Holodex 스트림 조회 인터페이스
type StreamProvider interface {
	GetLiveStreams(ctx context.Context) ([]*Stream, error)
	GetUpcomingStreams(ctx context.Context, hours int) ([]*Stream, error)
	GetChannelSchedule(ctx context.Context, channelID string, hours int, includeLive bool) ([]*Stream, error)
	GetChannel(ctx context.Context, channelID string) (*Channel, error)
}
