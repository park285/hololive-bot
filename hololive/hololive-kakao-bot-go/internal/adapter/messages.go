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

package adapter

import (
	"fmt"
	"strings"
)

// UIEmoji: 사용자 메시지에 사용하는 이모지 모음입니다.
type UIEmoji struct {
	Brand     string
	Alarm     string
	Broadcast string
	Success   string
	Error     string
	Schedule  string
	Live      string
	Hint      string
	Time      string
	Info      string
	Member    string
	Link      string
	Web       string
	Speech    string
	Highlight string
	Data      string
	Stats     string
	Video     string
}

// DefaultEmoji: 모든 사용자 메시지에 사용되는 이모지 단일 정의다.
var DefaultEmoji = UIEmoji{
	Brand:     "🌸",
	Alarm:     "🔔",
	Broadcast: "📺",
	Success:   "✅",
	Error:     "❌",
	Schedule:  "📅",
	Live:      "🔴",
	Hint:      "💡",
	Time:      "⏰",
	Info:      "ℹ️",
	Member:    "📘",
	Link:      "🔗",
	Web:       "🌐",
	Speech:    "🗣️",
	Highlight: "✨",
	Data:      "📋",
	Stats:     "📊",
	Video:     "🎬",
}

// MessageBuilder: 공통 메시지 패턴을 생성합니다.
type MessageBuilder struct {
	emoji UIEmoji
}

// NewMessageBuilder: 기본 이모지를 사용하는 MessageBuilder를 생성합니다.
func NewMessageBuilder() *MessageBuilder {
	return &MessageBuilder{emoji: DefaultEmoji}
}

// CountedHeader: 설정된 알람 개수 헤더를 생성합니다.
func (mb *MessageBuilder) CountedHeader(emoji, label string, count int) string {
	return fmt.Sprintf("%s %s (%d개)", emoji, label, count)
}

// TimeRangeHeader: 시간 범위 헤더를 생성합니다.
func (mb *MessageBuilder) TimeRangeHeader(emoji, label string, hours, count int) string {
	return fmt.Sprintf("%s %s (%d시간 이내, %d개)", emoji, label, hours, count)
}

// DayRangeHeader: 일수 범위 헤더를 생성합니다.
func (mb *MessageBuilder) DayRangeHeader(emoji, channelName string, days, count int) string {
	if channelName != "" {
		return fmt.Sprintf("%s %s 일정 (%d일 이내, %d개)", emoji, channelName, days, count)
	}
	return fmt.Sprintf("%s 일정 (%d일 이내, %d개)", emoji, days, count)
}

// EmptyMessage: 빈 상태 메시지를 생성합니다.
func (mb *MessageBuilder) EmptyMessage(emoji, message string) string {
	return fmt.Sprintf("%s %s", emoji, message)
}

// UsageHint: 사용법 안내 메시지를 생성합니다.
func (mb *MessageBuilder) UsageHint(prefix, command, example string) string {
	return fmt.Sprintf("%s 사용법:\n%s%s [멤버명]\n예) %s%s",
		mb.emoji.Hint, prefix, command, prefix, example)
}

// ErrorMessage: 에러 메시지를 생성합니다.
func (mb *MessageBuilder) ErrorMessage(message string) string {
	return fmt.Sprintf("%s %s", mb.emoji.Error, message)
}

// SuccessMessage: 성공 메시지를 생성합니다.
func (mb *MessageBuilder) SuccessMessage(message string) string {
	return fmt.Sprintf("%s %s", mb.emoji.Success, message)
}

// MemberHeader: 멤버 프로필 헤더를 생성합니다.
func (mb *MessageBuilder) MemberHeader(names []string) string {
	if len(names) == 0 {
		return fmt.Sprintf("%s 멤버 정보", mb.emoji.Member)
	}

	header := fmt.Sprintf("%s %s", mb.emoji.Member, names[0])
	if len(names) > 1 {
		header = fmt.Sprintf("%s (%s)", header, joinNames(names[1:]))
	}
	return header
}

func joinNames(names []string) string {
	var result strings.Builder
	for i, name := range names {
		if i > 0 {
			result.WriteString(" / ")
		}
		result.WriteString(name)
	}
	return result.String()
}

// 전역 MessageBuilder 인스턴스
var defaultMessageBuilder = NewMessageBuilder()

