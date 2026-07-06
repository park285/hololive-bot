package messaging

import (
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/park285/iris-client-go/webhook"
)

func TestParseMessage_BroadcastHistory(t *testing.T) {
	adapter := NewMessageAdapter("!", "")
	msg := &webhook.Message{Msg: "!방송이력 게임 30일 20 topic:Forza 페코라"}

	result := adapter.ParseMessage(msg)
	if result == nil {
		t.Fatal("expected parsed command, got nil")
	}
	if result.Type != domain.CommandBroadcastHistory {
		t.Fatalf("expected CommandBroadcastHistory, got %s", result.Type)
	}
	if got := result.Params["type"]; got != "게임" {
		t.Fatalf("type = %v, want 게임", got)
	}
	if got := result.Params["limit"]; got != 20 {
		t.Fatalf("limit = %v, want 20", got)
	}
	if got := result.Params["days"]; got != 30 {
		t.Fatalf("days = %v, want 30", got)
	}
	if got := result.Params["topic"]; got != "Forza" {
		t.Fatalf("topic = %v, want Forza", got)
	}
	if got := result.Params["member"]; got != "페코라" {
		t.Fatalf("member = %v, want 페코라", got)
	}
}

func TestParseMessage_BroadcastHistoryCategoryAlias(t *testing.T) {
	adapter := NewMessageAdapter("!", "")
	msg := &webhook.Message{Msg: "!방송이력 카테고리:잡담 멤버:페코라"}

	result := adapter.ParseMessage(msg)
	if result == nil {
		t.Fatal("expected parsed command, got nil")
	}
	if result.Type != domain.CommandBroadcastHistory {
		t.Fatalf("expected CommandBroadcastHistory, got %s", result.Type)
	}
	if got := result.Params["type"]; got != "잡담" {
		t.Fatalf("type = %v, want 잡담", got)
	}
	if got := result.Params["member"]; got != "페코라" {
		t.Fatalf("member = %v, want 페코라", got)
	}
}

func TestParseMessage_BroadcastHistoryMembershipTypeAlias(t *testing.T) {
	adapter := NewMessageAdapter("!", "")
	msg := &webhook.Message{Msg: "!방송이력 멤버십"}

	result := adapter.ParseMessage(msg)
	if result == nil {
		t.Fatal("expected parsed command, got nil")
	}
	if result.Type != domain.CommandBroadcastHistory {
		t.Fatalf("expected CommandBroadcastHistory, got %s", result.Type)
	}
	if got := result.Params["type"]; got != "멤버십" {
		t.Fatalf("type = %v, want 멤버십", got)
	}
	if got := result.Params["member"]; got != nil {
		t.Fatalf("member = %v, want nil", got)
	}
}

func TestParseMessage_BroadcastHistoryBareMemberNameAndType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		msg        string
		wantMember string
		wantType   string
	}{
		{name: "member then type with command alias", msg: "!방송기록 사쿠라 미코 게임", wantMember: "사쿠라 미코", wantType: "게임"},
		{name: "type then member with command alias", msg: "!방송기록 게임 사쿠라 미코", wantMember: "사쿠라 미코", wantType: "게임"},
		{name: "member then broadcast type filter", msg: "!방송기록 페코라 방송타입:게임", wantMember: "페코라", wantType: "게임"},
		{name: "member then membership type", msg: "!방송기록 사쿠라 미코 멤버십", wantMember: "사쿠라 미코", wantType: "멤버십"},
		{name: "bare korean member token is member filter", msg: "!방송기록 멤버", wantMember: "멤버", wantType: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			adapter := NewMessageAdapter("!", "")
			result := adapter.ParseMessage(&webhook.Message{Msg: tt.msg})
			if result == nil {
				t.Fatal("expected parsed command, got nil")
			}
			if result.Type != domain.CommandBroadcastHistory {
				t.Fatalf("expected CommandBroadcastHistory, got %s", result.Type)
			}
			if got := result.Params["member"]; got != tt.wantMember {
				t.Fatalf("member = %v, want %s", got, tt.wantMember)
			}
			if tt.wantType == "" {
				if got := result.Params["type"]; got != nil {
					t.Fatalf("type = %v, want nil", got)
				}
				return
			}
			if got := result.Params["type"]; got != tt.wantType {
				t.Fatalf("type = %v, want %s", got, tt.wantType)
			}
		})
	}
}

