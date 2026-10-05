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

package domain

// AddAlarmRequest는 채팅방의 구독 대상을 지정하며 빈 HostID는 전체 채널을 뜻한다.
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

// AlarmListView는 채팅방에서 구독한 대상과 알림 종류를 표시한다.
type AlarmListView struct {
	ChannelID  string
	HostID     string `json:",omitempty"`
	MemberName string
	AlarmTypes AlarmTypes
}

// ApplyOutcome은 변경 적용 여부를 확인한 근거를 구분한다.
type ApplyOutcome string

const (
	ApplyConfirmed ApplyOutcome = "applied"
	ApplyRejected  ApplyOutcome = "not_applied"
	ApplyUnknown   ApplyOutcome = "outcome_unknown"
)

// AdvanceMinutesResult의 TargetMinutes는 이번 호출에서 적용을 확인한 값만 담는다.
type AdvanceMinutesResult struct {
	RequestedMinutes int
	Outcome          ApplyOutcome
	TargetMinutes    []int
}
