package handlers

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/kapu/hololive-shared/pkg/dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
	serviceTemplate "github.com/kapu/hololive-shared/pkg/service/template"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/service/matcher"
)

type scheduleStreamProviderStub struct {
	scheduleStreams []*domain.Stream
	scheduleErr     error
}

func (s *scheduleStreamProviderStub) GetLiveStreams(_ context.Context) ([]*domain.Stream, error) {
	return nil, nil
}

func (s *scheduleStreamProviderStub) GetUpcomingStreams(_ context.Context, _ int) ([]*domain.Stream, error) {
	return nil, nil
}

func (s *scheduleStreamProviderStub) GetChannelSchedule(_ context.Context, _ string, _ int, _ bool) ([]*domain.Stream, error) {
	return s.scheduleStreams, s.scheduleErr
}

func (s *scheduleStreamProviderStub) GetChannel(_ context.Context, _ string) (*domain.Channel, error) {
	return nil, nil
}

func setupScheduleTestRenderer(t *testing.T) *serviceTemplate.Renderer {
	t.Helper()

	pool := dbtest.NewPool(t)
	if _, err := pool.Exec(t.Context(), `DELETE FROM notification_templates`); err != nil {
		t.Fatalf("clear templates: %v", err)
	}
	if _, err := pool.Exec(t.Context(), `
		INSERT INTO notification_templates(template_key, channel_id, body)
		VALUES ($1, NULL, $2)
		ON CONFLICT (template_key) WHERE channel_id IS NULL
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
		ChannelID: "ch-miko",
		Name:      "미코",
	}})

	holodex := &scheduleStreamProviderStub{
		scheduleStreams: []*domain.Stream{
			{ID: "s1", Title: "미코 일정 방송", ChannelID: "ch-miko", ChannelName: "미코"},
		},
	}

	deps := &Dependencies{
		Holodex:   holodex,
		Matcher:   matcher.NewMatcher(nilBaseContext(), memberProvider, nil, nil, nil, slog.New(slog.DiscardHandler)),
		Formatter: adapter.NewResponseFormatter("!", setupScheduleTestRenderer(t)),
		SendMessage: func(_ context.Context, _, message string) error {
			sentMessage = message
			return nil
		},
		SendError: func(_ context.Context, _, _ string) error { return nil },
		Logger:    slog.New(slog.DiscardHandler),
	}

	cmd := NewScheduleCommand(deps)
	err := cmd.Execute(t.Context(), &domain.CommandContext{Room: "room-1"}, map[string]any{
		"member": "미코",
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if sentMessage == "" {
		t.Fatal("expected non-empty schedule message")
	}
}

func TestScheduleCommand_Execute_NoMemberName(t *testing.T) {
	var sentError string

	deps := &Dependencies{
		Holodex:   &scheduleStreamProviderStub{},
		Matcher:   &matcher.Matcher{},
		Formatter: adapter.NewResponseFormatter("!", nil),
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
	err := cmd.Execute(t.Context(), &domain.CommandContext{Room: "room-1"}, map[string]any{})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if sentError != adapter.ErrScheduleNeedMemberName {
		t.Fatalf("sent error %q, want %q", sentError, adapter.ErrScheduleNeedMemberName)
	}
}

func TestScheduleCommand_Execute_NoMemberName_SuppressedByMemberToken(t *testing.T) {
	sendErrorCalled := false

	deps := &Dependencies{
		Holodex:   &scheduleStreamProviderStub{},
		Matcher:   &matcher.Matcher{},
		Formatter: adapter.NewResponseFormatter("!", nil),
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
	err := cmd.Execute(t.Context(), &domain.CommandContext{Room: "room-1"}, map[string]any{
		"_raw_command": "멤버",
	})
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
		ChannelID: "ch-miko",
		Name:      "미코",
	}})

	holodex := &scheduleStreamProviderStub{
		scheduleErr: errors.New("holodex down"),
	}

	deps := &Dependencies{
		Holodex:   holodex,
		Matcher:   matcher.NewMatcher(nilBaseContext(), memberProvider, nil, nil, nil, slog.New(slog.DiscardHandler)),
		Formatter: adapter.NewResponseFormatter("!", nil),
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
	err := cmd.Execute(t.Context(), &domain.CommandContext{Room: "room-1"}, map[string]any{
		"member": "미코",
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if sentError != adapter.ErrScheduleQueryFailed {
		t.Fatalf("sent error %q, want %q", sentError, adapter.ErrScheduleQueryFailed)
	}
}

func TestScheduleCommand_Execute_MemberNotFound(t *testing.T) {
	sendErrorCalled := false

	memberProvider := newContextAwareMemberProvider(nil)

	deps := &Dependencies{
		Holodex:   &scheduleStreamProviderStub{},
		Matcher:   matcher.NewMatcher(nilBaseContext(), memberProvider, nil, nil, nil, slog.New(slog.DiscardHandler)),
		Formatter: adapter.NewResponseFormatter("!", nil),
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
	if err := cmd.Execute(t.Context(), &domain.CommandContext{Room: "room-1"}, map[string]any{
		"member": "존재하지않는멤버",
	}); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if !sendErrorCalled {
		t.Fatal("expected SendError to be called for unknown member")
	}
}

func TestScheduleCommand_Execute_WithDays(t *testing.T) {
	var sentMessage string

	memberProvider := newContextAwareMemberProvider([]*domain.Member{{
		ChannelID: "ch-miko",
		Name:      "미코",
	}})

	holodex := &scheduleStreamProviderStub{
		scheduleStreams: []*domain.Stream{
			{ID: "s1", Title: "미코 방송", ChannelID: "ch-miko", ChannelName: "미코"},
		},
	}

	deps := &Dependencies{
		Holodex:   holodex,
		Matcher:   matcher.NewMatcher(nilBaseContext(), memberProvider, nil, nil, nil, slog.New(slog.DiscardHandler)),
		Formatter: adapter.NewResponseFormatter("!", setupScheduleTestRenderer(t)),
		SendMessage: func(_ context.Context, _, message string) error {
			sentMessage = message
			return nil
		},
		SendError: func(_ context.Context, _, _ string) error { return nil },
		Logger:    slog.New(slog.DiscardHandler),
	}

	cmd := NewScheduleCommand(deps)
	err := cmd.Execute(t.Context(), &domain.CommandContext{Room: "room-1"}, map[string]any{
		"member": "미코",
		"days":   14,
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if sentMessage == "" {
		t.Fatal("expected non-empty schedule message with days param")
	}
}

func TestScheduleCommand_Execute_NilDeps(t *testing.T) {
	cmd := NewScheduleCommand(nil)

	err := cmd.Execute(t.Context(), &domain.CommandContext{Room: "room-1"}, nil)
	if err == nil {
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

func TestRawScheduleDays(t *testing.T) {
	tests := []struct {
		name   string
		params map[string]any
		want   int
	}{
		{name: "missing key", params: map[string]any{}, want: 7},
		{name: "int value", params: map[string]any{"days": 14}, want: 14},
		{name: "float64 value", params: map[string]any{"days": 10.5}, want: 10},
		{name: "unsupported type", params: map[string]any{"days": "14"}, want: 7},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := rawScheduleDays(tc.params)
			if got != tc.want {
				t.Fatalf("rawScheduleDays() = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestScheduleMemberName(t *testing.T) {
	tests := []struct {
		name     string
		params   map[string]any
		wantName string
		wantOK   bool
	}{
		{name: "present", params: map[string]any{"member": "미코"}, wantName: "미코", wantOK: true},
		{name: "empty string", params: map[string]any{"member": ""}, wantName: "", wantOK: false},
		{name: "missing", params: map[string]any{}, wantName: "", wantOK: false},
		{name: "wrong type", params: map[string]any{"member": 123}, wantName: "", wantOK: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			name, ok := scheduleMemberName(tc.params)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if name != tc.wantName {
				t.Fatalf("name = %q, want %q", name, tc.wantName)
			}
		})
	}
}
