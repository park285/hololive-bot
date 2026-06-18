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

package orchcmd

import "github.com/kapu/hololive-shared/pkg/domain"

func ShouldExecuteAsync(cmdType domain.CommandType) bool {
	if !cmdType.IsValid() {
		// 외부 플러그인/추가 명령은 상태형일 수 있으므로 보수적으로 직렬 실행합니다.
		return false
	}

	switch cmdType {
	case domain.CommandHelp,
		domain.CommandLive,
		domain.CommandUpcoming,
		domain.CommandSchedule,
		domain.CommandMemberInfo,
		domain.CommandStats,
		domain.CommandSubscriber,
		domain.CommandCalendar:
		return true
	case domain.CommandAlarmAdd,
		domain.CommandAlarmRemove,
		domain.CommandAlarmList,
		domain.CommandAlarmClear,
		domain.CommandAlarmInvalid,
		domain.CommandMemberNews,
		domain.CommandMemberNewsSubscription,
		domain.CommandMajorEvent,
		domain.CommandUnknown:
		// 상태형(알람/구독/뉴스 다이제스트 등)은 room 순서를 보장해야 하므로 직렬 실행합니다.
		return false
	default:
		return false
	}
}
