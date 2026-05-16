package adapter

import messaging "github.com/kapu/hololive-kakao-bot-go/internal/adapter/internal/messaging"

type AlarmListEntry = messaging.AlarmListEntry
type CommandParser = messaging.CommandParser
type MemberDirectoryEntry = messaging.MemberDirectoryEntry
type MemberDirectoryGroup = messaging.MemberDirectoryGroup
type MessageAdapter = messaging.MessageAdapter
type MessageBuilder = messaging.MessageBuilder
type ParsedCommand = messaging.ParsedCommand
type ResponseFormatter = messaging.ResponseFormatter
type UIEmoji = messaging.UIEmoji

const (
	ErrMemberProfileLoadFailed  = messaging.ErrMemberProfileLoadFailed
	ErrMemberProfileBuildFailed = messaging.ErrMemberProfileBuildFailed
	ErrMemberInfoDisplayFailed  = messaging.ErrMemberInfoDisplayFailed
	ErrNoMemberInfoFound        = messaging.ErrNoMemberInfoFound
	ErrCannotDisplayMemberInfo  = messaging.ErrCannotDisplayMemberInfo
	MsgGraduatedMemberWarning   = messaging.MsgGraduatedMemberWarning
	ErrGraduatedMemberBlocked   = messaging.ErrGraduatedMemberBlocked

	ErrAlarmServiceNotInitialized = messaging.ErrAlarmServiceNotInitialized
	ErrAlarmAddFailed             = messaging.ErrAlarmAddFailed
	ErrAlarmRemoveFailed          = messaging.ErrAlarmRemoveFailed
	ErrAlarmListFailed            = messaging.ErrAlarmListFailed
	ErrAlarmClearFailed           = messaging.ErrAlarmClearFailed
	ErrAlarmNeedMemberNameAdd     = messaging.ErrAlarmNeedMemberNameAdd
	ErrAlarmNeedMemberNameRemove  = messaging.ErrAlarmNeedMemberNameRemove

	ErrLiveStreamQueryFailed     = messaging.ErrLiveStreamQueryFailed
	ErrUpcomingStreamQueryFailed = messaging.ErrUpcomingStreamQueryFailed
	ErrScheduleQueryFailed       = messaging.ErrScheduleQueryFailed
	MsgMemberNotLive             = messaging.MsgMemberNotLive
	MsgMemberNoUpcoming          = messaging.MsgMemberNoUpcoming
	ErrScheduleNeedMemberName    = messaging.ErrScheduleNeedMemberName

	ErrUnknownStatsPeriod = messaging.ErrUnknownStatsPeriod
	ErrStatsQueryFailed   = messaging.ErrStatsQueryFailed
	MsgNoStatsData        = messaging.MsgNoStatsData

	ErrSubscriberNeedMemberName = messaging.ErrSubscriberNeedMemberName
	ErrSubscriberQueryFailed    = messaging.ErrSubscriberQueryFailed
	MsgNoSubscriberData         = messaging.MsgNoSubscriberData

	ErrMatcherNotActivated = messaging.ErrMatcherNotActivated

	ErrUnknownCommand           = messaging.ErrUnknownCommand
	ErrExternalAPICallFailed    = messaging.ErrExternalAPICallFailed
	ErrCacheConnectionFailed    = messaging.ErrCacheConnectionFailed
	ErrIrisConnectionFailed     = messaging.ErrIrisConnectionFailed
	ErrCommandProcessingFailed  = messaging.ErrCommandProcessingFailed
	ErrDisplayLiveStreamsFailed = messaging.ErrDisplayLiveStreamsFailed
	ErrDisplayUpcomingFailed    = messaging.ErrDisplayUpcomingFailed
	ErrDisplayScheduleFailed    = messaging.ErrDisplayScheduleFailed
	ErrDisplayAlarmAddFailed    = messaging.ErrDisplayAlarmAddFailed
	ErrDisplayAlarmRemoveFailed = messaging.ErrDisplayAlarmRemoveFailed
	ErrDisplayAlarmListFailed   = messaging.ErrDisplayAlarmListFailed
	ErrDisplayAlarmClearFailed  = messaging.ErrDisplayAlarmClearFailed
	ErrDisplayAlarmNotifyFailed = messaging.ErrDisplayAlarmNotifyFailed
	ErrDisplayMemberListFailed  = messaging.ErrDisplayMemberListFailed
	ErrDisplayHelpFailed        = messaging.ErrDisplayHelpFailed
	ErrDisplayProfileDataFailed = messaging.ErrDisplayProfileDataFailed
	ErrDisplayMajorEventFailed  = messaging.ErrDisplayMajorEventFailed
	ErrDisplayMemberNewsFailed  = messaging.ErrDisplayMemberNewsFailed
	ErrInvalidAlarmUsage        = messaging.ErrInvalidAlarmUsage
	MsgTimeUnknown              = messaging.MsgTimeUnknown
	MsgStatsGainersHeader       = messaging.MsgStatsGainersHeader

	ErrMemberNewsServiceNotInitialized = messaging.ErrMemberNewsServiceNotInitialized
	ErrMemberNewsQueryFailed           = messaging.ErrMemberNewsQueryFailed
	ErrMemberNewsSubscriptionFailed    = messaging.ErrMemberNewsSubscriptionFailed
	MsgMemberNewsNoMembers             = messaging.MsgMemberNewsNoMembers
	MsgMemberNewsSubscribed            = messaging.MsgMemberNewsSubscribed
	MsgMemberNewsAlreadySubscribed     = messaging.MsgMemberNewsAlreadySubscribed
	MsgMemberNewsUnsubscribed          = messaging.MsgMemberNewsUnsubscribed
	MsgMemberNewsNotSubscribed         = messaging.MsgMemberNewsNotSubscribed
	MsgMemberNewsStatusOn              = messaging.MsgMemberNewsStatusOn
	MsgMemberNewsStatusOff             = messaging.MsgMemberNewsStatusOff

	ErrGraphNeedMemberName = messaging.ErrGraphNeedMemberName
	ErrGraphQueryFailed    = messaging.ErrGraphQueryFailed
	MsgNoGraphData         = messaging.MsgNoGraphData
)

var DefaultEmoji = messaging.DefaultEmoji

var CountedHeader = messaging.CountedHeader
var DayRangeHeader = messaging.DayRangeHeader
var EmptyMessage = messaging.EmptyMessage
var ErrorMessage = messaging.ErrorMessage
var MemberHeader = messaging.MemberHeader
var SuccessMessage = messaging.SuccessMessage
var TimeRangeHeader = messaging.TimeRangeHeader
var UsageHint = messaging.UsageHint

var NewMessageAdapter = messaging.NewMessageAdapter
var NewMessageBuilder = messaging.NewMessageBuilder
var NewResponseFormatter = messaging.NewResponseFormatter
