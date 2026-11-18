package alarm

type AddRequest struct {
	RoomID     string   `json:"room_id"`
	UserID     string   `json:"user_id"`
	ChannelID  string   `json:"channel_id"`
	HostID     string   `json:"host_id,omitempty"`
	RoomName   string   `json:"room_name"`
	UserName   string   `json:"user_name"`
	AlarmTypes []string `json:"alarm_types"`
}

type RemoveRequest struct {
	RoomID     string   `json:"room_id"`
	ChannelID  string   `json:"channel_id"`
	HostID     string   `json:"host_id,omitempty"`
	AlarmTypes []string `json:"alarm_types"`
}

type ClearRequest struct {
	RoomID string `json:"room_id"`
}

type UpdateAdvanceMinutesRequest struct {
	Minutes int `json:"minutes"`
}

type SetRoomNameRequest struct {
	RoomID   string  `json:"room_id"`
	RoomName *string `json:"room_name"`
}

type AddResponse struct {
	Added bool `json:"added"`
}

type RemoveResponse struct {
	Removed bool `json:"removed"`
}

type ClearResponse struct {
	Deleted int `json:"deleted"`
}

type MinutesResponse struct {
	TargetMinutes []int `json:"target_minutes"`
}

type Envelope[T any] struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
	Message string `json:"message,omitempty"`
	Data    T      `json:"data,omitempty"`
}
