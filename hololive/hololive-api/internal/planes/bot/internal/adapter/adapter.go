package adapter

import (
	messaging "github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging/formatter"
)

type AlarmListEntry = formatter.AlarmListEntry
type CommandParser = messaging.CommandParser
type MemberDirectoryEntry = formatter.MemberDirectoryEntry
type MemberDirectoryGroup = formatter.MemberDirectoryGroup
type MessageAdapter = messaging.MessageAdapter
type ParsedCommand = messaging.ParsedCommand
type ResponseFormatter = formatter.ResponseFormatter

const (
	ErrMemberProfileLoadFailed  = messaging.ErrMemberProfileLoadFailed
	ErrMemberProfileBuildFailed = messaging.ErrMemberProfileBuildFailed
	ErrNoMemberInfoFound        = messaging.ErrNoMemberInfoFound
	ErrCannotDisplayMemberInfo  = messaging.ErrCannotDisplayMemberInfo
	ErrGraduatedMemberBlocked   = messaging.ErrGraduatedMemberBlocked

	ErrAlarmServiceNotInitialized = messaging.ErrAlarmServiceNotInitialized
	ErrAlarmAddFailed             = messaging.ErrAlarmAddFailed
	ErrAlarmRemoveFailed          = messaging.ErrAlarmRemoveFailed
	ErrAlarmListFailed            = messaging.ErrAlarmListFailed
	ErrAlarmClearFailed           = messaging.ErrAlarmClearFailed
	ErrAlarmNeedMemberNameAdd     = messaging.ErrAlarmNeedMemberNameAdd
	ErrAlarmNeedMemberNameRemove  = messaging.ErrAlarmNeedMemberNameRemove
	ErrInvalidAlarmUsage          = messaging.ErrInvalidAlarmUsage

	ErrLiveStreamQueryFailed     = messaging.ErrLiveStreamQueryFailed
	ErrUpcomingStreamQueryFailed = messaging.ErrUpcomingStreamQueryFailed
	ErrScheduleQueryFailed       = messaging.ErrScheduleQueryFailed
	ErrScheduleNeedMemberName    = messaging.ErrScheduleNeedMemberName

	ErrUnknownStatsPeriod = messaging.ErrUnknownStatsPeriod
	ErrStatsQueryFailed   = messaging.ErrStatsQueryFailed
	MsgNoStatsData        = messaging.MsgNoStatsData

	ErrSubscriberNeedMemberName = messaging.ErrSubscriberNeedMemberName
	ErrSubscriberQueryFailed    = messaging.ErrSubscriberQueryFailed
	MsgNoSubscriberData         = messaging.MsgNoSubscriberData

	ErrCalendarQueryFailed = messaging.ErrCalendarQueryFailed

	ErrMajorEventServiceNotInitialized = messaging.ErrMajorEventServiceNotInitialized
	ErrMajorEventStatusCheckFailed     = messaging.ErrMajorEventStatusCheckFailed
	ErrMajorEventSubscribeFailed       = messaging.ErrMajorEventSubscribeFailed
	ErrMajorEventUnsubscribeFailed     = messaging.ErrMajorEventUnsubscribeFailed

	ErrMemberNewsServiceNotInitialized = messaging.ErrMemberNewsServiceNotInitialized
	ErrMemberNewsQueryFailed           = messaging.ErrMemberNewsQueryFailed
	ErrMemberNewsSubscriptionFailed    = messaging.ErrMemberNewsSubscriptionFailed

	ErrUnknownCommand          = messaging.ErrUnknownCommand
	ErrExternalAPICallFailed   = messaging.ErrExternalAPICallFailed
	ErrCacheConnectionFailed   = messaging.ErrCacheConnectionFailed
	ErrIrisConnectionFailed    = messaging.ErrIrisConnectionFailed
	ErrCommandProcessingFailed = messaging.ErrCommandProcessingFailed
)

var NewMessageAdapter = messaging.NewMessageAdapter
var NewResponseFormatter = formatter.NewResponseFormatter
var WithMessageStrings = formatter.WithMessageStrings
