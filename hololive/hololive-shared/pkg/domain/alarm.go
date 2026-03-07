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

package domain

import (
	"database/sql/driver"
	"fmt"
	"slices"
	"strings"
	"time"
)

// AlarmType: 알람 종류
type AlarmType string

const (
	// AlarmTypeLive: 방송 시작 알람 (기존 기능)
	AlarmTypeLive AlarmType = "LIVE"
	// AlarmTypeCommunity: 커뮤니티 포스트 알람
	AlarmTypeCommunity AlarmType = "COMMUNITY"
	// AlarmTypeShorts: 쇼츠 영상 알람
	AlarmTypeShorts AlarmType = "SHORTS"
)

// AllAlarmTypes: 모든 알람 타입 목록
var AllAlarmTypes = []AlarmType{AlarmTypeLive, AlarmTypeCommunity, AlarmTypeShorts}

// DefaultAlarmTypes: 기본 알람 타입 (타입 미지정 시 전체)
var DefaultAlarmTypes = AllAlarmTypes

// IsValid: 유효한 알람 타입인지 확인
func (t AlarmType) IsValid() bool {
	switch t {
	case AlarmTypeLive, AlarmTypeCommunity, AlarmTypeShorts:
		return true
	default:
		return false
	}
}

// String: 문자열 표현
func (t AlarmType) String() string {
	return string(t)
}

// DisplayName: 사용자에게 표시할 이름
func (t AlarmType) DisplayName() string {
	switch t {
	case AlarmTypeLive:
		return "방송"
	case AlarmTypeCommunity:
		return "커뮤니티"
	case AlarmTypeShorts:
		return "쇼츠"
	default:
		return string(t)
	}
}

// AlarmTypes: PostgreSQL alarm_type[] 배열 타입
type AlarmTypes []AlarmType

// Value: driver.Valuer 구현 (DB 저장 시)
func (a AlarmTypes) Value() (driver.Value, error) {
	if len(a) == 0 {
		return "{}", nil
	}
	parts := make([]string, len(a))
	for i, t := range a {
		parts[i] = string(t)
	}
	return "{" + strings.Join(parts, ",") + "}", nil
}

// Scan: sql.Scanner 구현 (DB 로드 시)
func (a *AlarmTypes) Scan(value any) error {
	if value == nil {
		*a = nil
		return nil
	}
	var str string
	switch v := value.(type) {
	case []byte:
		str = string(v)
	case string:
		str = v
	default:
		return fmt.Errorf("failed to scan AlarmTypes: expected string or []byte, got %T", value)
	}

	// PostgreSQL 배열 형식 파싱: {LIVE,COMMUNITY,SHORTS}
	str = strings.TrimPrefix(str, "{")
	str = strings.TrimSuffix(str, "}")
	if str == "" {
		*a = nil
		return nil
	}
	parts := strings.Split(str, ",")
	result := make(AlarmTypes, 0, len(parts))
	for _, p := range parts {
		t := AlarmType(strings.TrimSpace(p))
		if t.IsValid() {
			result = append(result, t)
		}
	}
	*a = result
	return nil
}

// Contains: 특정 알람 타입 포함 여부
func (a AlarmTypes) Contains(t AlarmType) bool {
	return slices.Contains(a, t)
}

// Alarm: 특정 채팅방(user)이 특정 멤버(channel)의 방송 알림을 구독한 정보
type Alarm struct {
	ID         int        `json:"id,omitempty"`          // DB 기본 키
	RoomID     string     `json:"room_id"`               // 카카오톡 방 ID
	UserID     string     `json:"user_id"`               // 카카오톡 사용자 ID
	ChannelID  string     `json:"channel_id"`            // YouTube 채널 ID
	MemberName string     `json:"member_name,omitempty"` // 표시용 멤버 이름
	RoomName   string     `json:"room_name,omitempty"`   // 방 이름 (캐싱용)
	UserName   string     `json:"user_name,omitempty"`   // 사용자 이름 (캐싱용)
	AlarmTypes AlarmTypes `json:"alarm_types"`           // 알람 타입 (LIVE, COMMUNITY, SHORTS)
	CreatedAt  time.Time  `json:"created_at"`
}

// RegistryKey: 방 기반 레지스트리 키를 반환합니다. (room_id가 PRIMARY)
func (a *Alarm) RegistryKey() string {
	return a.RoomID
}

// NewAlarm: 새로운 알림 구독 객체를 생성합니다.
func NewAlarm(roomID, userID, channelID, memberName string) *Alarm {
	return &Alarm{
		RoomID:     roomID,
		UserID:     userID,
		ChannelID:  channelID,
		MemberName: memberName,
		CreatedAt:  time.Now(),
	}
}

// AlarmNotification: 방송 시작 임박 등의 이벤트로 인해 발송될 알림 메시지 정보
// 여러 사용자(Users)에게 동일한 내용이 전송될 수 있다.
type AlarmNotification struct {
	RoomID                string   `json:"room_id"`
	Channel               *Channel `json:"channel"`
	Stream                *Stream  `json:"stream"`
	MinutesUntil          int      `json:"minutes_until"`
	Users                 []string `json:"users"`
	ScheduleChangeMessage string   `json:"schedule_change_message,omitempty"`
}

// NewAlarmNotification: 알림 발송을 위한 새로운 Notification 객체를 생성합니다.
func NewAlarmNotification(roomID string, channel *Channel, stream *Stream, minutesUntil int, users []string, scheduleMessage string) *AlarmNotification {
	return &AlarmNotification{
		RoomID:                roomID,
		Channel:               channel,
		Stream:                stream,
		MinutesUntil:          minutesUntil,
		Users:                 users,
		ScheduleChangeMessage: scheduleMessage,
	}
}

// UserCount: 이 알림을 수신하게 될 사용자의 수를 반환합니다.
func (n *AlarmNotification) UserCount() int {
	return len(n.Users)
}

// AlarmQueueEnvelope: Rust 알람 서비스에서 Valkey List 큐를 통해 전달하는 알림 발송 봉투
type AlarmQueueEnvelope struct {
	Notification AlarmNotification `json:"notification"`
	ClaimKeys    []string          `json:"claim_keys"`
	EnqueuedAt   string            `json:"enqueued_at"`
	Version      uint8             `json:"version"`
}
