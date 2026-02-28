package alarm

// API 요청/응답 DTO (alarm-dispatcher internal API)

// AddAlarmRequest: 알람 추가 요청
type AddAlarmRequest struct {
	RoomID     string   `json:"room_id" binding:"required"`
	UserID     string   `json:"user_id"`
	ChannelID  string   `json:"channel_id" binding:"required"`
	MemberName string   `json:"member_name"`
	RoomName   string   `json:"room_name"`
	UserName   string   `json:"user_name"`
	AlarmTypes []string `json:"alarm_types"`
}

// RemoveAlarmRequest: 알람 제거 요청
type RemoveAlarmRequest struct {
	RoomID     string   `json:"room_id" binding:"required"`
	ChannelID  string   `json:"channel_id" binding:"required"`
	AlarmTypes []string `json:"alarm_types"`
}

// ClearAlarmsRequest: 방 전체 알람 삭제 요청
type ClearAlarmsRequest struct {
	RoomID string `json:"room_id" binding:"required"`
}

// UpdateAdvanceMinutesRequest: 알림 시간 변경 요청
type UpdateAdvanceMinutesRequest struct {
	Minutes int `json:"minutes" binding:"required,min=1"`
}

// SetRoomNameRequest: 방 이름 설정 요청
type SetRoomNameRequest struct {
	RoomID   string `json:"room_id" binding:"required"`
	RoomName string `json:"room_name" binding:"required"`
}

// SetUserNameRequest: 사용자 이름 설정 요청
type SetUserNameRequest struct {
	UserID   string `json:"user_id" binding:"required"`
	UserName string `json:"user_name" binding:"required"`
}

// APIResponse: 공통 응답 DTO
type APIResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
	Data    any    `json:"data,omitempty"`
}
