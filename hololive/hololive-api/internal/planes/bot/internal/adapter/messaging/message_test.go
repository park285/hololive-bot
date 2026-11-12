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

package messaging

import (
	"testing"

	"github.com/park285/iris-client-go/v5/webhook"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/botcommand"
)

func TestParseMessage_CompactAlarmAdd(t *testing.T) {
	adapter := NewMessageAdapter("!")
	msg := &webhook.Message{Msg: "!알람설정 미즈미야"}

	result := adapter.ParseMessage(msg)
	if result == nil {
		t.Fatal("expected parsed command, got nil")
	}

	if result.Type != botcommand.CommandAlarmAdd {
		t.Fatalf("expected CommandAlarmAdd, got %s", result.Type)
	}

	member, ok := result.Params["member"].(string)
	if !ok {
		t.Fatal("expected member param to exist")
	}

	if member != "미즈미야" {
		t.Fatalf("expected member to be '미즈미야', got %s", member)
	}
}

func TestParseMessage_CompactAlarmList(t *testing.T) {
	adapter := NewMessageAdapter("!")
	msg := &webhook.Message{Msg: "!알람목록"}

	result := adapter.ParseMessage(msg)
	if result == nil {
		t.Fatal("expected parsed command, got nil")
	}

	if result.Type != botcommand.CommandAlarmList {
		t.Fatalf("expected CommandAlarmList, got %s", result.Type)
	}
}

func TestParseMessage_InvalidAlarmCommand(t *testing.T) {
	adapter := NewMessageAdapter("!")
	msg := &webhook.Message{Msg: "!알람 설정123"}

	result := adapter.ParseMessage(msg)
	if result == nil {
		t.Fatal("expected parsed command, got nil")
	}

	if result.Type != botcommand.CommandAlarmInvalid {
		t.Fatalf("expected CommandAlarmInvalid, got %s", result.Type)
	}

	action, ok := result.Params["action"].(string)
	if !ok || action != "invalid" {
		t.Fatalf("expected action invalid, got %v", result.Params["action"])
	}
}

func TestParseMessage_UsesConfiguredPrefixOnly(t *testing.T) {
	adapter := NewMessageAdapter("!")
	msg := &webhook.Message{Msg: "/도움"}

	result := adapter.ParseMessage(msg)
	if result == nil {
		t.Fatal("expected parsed command, got nil")
	}

	if result.Type != botcommand.CommandUnknown {
		t.Fatalf("expected CommandUnknown, got %s", result.Type)
	}
}

func TestParseMessage_LeadingZeroWidthBeforePrefix(t *testing.T) {
	adapter := NewMessageAdapter("!")
	msg := &webhook.Message{Msg: "\u200b!도움"}

	result := adapter.ParseMessage(msg)
	if result == nil {
		t.Fatal("expected parsed command, got nil")
	}

	if result.Type != botcommand.CommandHelp {
		t.Fatalf("expected CommandHelp, got %s", result.Type)
	}
}

func TestParseMessage_UpcomingAll(t *testing.T) {
	adapter := NewMessageAdapter("!")
	msg := &webhook.Message{Msg: "!예정 전체"}

	result := adapter.ParseMessage(msg)
	if result == nil {
		t.Fatal("expected parsed command, got nil")
	}

	if result.Type != botcommand.CommandUpcoming {
		t.Fatalf("expected CommandUpcoming, got %s", result.Type)
	}

	all, ok := result.Params["all"].(bool)
	if !ok || !all {
		t.Fatalf("expected all=true, got %v", result.Params["all"])
	}

	if _, exists := result.Params["limit"]; exists {
		t.Fatal("expected limit to be removed when all is set")
	}
}

func TestParseMessage_UpcomingLimit(t *testing.T) {
	adapter := NewMessageAdapter("!")
	msg := &webhook.Message{Msg: "!예정 30"}

	result := adapter.ParseMessage(msg)
	if result == nil {
		t.Fatal("expected parsed command, got nil")
	}

	if result.Type != botcommand.CommandUpcoming {
		t.Fatalf("expected CommandUpcoming, got %s", result.Type)
	}

	limit, ok := result.Params["limit"].(int)
	if !ok || limit != 30 {
		t.Fatalf("expected limit=30, got %v", result.Params["limit"])
	}
}

func TestParseMessage_UpcomingLimitAndMember(t *testing.T) {
	adapter := NewMessageAdapter("!")
	msg := &webhook.Message{Msg: "!예정 30 페코라"}

	result := adapter.ParseMessage(msg)
	if result == nil {
		t.Fatal("expected parsed command, got nil")
	}

	if result.Type != botcommand.CommandUpcoming {
		t.Fatalf("expected CommandUpcoming, got %s", result.Type)
	}

	limit, ok := result.Params["limit"].(int)
	if !ok || limit != 30 {
		t.Fatalf("expected limit=30, got %v", result.Params["limit"])
	}

	member, ok := result.Params["member"].(string)
	if !ok || member != testMemberPekora {
		t.Fatalf("expected member=페코라, got %v", result.Params["member"])
	}
}

func TestParseMessage_ParserPriority_MemberInfoOverScheduleWhenNoArgs(t *testing.T) {
	adapter := NewMessageAdapter("!")
	msg := &webhook.Message{Msg: "!멤버"}

	result := adapter.ParseMessage(msg)
	if result == nil {
		t.Fatal("expected parsed command, got nil")
	}

	if result.Type != botcommand.CommandMemberInfo {
		t.Fatalf("expected CommandMemberInfo, got %s", result.Type)
	}
}

func TestParseMessage_SettlementCommandsIgnoredByMainBot(t *testing.T) {
	adapter := NewMessageAdapter("!")

	tests := []struct {
		name  string
		input string
	}{
		{name: "status", input: "!정산"},
		{name: "paid_compact", input: "!정산완료"},
		{name: "paid_spaced", input: "!정산 완료"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := adapter.ParseMessage(&webhook.Message{Msg: tt.input})
			if result == nil {
				t.Fatal("expected parsed command, got nil")
			}

			if result.Type != botcommand.CommandUnknown {
				t.Fatalf("expected CommandUnknown, got %s", result.Type)
			}
		})
	}
}
