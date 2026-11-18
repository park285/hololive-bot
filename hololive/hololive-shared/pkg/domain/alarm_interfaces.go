package domain

type AddAlarmRequest struct {
	RoomID     string
	UserID     string
	ChannelID  string
	HostID     string
	RoomName   string
	UserName   string
	AlarmTypes AlarmTypes
}

type AlarmEntry struct {
	RoomID     string `json:"roomId"`
	RoomName   string `json:"roomName"`
	ChannelID  string `json:"channelId"`
	MemberName string `json:"memberName"`
}

type AlarmListView struct {
	ChannelID  string
	HostID     string `json:",omitempty"`
	MemberName string
	AlarmTypes AlarmTypes
}

type ApplyOutcome string

const (
	ApplyConfirmed ApplyOutcome = "applied"
	ApplyRejected  ApplyOutcome = "not_applied"
	ApplyUnknown   ApplyOutcome = "outcome_unknown"
)

type AdvanceMinutesResult struct {
	RequestedMinutes int
	Outcome          ApplyOutcome
	TargetMinutes    []int
}
