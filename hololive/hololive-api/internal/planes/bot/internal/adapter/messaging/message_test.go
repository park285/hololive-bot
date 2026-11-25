package messaging

import (
	"testing"

	"github.com/park285/iris-client-go/v6/webhook"

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

	if member := typedParams[botcommand.AlarmParams](t, result).Member; member != "미즈미야" {
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

	if params := typedParams[botcommand.AlarmParams](t, result); params != (botcommand.AlarmParams{}) {
		t.Fatalf("expected no alarm arguments, got %#v", params)
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

	if params := typedParams[botcommand.UpcomingParams](t, result); !params.All || params.Limit != 0 {
		t.Fatalf("expected all=true without limit, got %#v", params)
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

	if limit := typedParams[botcommand.UpcomingParams](t, result).Limit; limit != 30 {
		t.Fatalf("expected limit=30, got %v", limit)
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

	if params := typedParams[botcommand.UpcomingParams](t, result); params.Limit != 30 || params.Member != testMemberPekora {
		t.Fatalf("expected limit=30 member=페코라, got %#v", params)
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
