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

const (
	CommandLive                   CommandType = "live"
	CommandUpcoming               CommandType = "upcoming"
	CommandSchedule               CommandType = "schedule"
	CommandHelp                   CommandType = "help"
	CommandAlarmAdd               CommandType = "alarm_add"
	CommandAlarmRemove            CommandType = "alarm_remove"
	CommandAlarmList              CommandType = "alarm_list"
	CommandAlarmClear             CommandType = "alarm_clear"
	CommandAlarmInvalid           CommandType = "alarm_invalid"
	CommandMemberInfo             CommandType = "member_info"
	CommandStats                  CommandType = "stats"
	CommandSubscriber             CommandType = "subscriber"
	CommandMemberNews             CommandType = "member_news"
	CommandMemberNewsSubscription CommandType = "news_subscription"
	CommandMajorEvent             CommandType = "major_event"
	CommandCalendar               CommandType = "calendar"
	CommandBroadcastHistory       CommandType = "broadcast_history"
	CommandBroadcastThumbnail     CommandType = "broadcast_thumbnail"
	CommandUnknown                CommandType = "unknown"
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
		CommandCalendar,
		CommandBroadcastHistory, CommandBroadcastThumbnail,
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
