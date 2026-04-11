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

package keys

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/stringutil"
)

// Valkey 키 접두사 (Go alarm_types.go, Rust keys.rs 1:1 대응)
const (
	AlarmKeyPrefix                    = "alarm:"
	AlarmRegistryKey                  = "alarm:registry"
	AlarmChannelRegistryKey           = "alarm:channel_registry"
	ChannelSubscribersKeyPrefix       = "alarm:channel_subscribers:"
	ChannelSubscribersCommunityPrefix = "alarm:channel_subscribers:COMMUNITY:"
	ChannelSubscribersShortsPrefix    = "alarm:channel_subscribers:SHORTS:"
	MemberNameKey                     = "alarm:member_names"
	RoomNamesCacheKey                 = "alarm:room_names"
	UserNamesCacheKey                 = "alarm:user_names"
	NotifiedKeyPrefix                 = "notified:"
	NotifyClaimKeyPrefix              = "notified:claim:"
	NotifyLogicalClaimKeyPrefix       = "notified:claim:event:"
	UpcomingEventKeyPrefix            = "notified:upcoming:event:"
	ScheduleTransitionKeyPrefix       = "notified:schedule:transition:"
)

func BuildRoomAlarmKey(roomID string) string {
	return AlarmKeyPrefix + roomID
}

type ChannelContentAlarmTargetKeys struct {
	ChannelID               string `json:"channel_id"`
	CommunitySubscribersKey string `json:"community_subscribers_key"`
	ShortsSubscribersKey    string `json:"shorts_subscribers_key"`
}

func BuildChannelSubscriberKey(channelID string, alarmType domain.AlarmType) string {
	switch alarmType {
	case domain.AlarmTypeCommunity:
		return ChannelSubscribersCommunityPrefix + channelID
	case domain.AlarmTypeShorts:
		return ChannelSubscribersShortsPrefix + channelID
	default:
		return ChannelSubscribersKeyPrefix + channelID
	}
}

func BuildChannelContentAlarmTargetKeys(channelID string) ChannelContentAlarmTargetKeys {
	return ChannelContentAlarmTargetKeys{
		ChannelID:               channelID,
		CommunitySubscribersKey: BuildChannelSubscriberKey(channelID, domain.AlarmTypeCommunity),
		ShortsSubscribersKey:    BuildChannelSubscriberKey(channelID, domain.AlarmTypeShorts),
	}
}

func (k ChannelContentAlarmTargetKeys) KeyFor(alarmType domain.AlarmType) string {
	switch alarmType {
	case domain.AlarmTypeCommunity:
		return k.CommunitySubscribersKey
	case domain.AlarmTypeShorts:
		return k.ShortsSubscribersKey
	default:
		return ""
	}
}

// NotifiedKey: "notified:{streamID}"
func NotifiedKey(streamID string) string {
	return NotifiedKeyPrefix + streamID
}

// NotificationCategory: dedup 키 구성 요소
//   - minutesUntil == 0 -> "live"
//   - targetMinutes에 포함 -> "target"
//   - 그 외 -> minutesUntil 문자열
func NotificationCategory(targetMinutes []int, minutesUntil int) string {
	if minutesUntil == 0 {
		return "live"
	}
	if slices.Contains(targetMinutes, minutesUntil) {
		return "target"
	}
	return strconv.Itoa(minutesUntil)
}

// NormalizeScheduledMinute: 시각을 분 단위로 truncate (초/나노초 제거)
func NormalizeScheduledMinute(t time.Time) time.Time {
	return t.Truncate(time.Minute)
}

// normalizeTitleForFingerprint: alarm dedup 전용 title 정규화.
// NormalizeKey 기반에 CJK full-width 구두점을 추가로 제거한다.
// NormalizeKey 전역 수정 시 멤버 검색/LLM 필터 side effect 발생하므로 분리.
func normalizeTitleForFingerprint(title string) string {
	base := stringutil.NormalizeKey(title)
	if base == "" {
		return ""
	}

	var builder strings.Builder
	builder.Grow(len(base))
	for _, r := range base {
		switch r {
		case '?', '(', ')', '&', ',', ':', ';',
			'\uFF01', '\uFF1F', '\uFF08', '\uFF09', '\uFF06', '\uFF0C', '\uFF1A', '\uFF1B',
			'\u3001', '\u3002', '\u300C', '\u300D', '\u3010', '\u3011':
			continue
		default:
			builder.WriteRune(r)
		}
	}
	return builder.String()
}

// BuildTitleFingerprint: SHA256 앞 8바이트 hex (16문자)
func BuildTitleFingerprint(title, streamID string) string {
	normalized := normalizeTitleForFingerprint(title)
	if normalized == "" {
		normalized = normalizeTitleForFingerprint(streamID)
	}
	if normalized == "" {
		normalized = "untitled"
	}

	h := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(h[:8])
}

// BuildNotifyClaimKey: SETNX 기반 알림 선점 키
// "notified:claim:{roomID}:{streamID}:{scheduleUnix}:{category}"
func BuildNotifyClaimKey(roomID, streamID string, startScheduled time.Time, category string) string {
	scheduleUnix := NormalizeScheduledMinute(startScheduled).Unix()
	return fmt.Sprintf("%s%s:%s:%d:%s", NotifyClaimKeyPrefix, roomID, streamID, scheduleUnix, category)
}

// BuildLogicalEventClaimKey: stream_id 변경 대응 논리 이벤트 claim 키
// "notified:claim:event:{roomID}:{channelID}:{scheduleUnix}:{titleFP}:{category}"
func BuildLogicalEventClaimKey(roomID, channelID, streamID, title string, startScheduled time.Time, category string) string {
	scheduleUnix := NormalizeScheduledMinute(startScheduled).Unix()
	titleFP := BuildTitleFingerprint(title, streamID)
	return fmt.Sprintf("%s%s:%s:%d:%s:%s", NotifyLogicalClaimKeyPrefix, roomID, channelID, scheduleUnix, titleFP, category)
}

// BuildUpcomingEventKey: 예정 알림 이벤트 키
// "notified:upcoming:event:{roomID}:{channelID}:{scheduleUnix}:{titleFP}"
func BuildUpcomingEventKey(roomID, channelID, streamID, title string, startScheduled time.Time) string {
	scheduleUnix := NormalizeScheduledMinute(startScheduled).Unix()
	titleFP := BuildTitleFingerprint(title, streamID)
	return fmt.Sprintf("%s%s:%s:%d:%s", UpcomingEventKeyPrefix, roomID, channelID, scheduleUnix, titleFP)
}

// BuildScheduleTransitionKey: 일정 변경 전환 claim 키
// "notified:schedule:transition:{streamID}:{oldUnix}:{newUnix}"
func BuildScheduleTransitionKey(streamID string, oldScheduled, newScheduled time.Time) string {
	oldUnix := NormalizeScheduledMinute(oldScheduled).Unix()
	newUnix := NormalizeScheduledMinute(newScheduled).Unix()
	return fmt.Sprintf("%s%s:%d:%d", ScheduleTransitionKeyPrefix, streamID, oldUnix, newUnix)
}

// FormatScheduled: DateTime을 RFC3339 분 단위 포맷 (초 버림)
func FormatScheduled(t time.Time) string {
	return NormalizeScheduledMinute(t).UTC().Format(time.RFC3339)
}