// CountedHeader: 전역 MessageBuilder로 헤더를 생성합니다.
func CountedHeader(emoji, label string, count int) string {
	return defaultMessageBuilder.CountedHeader(emoji, label, count)
}

// TimeRangeHeader: 전역 MessageBuilder로 헤더를 생성합니다.
func TimeRangeHeader(emoji, label string, hours, count int) string {
	return defaultMessageBuilder.TimeRangeHeader(emoji, label, hours, count)
}

// DayRangeHeader: 전역 MessageBuilder로 헤더를 생성합니다.
func DayRangeHeader(emoji, channelName string, days, count int) string {
	return defaultMessageBuilder.DayRangeHeader(emoji, channelName, days, count)
}

// EmptyMessage: 전역 MessageBuilder로 메시지를 생성합니다.
func EmptyMessage(emoji, message string) string {
	return defaultMessageBuilder.EmptyMessage(emoji, message)
}

// UsageHint: 전역 MessageBuilder로 사용법 안내 메시지를 생성합니다.
func UsageHint(prefix, command, example string) string {
	return defaultMessageBuilder.UsageHint(prefix, command, example)
}

// ErrorMessage: 전역 MessageBuilder로 에러 메시지를 생성합니다.
func ErrorMessage(message string) string {
	return defaultMessageBuilder.ErrorMessage(message)
}

// SuccessMessage: 전역 MessageBuilder로 성공 메시지를 생성합니다.
func SuccessMessage(message string) string {
	return defaultMessageBuilder.SuccessMessage(message)
}

// MemberHeader: 전역 MessageBuilder로 멤버 헤더를 생성합니다.
func MemberHeader(names []string) string {
	return defaultMessageBuilder.MemberHeader(names)
}