func TestParseMessage_BroadcastHistoryHorseRacingAlias(t *testing.T) {
	adapter := NewMessageAdapter("!", "")
	msg := &webhook.Message{Msg: "!방송이력 경마 20"}

	result := adapter.ParseMessage(msg)
	if result == nil {
		t.Fatal("expected parsed command, got nil")
	}
	if result.Type != domain.CommandBroadcastHistory {
		t.Fatalf("expected CommandBroadcastHistory, got %s", result.Type)
	}
	if got := result.Params["type"]; got != "경마" {
		t.Fatalf("type = %v, want 경마", got)
	}
	if got := result.Params["days"]; got != 20 {
		t.Fatalf("days = %v, want 20", got)
	}
	if got := result.Params["limit"]; got != nil {
		t.Fatalf("limit = %v, want nil", got)
	}
	if got := result.Params["member"]; got != nil {
		t.Fatalf("member = %v, want nil", got)
	}
}

func TestParseMessage_BroadcastHistoryExplicitLimitFilter(t *testing.T) {
	adapter := NewMessageAdapter("!", "")
	msg := &webhook.Message{Msg: "!방송이력 경마 limit:20"}

	result := adapter.ParseMessage(msg)
	if result == nil {
		t.Fatal("expected parsed command, got nil")
	}
	if result.Type != domain.CommandBroadcastHistory {
		t.Fatalf("expected CommandBroadcastHistory, got %s", result.Type)
	}
	if got := result.Params["type"]; got != "경마" {
		t.Fatalf("type = %v, want 경마", got)
	}
	if got := result.Params["limit"]; got != 20 {
		t.Fatalf("limit = %v, want 20", got)
	}
}

func TestParseMessage_BroadcastHistoryLegacyLimitBeforeDays(t *testing.T) {
	adapter := NewMessageAdapter("!", "")
	msg := &webhook.Message{Msg: "!방송이력 게임 20 기간:30"}

	result := adapter.ParseMessage(msg)
	if result == nil {
		t.Fatal("expected parsed command, got nil")
	}
	if result.Type != domain.CommandBroadcastHistory {
		t.Fatalf("expected CommandBroadcastHistory, got %s", result.Type)
	}
	if got := result.Params["type"]; got != "게임" {
		t.Fatalf("type = %v, want 게임", got)
	}
	if got := result.Params["limit"]; got != 20 {
		t.Fatalf("limit = %v, want 20", got)
	}
	if got := result.Params["days"]; got != 30 {
		t.Fatalf("days = %v, want 30", got)
	}
}

func TestParseMessage_BroadcastHistoryAliasVariants(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		msg  string
		want string
	}{
		{name: "free talk underscore", msg: "!방송이력 free_talk", want: "free_talk"},
		{name: "watch party hyphen", msg: "!방송이력 watch-party", want: "watch-party"},
		{name: "watch along hyphen", msg: "!방송이력 watch-along", want: "watch-along"},
		{name: "membership korean", msg: "!방송이력 멤버십", want: "멤버십"},
		{name: "horse racing korean", msg: "!방송이력 경마", want: "경마"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			adapter := NewMessageAdapter("!", "")
			result := adapter.ParseMessage(&webhook.Message{Msg: tt.msg})
			if result == nil {
				t.Fatal("expected parsed command, got nil")
			}
			if result.Type != domain.CommandBroadcastHistory {
				t.Fatalf("expected CommandBroadcastHistory, got %s", result.Type)
			}
			if got := result.Params["type"]; got != tt.want {
				t.Fatalf("type = %v, want %s", got, tt.want)
			}
			if got := result.Params["member"]; got != nil {
				t.Fatalf("member = %v, want nil", got)
			}
		})
	}
}

