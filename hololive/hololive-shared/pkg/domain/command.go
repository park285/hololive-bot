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

type CommandType string

// CommandType 상수 목록.
// CommandType 상수 목록.
const (
	// CommandLive: 현재 방송 중인 멤버 조회 명령어
	CommandLive CommandType = "live"
	// CommandUpcoming: 방송 예정인 스트림 조회 명령어
	CommandUpcoming CommandType = "upcoming"
	// CommandSchedule: 전체 일정 조회 명령어
	CommandSchedule CommandType = "schedule"
	// CommandHelp: 도움말 보기 명령어
	CommandHelp CommandType = "help"
	// CommandAlarmAdd: 방송 알림 추가 명령어 (예: "페코라 알림 켜줘")
	CommandAlarmAdd CommandType = "alarm_add"
	// CommandAlarmRemove: 방송 알림 삭제 명령어
	CommandAlarmRemove CommandType = "alarm_remove"
	// CommandAlarmList: 현재 설정된 알림 목록 조회 명령어
	CommandAlarmList CommandType = "alarm_list"
	// CommandAlarmClear: 모든 알림 초기화 명령어
	CommandAlarmClear CommandType = "alarm_clear"
	// CommandAlarmInvalid: 알림 관련 불완전하거나 유효하지 않은 명령어
	CommandAlarmInvalid CommandType = "alarm_invalid"
	// CommandMemberInfo: 멤버 프로필 정보 조회 명령어
	CommandMemberInfo CommandType = "member_info"
	// CommandStats: 통계 정보 조회 명령어
	CommandStats CommandType = "stats"
	// CommandSubscriber: 특정 멤버의 구독자 수 조회 명령어
	CommandSubscriber CommandType = "subscriber"
	// CommandMemberNews: 구독 멤버 뉴스 조회 명령어
	CommandMemberNews CommandType = "member_news"
	// CommandMemberNewsSubscription: 구독 멤버 뉴스 알림 구독/해제/상태 명령어
	CommandMemberNewsSubscription CommandType = "news_subscription"
	// CommandMajorEvent: 대형 행사 알림 관리 명령어 (구독/해제/목록)
	CommandMajorEvent CommandType = "major_event"
	// CommandUnknown: 인식할 수 없는 명령어
	CommandUnknown CommandType = "unknown"
)

func (c CommandType) String() string {
	return string(c)
}

func (c CommandType) IsValid() bool {
	switch c {
	case CommandLive, CommandUpcoming, CommandSchedule, CommandHelp,
		CommandAlarmAdd, CommandAlarmRemove, CommandAlarmList, CommandAlarmClear, CommandAlarmInvalid,
		CommandMemberInfo, CommandStats, CommandSubscriber,
		CommandMemberNews, CommandMemberNewsSubscription,
		CommandMajorEvent,
		CommandUnknown:
		return true
	default:
		return false
	}
}

type ParseResult struct {
	Command    CommandType    `json:"command"`
	Params     map[string]any `json:"params"`
	Confidence float64        `json:"confidence"`
	Reasoning  string         `json:"reasoning"`
}

type ParseResults struct {
	Single   *ParseResult
	Multiple []*ParseResult
}

type ChannelSelection struct {
	SelectedIndex int     `json:"selectedIndex"`
	Confidence    float64 `json:"confidence"`
	Reasoning     string  `json:"reasoning"`
}

func (pr *ParseResults) IsSingle() bool {
	return pr.Single != nil
}

func (pr *ParseResults) IsMultiple() bool {
	return len(pr.Multiple) > 0
}

func (pr *ParseResults) GetCommands() []*ParseResult {
	if pr.IsSingle() {
		return []*ParseResult{pr.Single}
	}
	return pr.Multiple
}
