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

package botcommand_test

import (
	"testing"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/botcommand"
)

func TestCommandType_IsValid(t *testing.T) {
	t.Parallel()

	// 유효한 명령어 상수
	validCmds := []botcommand.CommandType{
		botcommand.CommandLive,
		botcommand.CommandUpcoming,
		botcommand.CommandSchedule,
		botcommand.CommandHelp,
		botcommand.CommandAlarmAdd,
		botcommand.CommandAlarmRemove,
		botcommand.CommandAlarmList,
		botcommand.CommandAlarmClear,
		botcommand.CommandAlarmInvalid,
		botcommand.CommandMemberInfo,
		botcommand.CommandMemberNews,
		botcommand.CommandMemberNewsSubscription,
		botcommand.CommandMajorEvent,
		botcommand.CommandCalendar,
		botcommand.CommandBroadcastHistory,
		botcommand.CommandBroadcastThumbnail,
		botcommand.CommandUnknown,
	}

	for _, cmd := range validCmds {
		t.Run(string(cmd)+"_유효", func(t *testing.T) {
			t.Parallel()

			if !cmd.IsValid() {
				t.Errorf("IsValid() = false, want true (cmd=%q)", cmd)
			}
		})
	}

	invalidCmds := []struct {
		name string
		cmd  botcommand.CommandType
	}{
		{"정의되지 않은 명령어", botcommand.CommandType("invalid_cmd")},
		{"빈 문자열", botcommand.CommandType("")},
		{"제거된 legacy status 명령어", botcommand.CommandType("settlement" + "_status")},
		{"제거된 legacy paid 명령어", botcommand.CommandType("settlement" + "_paid")},
		{"제거된 legacy register 명령어", botcommand.CommandType("settlement" + "_register")},
	}

	for _, tt := range invalidCmds {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if tt.cmd.IsValid() {
				t.Errorf("IsValid() = true, want false (cmd=%q)", tt.cmd)
			}
		})
	}
}

func TestCommandType_String(t *testing.T) {
	t.Parallel()

	// CommandLive → "live" 검증
	got := botcommand.CommandLive.String()
	if got != "live" {
		t.Errorf("CommandLive.String() = %q, want %q", got, "live")
	}
}
