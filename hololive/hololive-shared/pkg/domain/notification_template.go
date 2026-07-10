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

import "time"

type TemplateKey string

const (
	// OUTBOX_* : YouTube 알림 (outbox dispatcher)
	TemplateKeyOutboxShorts    TemplateKey = "OUTBOX_SHORTS"
	TemplateKeyOutboxCommunity TemplateKey = "OUTBOX_COMMUNITY"
	TemplateKeyOutboxVideo     TemplateKey = "OUTBOX_VIDEO"
	TemplateKeyOutboxMilestone TemplateKey = "OUTBOX_MILESTONE"

	// OUTBOX_*_GROUP : YouTube 그룹 알림 (여러 항목 묶음)
	TemplateKeyOutboxVideoGroup     TemplateKey = "OUTBOX_VIDEO_GROUP"
	TemplateKeyOutboxShortsGroup    TemplateKey = "OUTBOX_SHORTS_GROUP"
	TemplateKeyOutboxCommunityGroup TemplateKey = "OUTBOX_COMMUNITY_GROUP"

	TemplateKeyAlarmDispatchNotification      TemplateKey = "ALARM_DISPATCH_NOTIFICATION"
	TemplateKeyAlarmDispatchNotificationGroup TemplateKey = "ALARM_DISPATCH_NOTIFICATION_GROUP"

	// CMD_* : 명령어 응답 (adapter formatter)
	TemplateKeyCmdAlarmList              TemplateKey = "CMD_ALARM_LIST"
	TemplateKeyCmdAlarmNotification      TemplateKey = "CMD_ALARM_NOTIFICATION"
	TemplateKeyCmdAlarmLiveStarted       TemplateKey = "CMD_ALARM_LIVE_STARTED"
	TemplateKeyCmdAlarmNotificationGroup TemplateKey = "CMD_ALARM_NOTIFICATION_GROUP"
	TemplateKeyCmdLiveStreams            TemplateKey = "CMD_LIVE_STREAMS"
	TemplateKeyCmdUpcomingStreams        TemplateKey = "CMD_UPCOMING_STREAMS"
	TemplateKeyCmdHelp                   TemplateKey = "CMD_HELP"
	TemplateKeyCmdMemberDirectory        TemplateKey = "CMD_MEMBER_DIRECTORY"
	TemplateKeyCmdProfile                TemplateKey = "CMD_PROFILE"
	TemplateKeyCmdChannelSchedule        TemplateKey = "CMD_CHANNEL_SCHEDULE"
	TemplateKeyCmdAlarmAdded             TemplateKey = "CMD_ALARM_ADDED"
	TemplateKeyCmdAlarmRemoved           TemplateKey = "CMD_ALARM_REMOVED"
	TemplateKeyCmdAlarmCleared           TemplateKey = "CMD_ALARM_CLEARED"
	TemplateKeyCmdMilestoneAchieved      TemplateKey = "CMD_MILESTONE_ACHIEVED"
	TemplateKeyCmdMilestoneApproach      TemplateKey = "CMD_MILESTONE_APPROACHING"
	TemplateKeyCmdStatsCount             TemplateKey = "CMD_STATS_COUNT"
	TemplateKeyCmdStatsGainers           TemplateKey = "CMD_STATS_GAINERS"
	TemplateKeyCmdCalendar               TemplateKey = "CMD_CALENDAR"
	TemplateKeyCmdMemberNotLive          TemplateKey = "CMD_MEMBER_NOT_LIVE"
	TemplateKeyCmdMemberNoUpcoming       TemplateKey = "CMD_MEMBER_NO_UPCOMING"
	TemplateKeyCmdMemberNotFound         TemplateKey = "CMD_MEMBER_NOT_FOUND"
	TemplateKeyCmdAmbiguousMember        TemplateKey = "CMD_AMBIGUOUS_MEMBER"

	// CMD_MAJOR_EVENT_* : 대형 행사 알림 명령어
	TemplateKeyCmdMajorEventWeeklySummary  TemplateKey = "CMD_MAJOR_EVENT_WEEKLY_SUMMARY"
	TemplateKeyCmdMajorEventMonthlySummary TemplateKey = "CMD_MAJOR_EVENT_MONTHLY_SUMMARY"
	TemplateKeyCmdMajorEventSubscribed     TemplateKey = "CMD_MAJOR_EVENT_SUBSCRIBED"
	TemplateKeyCmdMajorEventUnsubscribed   TemplateKey = "CMD_MAJOR_EVENT_UNSUBSCRIBED"
	TemplateKeyCmdMajorEventAlreadySub     TemplateKey = "CMD_MAJOR_EVENT_ALREADY_SUB"
	TemplateKeyCmdMajorEventNotSub         TemplateKey = "CMD_MAJOR_EVENT_NOT_SUB"
	TemplateKeyCmdMajorEventStatus         TemplateKey = "CMD_MAJOR_EVENT_STATUS"
	TemplateKeyCmdMajorEventUsage          TemplateKey = "CMD_MAJOR_EVENT_USAGE"

	TemplateKeyCelebrationBirthday       TemplateKey = "CELEBRATION_BIRTHDAY"
	TemplateKeyCelebrationAnniversary    TemplateKey = "CELEBRATION_ANNIVERSARY"
	TemplateKeyCelebrationBirthdayStream TemplateKey = "CELEBRATION_BIRTHDAY_STREAM"

	// CMD_MEMBER_NEWS_* : 구독 멤버 뉴스
	TemplateKeyCmdMemberNewsDigest       TemplateKey = "CMD_MEMBER_NEWS_DIGEST"
	TemplateKeyCmdMemberNewsNoMembers    TemplateKey = "CMD_MEMBER_NEWS_NO_MEMBERS"
	TemplateKeyCmdMemberNewsSubscribed   TemplateKey = "CMD_MEMBER_NEWS_SUBSCRIBED"
	TemplateKeyCmdMemberNewsUnsubscribed TemplateKey = "CMD_MEMBER_NEWS_UNSUBSCRIBED"
	TemplateKeyCmdMemberNewsAlreadySub   TemplateKey = "CMD_MEMBER_NEWS_ALREADY_SUB"
	TemplateKeyCmdMemberNewsNotSub       TemplateKey = "CMD_MEMBER_NEWS_NOT_SUB"
	TemplateKeyCmdMemberNewsStatus       TemplateKey = "CMD_MEMBER_NEWS_STATUS"
)

type NotificationTemplate struct {
	ID          int64       `db:"id" json:"id"`
	TemplateKey TemplateKey `db:"template_key" json:"template_key"`
	ChannelID   *string     `db:"channel_id" json:"channel_id,omitempty"`
	Body        string      `db:"body" json:"body"`
	CreatedAt   time.Time   `db:"created_at" json:"created_at"`
	UpdatedAt   time.Time   `db:"updated_at" json:"updated_at"`
}

func (NotificationTemplate) TableName() string {
	return "notification_templates"
}

type NotificationTemplateRevision struct {
	ID         int64     `db:"id" json:"id"`
	TemplateID int64     `db:"template_id" json:"template_id"`
	Body       string    `db:"body" json:"body"`
	CreatedAt  time.Time `db:"created_at" json:"created_at"`
}

func (NotificationTemplateRevision) TableName() string {
	return "notification_template_revisions"
}
