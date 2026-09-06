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

package alarm

// API 요청/응답 DTO (alarm-dispatcher internal API)

// AddAlarmRequest는 기존 채널 ID와 선택적인 UNIT B 멤버 ID로 구독을 지정한다.
type AddAlarmRequest struct {
	RoomID     string   `json:"room_id" binding:"required"`
	UserID     string   `json:"user_id"`
	ChannelID  string   `json:"channel_id" binding:"required"`
	HostID     string   `json:"host_id,omitempty"`
	MemberName string   `json:"member_name"`
	RoomName   string   `json:"room_name"`
	UserName   string   `json:"user_name"`
	AlarmTypes []string `json:"alarm_types"`
}

// RemoveAlarmRequest는 지정한 채널 또는 멤버 구독에서 해지할 알림 종류를 담는다.
type RemoveAlarmRequest struct {
	RoomID     string   `json:"room_id" binding:"required"`
	ChannelID  string   `json:"channel_id" binding:"required"`
	HostID     string   `json:"host_id,omitempty"`
	AlarmTypes []string `json:"alarm_types"`
}

type ClearAlarmsRequest struct {
	RoomID string `json:"room_id" binding:"required"`
}

type UpdateAdvanceMinutesRequest struct {
	Minutes int `json:"minutes" binding:"required,min=1"`
}

type SetRoomNameRequest struct {
	RoomID   string `json:"room_id" binding:"required"`
	RoomName string `json:"room_name" binding:"required"`
}

type SetUserNameRequest struct {
	UserID   string `json:"user_id" binding:"required"`
	UserName string `json:"user_name" binding:"required"`
}

type APIResponse struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
	Message string `json:"message,omitempty"`
	Data    any    `json:"data,omitempty"`
}

func alarmAPIError(code, message string) APIResponse {
	return APIResponse{Success: false, Error: code, Message: message}
}