func TestParseMessage_BroadcastHistorySeparatedFilters(t *testing.T) {
	adapter := NewMessageAdapter("!", "")
	msg := &webhook.Message{Msg: "!방송이력 카테고리: 게임 멤버: 사쿠라 미코 14일 10"}

	result := adapter.ParseMessage(msg)
	if result == nil {
		t.Fatal("expected parsed command, got nil")
	}
	if result.Type != domain.CommandBroadcastHistory {
		t.Fatalf("expected CommandBroadcastHistory, got %s", result.Type)
	}
	if got := result.Params["type"]; got != "게임" {
		t.Fatalf("type = %v, want 게임", got)
	}
	if got := result.Params["member"]; got != "사쿠라 미코" {
		t.Fatalf("member = %v, want 사쿠라 미코", got)
	}
	if got := result.Params["days"]; got != 14 {
		t.Fatalf("days = %v, want 14", got)
	}
	if got := result.Params["limit"]; got != 10 {
		t.Fatalf("limit = %v, want 10", got)
	}
}

func TestParseMessage_BroadcastHistoryAttachedMemberFilterConsumesNameUntilType(t *testing.T) {
	adapter := NewMessageAdapter("!", "")
	msg := &webhook.Message{Msg: "!방송기록 멤버:사쿠라 미코 게임"}

	result := adapter.ParseMessage(msg)
	if result == nil {
		t.Fatal("expected parsed command, got nil")
	}
	if result.Type != domain.CommandBroadcastHistory {
		t.Fatalf("expected CommandBroadcastHistory, got %s", result.Type)
	}
	if got := result.Params["member"]; got != "사쿠라 미코" {
		t.Fatalf("member = %v, want 사쿠라 미코", got)
	}
	if got := result.Params["type"]; got != "게임" {
		t.Fatalf("type = %v, want 게임", got)
	}
}

func TestParseMessage_BroadcastHistoryExplicitMemberFilterWinsOverTrailingBareToken(t *testing.T) {
	adapter := NewMessageAdapter("!", "")
	msg := &webhook.Message{Msg: "!방송기록 멤버:페코라 타입:게임 미코"}

	result := adapter.ParseMessage(msg)
	if result == nil {
		t.Fatal("expected parsed command, got nil")
	}
	if result.Type != domain.CommandBroadcastHistory {
		t.Fatalf("expected CommandBroadcastHistory, got %s", result.Type)
	}
	if got := result.Params["member"]; got != "페코라" {
		t.Fatalf("member = %v, want 페코라", got)
	}
	if got := result.Params["type"]; got != "게임" {
		t.Fatalf("type = %v, want 게임", got)
	}
}

func TestParseMessage_BroadcastThumbnail(t *testing.T) {
	adapter := NewMessageAdapter("!", "")
	msg := &webhook.Message{Msg: "!방송이력 썸네일 AqxEw3kXcgU"}

	result := adapter.ParseMessage(msg)
	if result == nil {
		t.Fatal("expected parsed command, got nil")
	}
	if result.Type != domain.CommandBroadcastThumbnail {
		t.Fatalf("expected CommandBroadcastThumbnail, got %s", result.Type)
	}
	if got := result.Params["video_id"]; got != "AqxEw3kXcgU" {
		t.Fatalf("video_id = %v, want AqxEw3kXcgU", got)
	}
}

func TestParseMessage_StandaloneBroadcastThumbnail(t *testing.T) {
	adapter := NewMessageAdapter("!", "")
	msg := &webhook.Message{Msg: "!썸네일 AqxEw3kXcgU"}

	result := adapter.ParseMessage(msg)
	if result == nil {
		t.Fatal("expected parsed command, got nil")
	}
	if result.Type != domain.CommandBroadcastThumbnail {
		t.Fatalf("expected CommandBroadcastThumbnail, got %s", result.Type)
	}
	if got := result.Params["video_id"]; got != "AqxEw3kXcgU" {
		t.Fatalf("video_id = %v, want AqxEw3kXcgU", got)
	}
}
