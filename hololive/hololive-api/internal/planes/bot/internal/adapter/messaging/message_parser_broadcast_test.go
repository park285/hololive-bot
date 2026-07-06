package messaging

import (
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/park285/iris-client-go/webhook"
)

func TestParseMessage_BroadcastHistory(t *testing.T) {
	adapter := NewMessageAdapter("!", "")
	msg := &webhook.Message{Msg: "!방송이력 게임 20 30일 topic:Forza 페코라"}

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

func TestParseMessage_BroadcastHistoryMembershipAlias(t *testing.T) {
	adapter := NewMessageAdapter("!", "")
	msg := &webhook.Message{Msg: "!방송이력 멤버"}

	result := adapter.ParseMessage(msg)
	if result == nil {
		t.Fatal("expected parsed command, got nil")
	}
	if result.Type != domain.CommandBroadcastHistory {
		t.Fatalf("expected CommandBroadcastHistory, got %s", result.Type)
	}
	if got := result.Params["type"]; got != "멤버" {
		t.Fatalf("type = %v, want 멤버", got)
	}
	if got := result.Params["member"]; got != nil {
		t.Fatalf("member = %v, want nil", got)
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
	if got := result.Params["limit"]; got != 20 {
		t.Fatalf("limit = %v, want 20", got)
	}
	if got := result.Params["member"]; got != nil {
		t.Fatalf("member = %v, want nil", got)
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
