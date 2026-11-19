package domain

type TemplateKey string

const (
	TemplateKeyOutboxShorts    TemplateKey = "OUTBOX_SHORTS"
	TemplateKeyOutboxCommunity TemplateKey = "OUTBOX_COMMUNITY"
	TemplateKeyOutboxVideo     TemplateKey = "OUTBOX_VIDEO"

	TemplateKeyOutboxVideoGroup     TemplateKey = "OUTBOX_VIDEO_GROUP"
	TemplateKeyOutboxShortsGroup    TemplateKey = "OUTBOX_SHORTS_GROUP"
	TemplateKeyOutboxCommunityGroup TemplateKey = "OUTBOX_COMMUNITY_GROUP"

	TemplateKeyAlarmDispatchNotification      TemplateKey = "ALARM_DISPATCH_NOTIFICATION"
	TemplateKeyAlarmDispatchNotificationGroup TemplateKey = "ALARM_DISPATCH_NOTIFICATION_GROUP"

	TemplateKeyXSpaceStarted TemplateKey = "X_SPACE_STARTED"

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

	TemplateKeyCelebrationBirthday    TemplateKey = "CELEBRATION_BIRTHDAY"
	TemplateKeyCelebrationAnniversary TemplateKey = "CELEBRATION_ANNIVERSARY"
)

type TemplateFormat string

const (
	TemplateFormatPlain    TemplateFormat = "plain"
	TemplateFormatRichText TemplateFormat = "richtext"
)

func (f TemplateFormat) IsValid() bool {
	return f == TemplateFormatPlain || f == TemplateFormatRichText
}
