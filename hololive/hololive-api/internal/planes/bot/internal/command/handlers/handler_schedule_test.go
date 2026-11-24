package handlers

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging/formatter"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/botcommand"
	handlercore "github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers/handlercore"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/service/matcher"
	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
	serviceTemplate "github.com/kapu/hololive-shared/pkg/service/template"
)

type scheduleStreamProviderStub struct {
	scheduleStreams []*domain.Stream
	scheduleErr     error
}

func (s *scheduleStreamProviderStub) GetUpcomingStreams(_ context.Context, _ int) ([]*domain.Stream, error) {
	return nil, nil
}

func (s *scheduleStreamProviderStub) GetChannelSchedule(_ context.Context, _ string, _ int, _ bool) ([]*domain.Stream, error) {
	return s.scheduleStreams, s.scheduleErr
}

func setupScheduleTestRenderer(t *testing.T) *serviceTemplate.Renderer {
	t.Helper()

	pool := dbtest.NewPool(t)
	if _, err := pool.Exec(t.Context(), `DELETE FROM notification_templates`); err != nil {
		t.Fatalf("clear templates: %v", err)
	}

	if _, err := pool.Exec(t.Context(), `
		INSERT INTO notification_templates(template_key, channel_id, format_variant, body)
		SELECT $1, NULL, variant, $2 FROM unnest(ARRAY['plain', 'richtext']) AS variant
		ON CONFLICT (template_key, format_variant) WHERE channel_id IS NULL
		DO UPDATE SET body = EXCLUDED.body, updated_at = NOW()
	`, domain.TemplateKeyCmdChannelSchedule, "채널 일정\n{{range .Streams}}{{.Title}}\n{{end}}"); err != nil {
		t.Fatalf("seed schedule template: %v", err)
	}

	return serviceTemplate.NewRenderer(pool, slog.New(slog.DiscardHandler))
}

func TestScheduleCommand_Name(t *testing.T) {
	cmd := NewScheduleCommand(nil)
	if cmd.Name() != "schedule" {
		t.Fatalf("Name() = %q, want %q", cmd.Name(), "schedule")
	}
}

func TestScheduleCommand_Description(t *testing.T) {
	cmd := NewScheduleCommand(nil)
	if cmd.Description() == "" {
		t.Fatal("Description() should not be empty")
	}
}

func TestScheduleCommand_Execute_GoldenPath(t *testing.T) {
	var sentMessage string

	memberProvider := newContextAwareMemberProvider([]*domain.Member{{
		ChannelID: testChannelMiko,
		Name:      "미코",
	}})

	holodex := &scheduleStreamProviderStub{
		scheduleStreams: []*domain.Stream{
			{ID: "s1", Title: "미코 일정 방송", ChannelID: testChannelMiko, ChannelName: "미코"},
		},
	}

	deps := &handlercore.Dependencies{
		CollabMembers: collabMembersStub{},
		Holodex:       holodex,
		Matcher:       matcher.NewMatcher(memberProvider, slog.New(slog.DiscardHandler)),
		Formatter:     formatter.NewResponseFormatter("!", setupScheduleTestRenderer(t)),
		SendMessage: func(_ context.Context, _, message string) error {
			sentMessage = message
			return nil
		},
		SendError: func(_ context.Context, _, _ string) error { return nil },
		Logger:    slog.New(slog.DiscardHandler),
	}

	cmd := NewScheduleCommand(deps)

	err := cmd.Execute(t.Context(), &botcommand.CommandContext{Room: testRoomID}, botcommand.ScheduleParams{Member: "미코"})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if sentMessage == "" {
		t.Fatal("expected non-empty schedule message")
	}
}

func TestScheduleCommand_Execute_NoMemberName(t *testing.T) {
	var sentError string

	deps := &handlercore.Dependencies{
		CollabMembers: collabMembersStub{},
		Holodex:       &scheduleStreamProviderStub{},
		Matcher:       &matcher.Matcher{},
		Formatter:     formatter.NewResponseFormatter("!", nil),
		SendMessage: func(_ context.Context, _, _ string) error {
			return nil
		},
		SendError: func(_ context.Context, _, message string) error {
			sentError = message
			return nil
		},
		Logger: slog.New(slog.DiscardHandler),
	}

	cmd := NewScheduleCommand(deps)

	err := cmd.Execute(t.Context(), &botcommand.CommandContext{Room: testRoomID}, botcommand.ScheduleParams{})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if sentError != messaging.ErrScheduleNeedMemberName {
		t.Fatalf("sent error %q, want %q", sentError, messaging.ErrScheduleNeedMemberName)
	}
}