// 에러 메시지 상수 (CONVENTIONS.md 5.2절 준수)
const (
	// Member Info 관련
	ErrMemberProfileLoadFailed  = "'%s' 프로필을 불러오는 중 오류가 발생했습니다."
	ErrMemberProfileBuildFailed = "'%s' 프로필을 구성하지 못했습니다."
	ErrMemberInfoDisplayFailed  = "멤버 정보를 표시할 수 없습니다. 관리자에게 문의해주세요."
	ErrNoMemberInfoFound        = "등록된 멤버 정보를 찾을 수 없습니다."
	ErrCannotDisplayMemberInfo  = "멤버 정보를 표시할 수 없습니다."
	MsgGraduatedMemberWarning   = "⚠️ 졸업한 멤버입니다.\n\n"
	// 졸업 멤버 조회 차단 메시지 (라이브/일정/알람 명령용)
	ErrGraduatedMemberBlocked = "⚠️ 졸업한 멤버입니다."

	// Alarm 관련
	ErrAlarmServiceNotInitialized = "알람 서비스가 초기화되지 않았습니다."
	ErrAlarmAddFailed             = "알람 설정 중 오류가 발생했습니다."
	ErrAlarmRemoveFailed          = "알람 제거 중 오류가 발생했습니다."
	ErrAlarmListFailed            = "알람 목록 조회 실패"
	ErrAlarmClearFailed           = "알람 초기화 중 오류가 발생했습니다."
	ErrAlarmNeedMemberNameAdd     = "멤버 이름을 입력해주세요.\n예) !알람 추가 페코라"
	ErrAlarmNeedMemberNameRemove  = "멤버 이름을 입력해주세요.\n예) !알람 제거 페코라"

	// Live/Upcoming/Schedule 관련
	ErrLiveStreamQueryFailed     = "라이브 스트림 조회 실패"
	ErrUpcomingStreamQueryFailed = "예정 방송 조회 실패"
	ErrScheduleQueryFailed       = "일정 조회 실패"
	MsgMemberNotLive             = "%s은(는) 현재 방송 중이 아닙니다."
	MsgMemberNoUpcoming          = "%s은(는) %d시간 이내 예정된 방송이 없습니다."
	ErrScheduleNeedMemberName    = "❌ 멤버 이름을 지정해주세요.\n예) !일정 페코라"

	// Stats 관련
	ErrUnknownStatsPeriod = "알 수 없는 통계 유형입니다. !도움말을 참고해주세요."
	ErrStatsQueryFailed   = "구독자 순위 조회 중 오류가 발생했습니다."
	MsgNoStatsData        = "해당 기간의 통계 데이터가 없습니다."

	// Subscriber 관련
	ErrSubscriberNeedMemberName = "❌ 멤버 이름을 입력해주세요.\n예) !구독자 페코라"
	ErrSubscriberQueryFailed    = "구독자 정보 조회 중 오류가 발생했습니다."
	MsgNoSubscriberData         = "해당 멤버의 구독자 정보가 없습니다."

	// Matcher 관련
	ErrMatcherNotActivated = "멤버 검색 기능이 활성화되지 않았습니다."

	// Bot 공통 에러/안내 메시지
	ErrUnknownCommand           = "죄송합니다. 요청하신 기능을 이해하지 못했습니다.\n!도움 명령어로 사용 가능한 기능을 확인하세요."
	ErrExternalAPICallFailed    = "외부 API 호출 중 오류가 발생했습니다. 잠시 후 다시 시도해주세요."
	ErrCacheConnectionFailed    = "데이터베이스 연결 오류입니다. 관리자에게 문의하세요."
	ErrIrisConnectionFailed     = "Iris 서버 연결 오류입니다. 서버 상태를 확인해주세요."
	ErrCommandProcessingFailed  = "%s 명령어 처리 중 오류가 발생했습니다."
	ErrDisplayLiveStreamsFailed = "방송 목록을 표시할 수 없습니다."
	ErrDisplayUpcomingFailed    = "예정 방송 목록을 표시할 수 없습니다."
	ErrDisplayScheduleFailed    = "일정을 표시할 수 없습니다."
	ErrDisplayAlarmAddFailed    = "알람 설정 결과를 표시할 수 없습니다."
	ErrDisplayAlarmRemoveFailed = "알람 제거 결과를 표시할 수 없습니다."
	ErrDisplayAlarmListFailed   = "알람 목록을 표시할 수 없습니다."
	ErrDisplayAlarmClearFailed  = "알람 초기화 결과를 표시할 수 없습니다."
	ErrDisplayAlarmNotifyFailed = "알람 알림을 표시할 수 없습니다."
	ErrDisplayMemberListFailed  = "멤버 목록을 표시할 수 없습니다."
	ErrDisplayHelpFailed        = "도움말을 표시할 수 없습니다."
	ErrDisplayProfileDataFailed = "프로필 데이터를 찾을 수 없습니다."
	ErrDisplayMajorEventFailed  = "행사 알림 정보를 표시할 수 없습니다."
	ErrDisplayMemberNewsFailed  = "멤버 뉴스 정보를 표시할 수 없습니다."
	ErrInvalidAlarmUsage        = "지원하지 않는 알람 명령입니다.\n예) !알람 추가 페코라"
	MsgTimeUnknown              = "시간 미정"
	MsgStatsGainersHeader       = "구독자 증가 순위"

	// MemberNews 관련
	ErrMemberNewsServiceNotInitialized = "뉴스 서비스가 초기화되지 않았습니다."
	ErrMemberNewsQueryFailed           = "뉴스 조회 중 오류가 발생했습니다."
	ErrMemberNewsSubscriptionFailed    = "뉴스 구독 처리 중 오류가 발생했습니다."
	MsgMemberNewsNoMembers             = "🗞️ 뉴스 대상 멤버가 없습니다. 먼저 !알람 추가 [멤버명] 으로 멤버를 등록해주세요."
	MsgMemberNewsSubscribed            = "✅ 뉴스 알림을 켰습니다. 매주 월요일 09:00 KST에 자동 발송됩니다."
	MsgMemberNewsAlreadySubscribed     = "🔔 뉴스 알림이 이미 켜져 있습니다."
	MsgMemberNewsUnsubscribed          = "✅ 뉴스 알림을 껐습니다."
	MsgMemberNewsNotSubscribed         = "ℹ️ 뉴스 알림이 이미 꺼져 있습니다."
	MsgMemberNewsStatusOn              = "🔔 뉴스 알림 상태: ON\n- 자동 발송: 매주 월요일 09:00 KST\n- 해제: !뉴스알림 끄기"
	MsgMemberNewsStatusOff             = "🔕 뉴스 알림 상태: OFF\n- 설정: !뉴스알림 켜기"

	// Subscriber Graph 관련
	ErrGraphNeedMemberName = "❌ 멤버 이름을 입력해주세요.\n예) !구독자그래프 페코라"
	ErrGraphQueryFailed    = "구독자 그래프 조회 중 오류가 발생했습니다."
	MsgNoGraphData         = "해당 멤버의 구독자 데이터가 없습니다."
)
