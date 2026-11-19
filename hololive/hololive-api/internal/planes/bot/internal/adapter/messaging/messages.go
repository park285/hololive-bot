package messaging

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

	ErrSchedulePosterNeedMemberName = "schedule_poster_need_member_name"
	ErrSchedulePosterQueryFailed    = "schedule_poster_query_failed"

	ErrCalendarQueryFailed = "calendar_query_failed"

	ErrBroadcastHistoryQueryFailed      = "broadcast_history_query_failed"
	ErrBroadcastTimeQueryFailed         = "broadcast_time_query_failed"
	ErrBroadcastTypeUnknown             = "broadcast_type_unknown"
	ErrBroadcastThumbnailUsage          = "broadcast_thumbnail_usage"
	ErrBroadcastThumbnailNotFound       = "broadcast_thumbnail_not_found"
	ErrBroadcastThumbnailDownloadFailed = "broadcast_thumbnail_download_failed"
	ErrCommandRateLimited               = "command_rate_limited"

	ErrUnknownCommand          = "unknown_command"
	ErrIrisConnectionFailed    = "iris_connection_failed"
	ErrCommandProcessingFailed = "command_processing_failed"
)

func ErrorMessageKeys() []string {
	return []string{
		ErrMemberProfileLoadFailed, ErrMemberProfileBuildFailed, ErrNoMemberInfoFound, ErrCannotDisplayMemberInfo,
		ErrGraduatedMemberBlocked,
		ErrAlarmServiceNotInitialized, ErrAlarmAddFailed, ErrAlarmRemoveFailed, ErrAlarmListFailed, ErrAlarmClearFailed,
		ErrAlarmNeedMemberNameAdd, ErrAlarmNeedMemberNameRemove, ErrInvalidAlarmUsage,
		ErrLiveStreamQueryFailed, ErrUpcomingStreamQueryFailed, ErrScheduleQueryFailed, ErrScheduleNeedMemberName,
		ErrSchedulePosterNeedMemberName, ErrSchedulePosterQueryFailed,
		ErrCalendarQueryFailed,
		ErrBroadcastHistoryQueryFailed, ErrBroadcastTimeQueryFailed, ErrBroadcastTypeUnknown,
		ErrBroadcastThumbnailUsage, ErrBroadcastThumbnailNotFound, ErrBroadcastThumbnailDownloadFailed,
		ErrCommandRateLimited,
		ErrUnknownCommand, ErrIrisConnectionFailed,
		ErrCommandProcessingFailed,
	}
}
