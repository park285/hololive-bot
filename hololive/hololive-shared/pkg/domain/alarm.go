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

	json "github.com/park285/shared-go/pkg/json"
)

type AlarmType string

const (
	AlarmTypeLive        AlarmType = "LIVE"
	AlarmTypeCommunity   AlarmType = "COMMUNITY"
	AlarmTypeShorts      AlarmType = "SHORTS"
	AlarmTypeBirthday    AlarmType = "BIRTHDAY"
	AlarmTypeAnniversary AlarmType = "ANNIVERSARY"
)

var AllAlarmTypes = []AlarmType{AlarmTypeLive, AlarmTypeCommunity, AlarmTypeShorts}

var DefaultAlarmTypes = AllAlarmTypes

var alarmTypeDisplayNames = map[AlarmType]string{
	AlarmTypeLive:        "방송",
	AlarmTypeCommunity:   "커뮤니티",
	AlarmTypeShorts:      "쇼츠",
	AlarmTypeBirthday:    "생일",
	AlarmTypeAnniversary: "주년",
}

func (t AlarmType) IsValid() bool {
	switch t {
	case AlarmTypeLive, AlarmTypeCommunity, AlarmTypeShorts,
		AlarmTypeBirthday, AlarmTypeAnniversary:
		return true
	default:
		return false
	}
}

func (t AlarmType) String() string {
	return string(t)
}

func (t AlarmType) DisplayName() string {
	if displayName, ok := alarmTypeDisplayNames[t]; ok {
		return displayName
	}
	return string(t)
}

type AlarmTypes []AlarmType

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

func (a *AlarmTypes) Scan(value any) error {
	if value == nil {
		*a = nil
		return nil
	}
	str, err := alarmTypesString(value)
	if err != nil {
		return err
	}
	*a = parseAlarmTypesArray(str)
	return nil
}

func alarmTypesString(value any) (string, error) {
	switch v := value.(type) {
	case []byte:
		return string(v), nil
	case string:
		return v, nil
	default:
		return "", fmt.Errorf("failed to scan AlarmTypes: expected string or []byte, got %T", value)
	}
}

