package alarm

// worker 알림 internal HTTP API의 요청·응답 wire 형식이다. hololive-api의 alarmhttp client와 alarm-worker handler가
// 같은 타입을 쓴다. 필수 값 검사는 이 타입에 두지 않고 worker handler가 명시적으로 한다.

// AddRequest는 기존 채널 ID와 선택적인 UNIT B 멤버 ID로 구독을 지정한다.
type AddRequest struct {
	RoomID     string   `json:"room_id"`
	UserID     string   `json:"user_id"`
	ChannelID  string   `json:"channel_id"`
	HostID     string   `json:"host_id,omitempty"`
	RoomName   string   `json:"room_name"`
	UserName   string   `json:"user_name"`
	AlarmTypes []string `json:"alarm_types"`
}

// RemoveRequest는 지정한 채널 또는 멤버 구독에서 해지할 알림 종류를 담는다.
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

// SetRoomNameRequest의 room_name은 필드 자체가 필수이고(nil이면 거부), 공백뿐인 값은 관리자 지정 이름 해제를 뜻한다.
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

// Envelope는 모든 응답의 공통 봉투다. 서버(worker)는 Data에 응답 값을 그대로 넣고, client는 jsontext.Value로 받아 경로별 타입으로 푼다.
type Envelope[T any] struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
	Message string `json:"message,omitempty"`
	Data    T      `json:"data,omitempty"`
}
