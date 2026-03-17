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

package adapter

import (
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/iris"
)

func TestParseMessage_CompactAlarmAdd(t *testing.T) {
	adapter := NewMessageAdapter("!", "")
	msg := &iris.Message{Msg: "!알람설정 미즈미야"}

	result := adapter.ParseMessage(msg)
	if result == nil {
		t.Fatal("expected parsed command, got nil")
	}

	if result.Type != domain.CommandAlarmAdd {
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
	adapter := NewMessageAdapter("!", "")
	msg := &iris.Message{Msg: "!알람목록"}

	result := adapter.ParseMessage(msg)
	if result == nil {
		t.Fatal("expected parsed command, got nil")
	}

	if result.Type != domain.CommandAlarmList {
		t.Fatalf("expected CommandAlarmList, got %s", result.Type)
	}
}

func TestParseMessage_InvalidAlarmCommand(t *testing.T) {
	adapter := NewMessageAdapter("!", "")
	msg := &iris.Message{Msg: "!알람 설정123"}

	result := adapter.ParseMessage(msg)
	if result == nil {
		t.Fatal("expected parsed command, got nil")
	}

	if result.Type != domain.CommandAlarmInvalid {
		t.Fatalf("expected CommandAlarmInvalid, got %s", result.Type)
	}

	action, ok := result.Params["action"].(string)
	if !ok || action != "invalid" {
		t.Fatalf("expected action invalid, got %v", result.Params["action"])
	}
}

func TestParseMessage_UsesConfiguredPrefixOnly(t *testing.T) {
	adapter := NewMessageAdapter("!", "")
	msg := &iris.Message{Msg: "/도움"}

	result := adapter.ParseMessage(msg)
	if result == nil {
		t.Fatal("expected parsed command, got nil")
	}

	if result.Type != domain.CommandUnknown {
		t.Fatalf("expected CommandUnknown, got %s", result.Type)
	}
}

func TestParseMessage_LeadingZeroWidthBeforePrefix(t *testing.T) {
	adapter := NewMessageAdapter("!", "")
	msg := &iris.Message{Msg: "\u200b!도움"}

	result := adapter.ParseMessage(msg)
	if result == nil {
		t.Fatal("expected parsed command, got nil")
	}

	if result.Type != domain.CommandHelp {
		t.Fatalf("expected CommandHelp, got %s", result.Type)
	}
}

func TestParseMessage_UpcomingAll(t *testing.T) {
	adapter := NewMessageAdapter("!", "")
	msg := &iris.Message{Msg: "!예정 전체"}

	result := adapter.ParseMessage(msg)
	if result == nil {
		t.Fatal("expected parsed command, got nil")
	}

	if result.Type != domain.CommandUpcoming {
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
	adapter := NewMessageAdapter("!", "")
	msg := &iris.Message{Msg: "!예정 30"}

	result := adapter.ParseMessage(msg)
	if result == nil {
		t.Fatal("expected parsed command, got nil")
	}

	if result.Type != domain.CommandUpcoming {
		t.Fatalf("expected CommandUpcoming, got %s", result.Type)
	}

	limit, ok := result.Params["limit"].(int)
	if !ok || limit != 30 {
		t.Fatalf("expected limit=30, got %v", result.Params["limit"])
	}
}

func TestParseMessage_UpcomingLimitAndMember(t *testing.T) {
	adapter := NewMessageAdapter("!", "")
	msg := &iris.Message{Msg: "!예정 30 페코라"}

	result := adapter.ParseMessage(msg)
	if result == nil {
		t.Fatal("expected parsed command, got nil")
	}

	if result.Type != domain.CommandUpcoming {
		t.Fatalf("expected CommandUpcoming, got %s", result.Type)
	}

	limit, ok := result.Params["limit"].(int)
	if !ok || limit != 30 {
		t.Fatalf("expected limit=30, got %v", result.Params["limit"])
	}

	member, ok := result.Params["member"].(string)
	if !ok || member != "페코라" {
		t.Fatalf("expected member=페코라, got %v", result.Params["member"])
	}
}

func TestParseMessage_MemberNewsDefaultPeriod(t *testing.T) {
	adapter := NewMessageAdapter("!", "")
	msg := &iris.Message{Msg: "!뉴스"}

	result := adapter.ParseMessage(msg)
	if result == nil {
		t.Fatal("expected parsed command, got nil")
	}

	if result.Type != domain.CommandMemberNews {
		t.Fatalf("expected CommandMemberNews, got %s", result.Type)
	}

	period, ok := result.Params["period"].(string)
	if !ok || period != "weekly" {
		t.Fatalf("expected period=weekly, got %v", result.Params["period"])
	}
}

func TestParseMessage_MemberNewsMonthlyPeriod(t *testing.T) {
	adapter := NewMessageAdapter("!", "")
	msg := &iris.Message{Msg: "!뉴스 이번달"}

	result := adapter.ParseMessage(msg)
	if result == nil {
		t.Fatal("expected parsed command, got nil")
	}

	if result.Type != domain.CommandMemberNews {
		t.Fatalf("expected CommandMemberNews, got %s", result.Type)
	}

	period, ok := result.Params["period"].(string)
	if !ok || period != "monthly" {
		t.Fatalf("expected period=monthly, got %v", result.Params["period"])
	}
}

func TestParseMessage_MemberNewsSubscriptionOn(t *testing.T) {
	adapter := NewMessageAdapter("!", "")
	msg := &iris.Message{Msg: "!뉴스알림 켜기"}

	result := adapter.ParseMessage(msg)
	if result == nil {
		t.Fatal("expected parsed command, got nil")
	}

	if result.Type != domain.CommandMemberNewsSubscription {
		t.Fatalf("expected CommandMemberNewsSubscription, got %s", result.Type)
	}

	action, ok := result.Params["action"].(string)
	if !ok || action != "on" {
		t.Fatalf("expected action=on, got %v", result.Params["action"])
	}
}

func TestParseMessage_MemberNewsSubscriptionOff(t *testing.T) {
	adapter := NewMessageAdapter("!", "")
	msg := &iris.Message{Msg: "!뉴스알림 끄기"}

	result := adapter.ParseMessage(msg)
	if result == nil {
		t.Fatal("expected parsed command, got nil")
	}

	if result.Type != domain.CommandMemberNewsSubscription {
		t.Fatalf("expected CommandMemberNewsSubscription, got %s", result.Type)
	}

	action, ok := result.Params["action"].(string)
	if !ok || action != "off" {
		t.Fatalf("expected action=off, got %v", result.Params["action"])
	}
}

func TestParseMessage_MemberNewsSubscriptionStatus(t *testing.T) {
	adapter := NewMessageAdapter("!", "")
	msg := &iris.Message{Msg: "!뉴스알림 상태"}

	result := adapter.ParseMessage(msg)
	if result == nil {
		t.Fatal("expected parsed command, got nil")
	}

	if result.Type != domain.CommandMemberNewsSubscription {
		t.Fatalf("expected CommandMemberNewsSubscription, got %s", result.Type)
	}

	action, ok := result.Params["action"].(string)
	if !ok || action != "status" {
		t.Fatalf("expected action=status, got %v", result.Params["action"])
	}
}

func TestParseMessage_MajorEventNotMisclassifiedAsNews(t *testing.T) {
	adapter := NewMessageAdapter("!", "")
	msg := &iris.Message{Msg: "!행사알림 상태"}

	result := adapter.ParseMessage(msg)
	if result == nil {
		t.Fatal("expected parsed command, got nil")
	}

	if result.Type != domain.CommandMajorEvent {
		t.Fatalf("expected CommandMajorEvent, got %s", result.Type)
	}
}

func TestParseMessage_MemberNewsMonthlyWithSpace(t *testing.T) {
	adapter := NewMessageAdapter("!", "")

	// "이번 달" — 공백 포함 입력이 monthly로 파싱되는지 검증 (회귀 방지)
	tests := []struct {
		input string
		want  string
	}{
		{"!뉴스 이번달", "monthly"},
		{"!뉴스 이번 달", "monthly"},
		{"!뉴스 월간", "monthly"},
		{"!뉴스 이번주", "weekly"},
		{"!뉴스 이번 주", "weekly"},
		{"!뉴스 주간", "weekly"},
	}

	for _, tt := range tests {
		msg := &iris.Message{Msg: tt.input}

		result := adapter.ParseMessage(msg)
		if result == nil {
			t.Fatalf("input %q: expected parsed command, got nil", tt.input)
		}

		if result.Type != domain.CommandMemberNews {
			t.Fatalf("input %q: expected CommandMemberNews, got %s", tt.input, result.Type)
		}

		period, ok := result.Params["period"].(string)
		if !ok || period != tt.want {
			t.Fatalf("input %q: expected period=%s, got %v", tt.input, tt.want, result.Params["period"])
		}
	}
}

func TestParseMessage_ParserPriority_MemberInfoOverScheduleWhenNoArgs(t *testing.T) {
	adapter := NewMessageAdapter("!", "")
	msg := &iris.Message{Msg: "!멤버"}

	result := adapter.ParseMessage(msg)
	if result == nil {
		t.Fatal("expected parsed command, got nil")
	}

	if result.Type != domain.CommandMemberInfo {
		t.Fatalf("expected CommandMemberInfo, got %s", result.Type)
	}
}

func TestParseMessage_ParserPriority_NewsSubscriptionOverNews(t *testing.T) {
	adapter := NewMessageAdapter("!", "")
	msg := &iris.Message{Msg: "!뉴스알림"}

	result := adapter.ParseMessage(msg)
	if result == nil {
		t.Fatal("expected parsed command, got nil")
	}

	if result.Type != domain.CommandMemberNewsSubscription {
		t.Fatalf("expected CommandMemberNewsSubscription, got %s", result.Type)
	}

	action, ok := result.Params["action"].(string)
	if !ok || action != "status" {
		t.Fatalf("expected action=status, got %v", result.Params["action"])
	}
}

func TestParseMessage_SettlementCommandsIgnoredByMainBot(t *testing.T) {
	adapter := NewMessageAdapter("!", "")

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
			result := adapter.ParseMessage(&iris.Message{Msg: tt.input})
			if result == nil {
				t.Fatal("expected parsed command, got nil")
			}

			if result.Type != domain.CommandUnknown {
				t.Fatalf("expected CommandUnknown, got %s", result.Type)
			}
		})
	}
}