func parseAlarmTypesArray(str string) AlarmTypes {
	str = strings.TrimPrefix(str, "{")
	str = strings.TrimSuffix(str, "}")
	if str == "" {
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
	return result
}

func (a AlarmTypes) Contains(t AlarmType) bool {
	return slices.Contains(a, t)
}

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

func (a *Alarm) RegistryKey() string {
	return a.RoomID
}

type AlarmNotification struct {
	AlarmType                   AlarmType `json:"alarm_type,omitempty"`
	RoomID                      string    `json:"room_id"`
	Channel                     *Channel  `json:"channel"`
	Stream                      *Stream   `json:"stream"`
	MinutesUntil                int       `json:"minutes_until"`
	Users                       []string  `json:"users"`
	ScheduleChangeMessage       string    `json:"schedule_change_message,omitempty"`
	ScheduleChangePreviousStart string    `json:"schedule_change_previous_start,omitempty"`
}

func NewAlarmNotification(roomID string, channel *Channel, stream *Stream, minutesUntil int, users []string, scheduleMessage string) *AlarmNotification {
	return &AlarmNotification{
		AlarmType:             AlarmTypeLive,
		RoomID:                roomID,
		Channel:               channel,
		Stream:                stream,
		MinutesUntil:          minutesUntil,
		Users:                 users,
		ScheduleChangeMessage: scheduleMessage,
	}
}

func (n *AlarmNotification) UserCount() int {
	return len(n.Users)
}

func (n *AlarmNotification) ValidateLegacyRoute() error {
	if n == nil {
		return fmt.Errorf("legacy alarm route: notification is nil")
	}
	return validateLegacyRouteAlarmType(n.AlarmType)
}

func validateLegacyRouteAlarmType(alarmType AlarmType) error {
	if alarmType == AlarmTypeLive {
		return nil
	}
	if alarmType == "" {
		return fmt.Errorf("legacy alarm route requires explicit alarm type")
	}
	return fmt.Errorf("legacy alarm route does not support alarm type %q", alarmType)
}

type AlarmQueueEnvelope struct {
	DispatchOutboxID  int64                         `json:"dispatch_outbox_id,omitempty"`
	Notification      AlarmNotification             `json:"notification"`
	SourceKind        AlarmDispatchSourceKind       `json:"source_kind,omitempty"`
	YouTubeOutbox     *YouTubeOutboxDispatchPayload `json:"youtube_outbox,omitempty"`
	Celebration       *CelebrationDispatchPayload   `json:"celebration,omitempty"`
	ClaimKeys         []string                      `json:"claim_keys"`
	EnqueuedAt        string                        `json:"enqueued_at"`
	Version           uint8                         `json:"version"`
	Retry             *AlarmQueueRetryMetadata      `json:"retry,omitempty"`
	SourcePayloadRaw  string                        `json:"source_payload,omitempty"`
	rawPayload        string                        `json:"-"`
	normalizedPayload string                        `json:"-"`
}

type AlarmQueueRetryMetadata struct {
	Attempt       int    `json:"attempt,omitempty"`
	RetryAfterMS  int64  `json:"retry_after_ms,omitempty"`
	NextVisibleAt string `json:"next_visible_at,omitempty"`
	LastError     string `json:"last_error,omitempty"`
}

type alarmQueueEnvelopeNotificationWire struct {
	AlarmType                   AlarmType `json:"alarm_type,omitempty"`
	RoomID                      string    `json:"room_id"`
	Channel                     *Channel  `json:"channel"`
	Stream                      *Stream   `json:"stream"`
	MinutesUntil                int       `json:"minutes_until"`
	Users                       []string  `json:"users"`
	ScheduleChangeMessage       string    `json:"schedule_change_message,omitempty"`
	ScheduleChangePreviousStart string    `json:"schedule_change_previous_start,omitempty"`
}

type alarmQueueEnvelopeWire struct {
	DispatchOutboxID int64                              `json:"dispatch_outbox_id,omitempty"`
	Notification     alarmQueueEnvelopeNotificationWire `json:"notification"`
	SourceKind       AlarmDispatchSourceKind            `json:"source_kind,omitempty"`
	YouTubeOutbox    *YouTubeOutboxDispatchPayload      `json:"youtube_outbox,omitempty"`
	Celebration      *CelebrationDispatchPayload        `json:"celebration,omitempty"`
	ClaimKeys        []string                           `json:"claim_keys"`
	EnqueuedAt       string                             `json:"enqueued_at"`
	Version          uint8                              `json:"version"`
	Retry            *AlarmQueueRetryMetadata           `json:"retry,omitempty"`
	SourcePayload    string                             `json:"source_payload,omitempty"`
}

func (e *AlarmQueueEnvelope) OriginalPayload() string {
	if e == nil {
		return ""
	}
	return e.rawPayload
}

func (e *AlarmQueueEnvelope) NormalizedPayload() string {
	if e == nil {
		return ""
	}
	return e.normalizedPayload
}

func (e *AlarmQueueEnvelope) SourcePayload() string {
	if e == nil {
		return ""
	}
	return e.SourcePayloadRaw
}

func (e *AlarmQueueEnvelope) EnsureSourcePayloadFromRaw() {
	if e == nil {
		return
	}
	if e.SourcePayloadRaw == "" && e.rawPayload != "" {
		e.SourcePayloadRaw = e.rawPayload
	}
}

func (e *AlarmQueueEnvelope) UnmarshalJSON(data []byte) error {
	var wire alarmQueueEnvelopeWire
	if err := json.Unmarshal(data, &wire); err != nil {
		return fmt.Errorf("unmarshal alarm queue envelope: %w", err)
	}

	alarmType := wire.Notification.AlarmType
	if alarmType == "" {
		alarmType = AlarmTypeLive
	}

	*e = AlarmQueueEnvelope{
		DispatchOutboxID: wire.DispatchOutboxID,
		Notification: AlarmNotification{
			AlarmType:                   alarmType,
			RoomID:                      wire.Notification.RoomID,
			Channel:                     wire.Notification.Channel,
			Stream:                      wire.Notification.Stream,
			MinutesUntil:                wire.Notification.MinutesUntil,
			Users:                       wire.Notification.Users,
			ScheduleChangeMessage:       wire.Notification.ScheduleChangeMessage,
			ScheduleChangePreviousStart: wire.Notification.ScheduleChangePreviousStart,
		},
		SourceKind:       wire.SourceKind,
		YouTubeOutbox:    wire.YouTubeOutbox,
		Celebration:      wire.Celebration,
		ClaimKeys:        wire.ClaimKeys,
		EnqueuedAt:       wire.EnqueuedAt,
		Version:          wire.Version,
		Retry:            wire.Retry,
		SourcePayloadRaw: wire.SourcePayload,
		rawPayload:       string(data),
	}

	normalizedPayload, err := json.Marshal(*e)
	if err != nil {
		return fmt.Errorf("marshal alarm queue envelope: %w", err)
	}
	e.normalizedPayload = string(normalizedPayload)

	return nil
}
