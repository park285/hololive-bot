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

type TemplateKey string

const (
	// OUTBOX_* : YouTube 알림 (outbox dispatcher).
	TemplateKeyOutboxShorts    TemplateKey = "OUTBOX_SHORTS"
	TemplateKeyOutboxCommunity TemplateKey = "OUTBOX_COMMUNITY"
	TemplateKeyOutboxVideo     TemplateKey = "OUTBOX_VIDEO"

	// OUTBOX_*_GROUP : YouTube 그룹 알림 (여러 항목 묶음).
	TemplateKeyOutboxVideoGroup     TemplateKey = "OUTBOX_VIDEO_GROUP"
	TemplateKeyOutboxShortsGroup    TemplateKey = "OUTBOX_SHORTS_GROUP"
	TemplateKeyOutboxCommunityGroup TemplateKey = "OUTBOX_COMMUNITY_GROUP"

	TemplateKeyAlarmDispatchNotification      TemplateKey = "ALARM_DISPATCH_NOTIFICATION"
	TemplateKeyAlarmDispatchNotificationGroup TemplateKey = "ALARM_DISPATCH_NOTIFICATION_GROUP"

	// TemplateKeyXSpaceStarted는 멤버가 개설한 X 스페이스의 시작 알림 템플릿입니다.
	TemplateKeyXSpaceStarted TemplateKey = "X_SPACE_STARTED"

	// CMD_* : 명령어 응답 (adapter formatter).
	TemplateKeyCmdAlarmList        TemplateKey = "CMD_ALARM_LIST"
	TemplateKeyCmdLiveStreams      TemplateKey = "CMD_LIVE_STREAMS"
	TemplateKeyCmdUpcomingStreams  TemplateKey = "CMD_UPCOMING_STREAMS"
	TemplateKeyCmdHelp             TemplateKey = "CMD_HELP"
	TemplateKeyCmdMemberDirectory  TemplateKey = "CMD_MEMBER_DIRECTORY"
	TemplateKeyCmdProfile          TemplateKey = "CMD_PROFILE"
	TemplateKeyCmdChannelSchedule  TemplateKey = "CMD_CHANNEL_SCHEDULE"
	TemplateKeyCmdAlarmAdded       TemplateKey = "CMD_ALARM_ADDED"
	TemplateKeyCmdAlarmRemoved     TemplateKey = "CMD_ALARM_REMOVED"
	TemplateKeyCmdAlarmCleared     TemplateKey = "CMD_ALARM_CLEARED"
	TemplateKeyCmdCalendar         TemplateKey = "CMD_CALENDAR"
	TemplateKeyCmdMemberNotLive    TemplateKey = "CMD_MEMBER_NOT_LIVE"
	TemplateKeyCmdMemberNoUpcoming TemplateKey = "CMD_MEMBER_NO_UPCOMING"
	TemplateKeyCmdMemberNotFound   TemplateKey = "CMD_MEMBER_NOT_FOUND"
	TemplateKeyCmdAmbiguousMember  TemplateKey = "CMD_AMBIGUOUS_MEMBER"

	TemplateKeyCelebrationBirthday       TemplateKey = "CELEBRATION_BIRTHDAY"
	TemplateKeyCelebrationAnniversary    TemplateKey = "CELEBRATION_ANNIVERSARY"
	TemplateKeyCelebrationBirthdayStream TemplateKey = "CELEBRATION_BIRTHDAY_STREAM"
)

// TemplateFormat은 같은 템플릿 키의 방 유형별 본문 변형입니다. 카카오 일반채팅은 RichText 서식을 표시하지 않으므로
// 일반 텍스트로 보내는 방은 plain 본문을, 확인된 오픈채팅의 RichText 발송은 richtext 본문을 씁니다.
// 값은 notification_templates.format_variant의 CHECK 어휘와 같습니다.
type TemplateFormat string

const (
	TemplateFormatPlain    TemplateFormat = "plain"
	TemplateFormatRichText TemplateFormat = "richtext"
)

func (f TemplateFormat) IsValid() bool {
	return f == TemplateFormatPlain || f == TemplateFormatRichText
}
