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

package messaging

// 값은 사용자-facing 문구가 아니라 message_strings(error ns) 조회 key. SendError가 해석+sentinel 폴백.
const (
	ErrMemberProfileLoadFailed  = "member_profile_load_failed"
	ErrMemberProfileBuildFailed = "member_profile_build_failed"
	ErrNoMemberInfoFound        = "no_member_info_found"
	ErrCannotDisplayMemberInfo  = "cannot_display_member_info"
	ErrGraduatedMemberBlocked   = "graduated_member_blocked"

	ErrAlarmServiceNotInitialized = "alarm_service_not_initialized"
	ErrAlarmAddFailed             = "alarm_add_failed"
	ErrAlarmRemoveFailed          = "alarm_remove_failed"
	ErrAlarmListFailed            = "alarm_list_failed"
	ErrAlarmClearFailed           = "alarm_clear_failed"
	ErrAlarmNeedMemberNameAdd     = "alarm_need_member_name_add"
	ErrAlarmNeedMemberNameRemove  = "alarm_need_member_name_remove"
	ErrInvalidAlarmUsage          = "invalid_alarm_usage"

	ErrLiveStreamQueryFailed     = "live_stream_query_failed"
	ErrUpcomingStreamQueryFailed = "upcoming_stream_query_failed"
	ErrScheduleQueryFailed       = "schedule_query_failed"
	ErrScheduleNeedMemberName    = "schedule_need_member_name"

	ErrUnknownStatsPeriod = "unknown_stats_period"
	ErrStatsQueryFailed   = "stats_query_failed"
	MsgNoStatsData        = "no_stats_data"

	ErrSubscriberNeedMemberName = "subscriber_need_member_name"
	ErrSubscriberQueryFailed    = "subscriber_query_failed"
	MsgNoSubscriberData         = "no_subscriber_data"

	ErrCalendarQueryFailed = "calendar_query_failed"

	ErrMajorEventServiceNotInitialized = "major_event_service_not_initialized"
	ErrMajorEventStatusCheckFailed     = "major_event_status_check_failed"
	ErrMajorEventSubscribeFailed       = "major_event_subscribe_failed"
	ErrMajorEventUnsubscribeFailed     = "major_event_unsubscribe_failed"

	ErrMemberNewsServiceNotInitialized = "member_news_service_not_initialized"
	ErrMemberNewsQueryFailed           = "member_news_query_failed"
	ErrMemberNewsSubscriptionFailed    = "member_news_subscription_failed"

	ErrUnknownCommand          = "unknown_command"
	ErrExternalAPICallFailed   = "external_api_call_failed"
	ErrCacheConnectionFailed   = "cache_connection_failed"
	ErrIrisConnectionFailed    = "iris_connection_failed"
	ErrCommandProcessingFailed = "command_processing_failed"
)