func TestScheduleCommand_Execute_NoMemberName_SuppressedByMemberToken(t *testing.T) {
	sendErrorCalled := false

	deps := &handlercore.Dependencies{
		CollabMembers: collabMembersStub{},
		Holodex:       &scheduleStreamProviderStub{},
		Matcher:       &matcher.Matcher{},
		Formatter:     formatter.NewResponseFormatter("!", nil),
		SendMessage: func(_ context.Context, _, _ string) error {
			return nil
		},
		SendError: func(_ context.Context, _, _ string) error {
			sendErrorCalled = true
			return nil
		},
		Logger: slog.New(slog.DiscardHandler),
	}

	cmd := NewScheduleCommand(deps)

	err := cmd.Execute(t.Context(), &botcommand.CommandContext{Room: testRoomID}, botcommand.ScheduleParams{RawCommand: "멤버"})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if sendErrorCalled {
		t.Fatal("expected error to be suppressed for '멤버' raw command token")
	}
}

func TestScheduleCommand_Execute_QueryError(t *testing.T) {
	var sentError string

	memberProvider := newContextAwareMemberProvider([]*domain.Member{{
		ChannelID: testChannelMiko,
		Name:      "미코",
	}})

	holodex := &scheduleStreamProviderStub{
		scheduleErr: errors.New("holodex down"),
	}

	deps := &handlercore.Dependencies{
		CollabMembers: collabMembersStub{},
		Holodex:       holodex,
		Matcher:       matcher.NewMatcher(memberProvider, slog.New(slog.DiscardHandler)),
		Formatter:     formatter.NewResponseFormatter("!", nil),
		SendMessage: func(_ context.Context, _, _ string) error {
			return nil
		},
		SendError: func(_ context.Context, _, message string) error {
			sentError = message
			return nil
		},
		Logger: slog.New(slog.DiscardHandler),
	}

	cmd := NewScheduleCommand(deps)

	err := cmd.Execute(t.Context(), &botcommand.CommandContext{Room: testRoomID}, botcommand.ScheduleParams{Member: "미코"})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if sentError != messaging.ErrScheduleQueryFailed {
		t.Fatalf("sent error %q, want %q", sentError, messaging.ErrScheduleQueryFailed)
	}
}

func TestScheduleCommand_Execute_MemberNotFound(t *testing.T) {
	sendMessageCalled := false

	memberProvider := newContextAwareMemberProvider(nil)

	deps := &handlercore.Dependencies{
		CollabMembers: collabMembersStub{},
		Holodex:       &scheduleStreamProviderStub{},
		Matcher:       matcher.NewMatcher(memberProvider, slog.New(slog.DiscardHandler)),
		Formatter:     formatter.NewResponseFormatter("!", nil),
		SendMessage: func(_ context.Context, _, _ string) error {
			sendMessageCalled = true
			return nil
		},
		SendError: func(_ context.Context, _, _ string) error {
			return nil
		},
		Logger: slog.New(slog.DiscardHandler),
	}

	cmd := NewScheduleCommand(deps)
	if err := cmd.Execute(t.Context(), &botcommand.CommandContext{Room: testRoomID}, botcommand.ScheduleParams{Member: "존재하지않는멤버"}); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if !sendMessageCalled {
		t.Fatal("expected SendMessage to be called for unknown member")
	}
}

func TestScheduleCommand_Execute_WithDays(t *testing.T) {
	var sentMessage string

	memberProvider := newContextAwareMemberProvider([]*domain.Member{{
		ChannelID: testChannelMiko,
		Name:      "미코",
	}})

	holodex := &scheduleStreamProviderStub{
		scheduleStreams: []*domain.Stream{
			{ID: "s1", Title: "미코 방송", ChannelID: testChannelMiko, ChannelName: "미코"},
		},
	}

	deps := &handlercore.Dependencies{
		CollabMembers: collabMembersStub{},
		Holodex:       holodex,
		Matcher:       matcher.NewMatcher(memberProvider, slog.New(slog.DiscardHandler)),
		Formatter:     formatter.NewResponseFormatter("!", setupScheduleTestRenderer(t)),
		SendMessage: func(_ context.Context, _, message string) error {
			sentMessage = message
			return nil
		},
		SendError: func(_ context.Context, _, _ string) error { return nil },
		Logger:    slog.New(slog.DiscardHandler),
	}

	cmd := NewScheduleCommand(deps)

	err := cmd.Execute(t.Context(), &botcommand.CommandContext{Room: testRoomID}, botcommand.ScheduleParams{Member: "미코", Days: 14})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if sentMessage == "" {
		t.Fatal("expected non-empty schedule message with days param")
	}
}

func TestScheduleCommand_CheckDependencies_NilDeps(t *testing.T) {
	if err := NewScheduleCommand(nil).CheckDependencies(); err == nil {
		t.Fatal("expected error for nil deps")
	}
}

func TestClampScheduleDays(t *testing.T) {
	tests := []struct {
		name string
		days int
		want int
	}{
		{name: "zero uses default", days: 0, want: 7},
		{name: "negative uses default", days: -3, want: 7},
		{name: "normal value", days: 14, want: 14},
		{name: "max boundary", days: 30, want: 30},
		{name: "over max clamped", days: 60, want: 30},
		{name: "min boundary", days: 1, want: 1},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := clampScheduleDays(tc.days)
			if got != tc.want {
				t.Fatalf("clampScheduleDays(%d) = %d, want %d", tc.days, got, tc.want)
			}
		})
	}
}
