package botcommand

type CommandType string

const (
	CommandLive               CommandType = "live"
	CommandUpcoming           CommandType = "upcoming"
	CommandSchedule           CommandType = "schedule"
	CommandSchedulePoster     CommandType = "schedule_poster"
	CommandEventGrade         CommandType = "event_grade"
	CommandHelp               CommandType = "help"
	CommandAlarmAdd           CommandType = "alarm_add"
	CommandAlarmRemove        CommandType = "alarm_remove"
	CommandAlarmList          CommandType = "alarm_list"
	CommandAlarmClear         CommandType = "alarm_clear"
	CommandAlarmInvalid       CommandType = "alarm_invalid"
	CommandMemberInfo         CommandType = "member_info"
	CommandCalendar           CommandType = "calendar"
	CommandBroadcastHistory   CommandType = "broadcast_history"
	CommandBroadcastThumbnail CommandType = "broadcast_thumbnail"
	CommandBroadcastTime      CommandType = "broadcast_time"
	CommandUnknown            CommandType = "unknown"
)

func (c CommandType) String() string {
	return string(c)
}
