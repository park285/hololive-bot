package handlers

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging/formatter"
	handlercore "github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers/handlercore"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/service/matcher"
	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
	serviceTemplate "github.com/kapu/hololive-shared/pkg/service/template"
)

type upcomingStreamProviderStub struct {
	upcomingStreams []*domain.Stream
	upcomingErr     error
}

func (s *upcomingStreamProviderStub) GetLiveStreams(_ context.Context) ([]*domain.Stream, error) {
	return nil, nil
}

func (s *upcomingStreamProviderStub) GetUpcomingStreams(_ context.Context, _ int) ([]*domain.Stream, error) {
	return s.upcomingStreams, s.upcomingErr
}

func (s *upcomingStreamProviderStub) GetChannelSchedule(_ context.Context, _ string, _ int, _ bool) ([]*domain.Stream, error) {
	return nil, nil
}

func (s *upcomingStreamProviderStub) GetChannel(_ context.Context, _ string) (*domain.Channel, error) {
	return nil, errTestStubNoChannel
}

func setupUpcomingTestRenderer(t *testing.T) *serviceTemplate.Renderer {
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
	`, domain.TemplateKeyCmdUpcomingStreams, "예정 목록 ({{.Hours}}시간)\n{{range .Streams}}{{.ChannelName}}|{{.Title}}\n{{end}}"); err != nil {
		t.Fatalf("seed upcoming template: %v", err)
	}

	return serviceTemplate.NewRenderer(pool, slog.New(slog.DiscardHandler))
}

func TestUpcomingCommand_Name(t *testing.T) {
	cmd := NewUpcomingCommand(nil)
	if cmd.Name() != "upcoming" {
		t.Fatalf("Name() = %q, want %q", cmd.Name(), "upcoming")
	}
}

func TestUpcomingCommand_Description(t *testing.T) {
	cmd := NewUpcomingCommand(nil)
	if cmd.Description() == "" {
		t.Fatal("Description() should not be empty")
	}
}

func TestUpcomingCommand_Execute_AllUpcoming_GoldenPath(t *testing.T) {
	var sentMessage string

	holodex := &upcomingStreamProviderStub{
		upcomingStreams: []*domain.Stream{
			{ID: "s1", Title: "테스트 방송 1", ChannelName: "미코"},
			{ID: "s2", Title: "테스트 방송 2", ChannelName: testMemberPekora},
		},
	}

	deps := &handlercore.Dependencies{
		Holodex:   holodex,
		Formatter: formatter.NewResponseFormatter("!", setupUpcomingTestRenderer(t)),
		SendMessage: func(_ context.Context, _, message string) error {
			sentMessage = message
			return nil
		},
		SendError: func(_ context.Context, _, _ string) error { return nil },
		Logger:    slog.New(slog.DiscardHandler),
	}

	cmd := NewUpcomingCommand(deps)

	err := cmd.Execute(t.Context(), &domain.CommandContext{Room: testRoomID}, map[string]any{})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if sentMessage == "" {
		t.Fatal("expected non-empty upcoming message")
	}
}

func TestUpcomingCommand_Execute_AllUpcoming_WithOverflow(t *testing.T) {
	streams := make([]*domain.Stream, 15)
	for i := range streams {
		streams[i] = &domain.Stream{ID: "s", Title: "방송", ChannelName: "미코"}
	}

	var sentMessage string

	holodex := &upcomingStreamProviderStub{upcomingStreams: streams}

	deps := &handlercore.Dependencies{
		Holodex:   holodex,
		Formatter: formatter.NewResponseFormatter("!", setupUpcomingTestRenderer(t)),
		SendMessage: func(_ context.Context, _, message string) error {
			sentMessage = message
			return nil
		},
		SendError: func(_ context.Context, _, _ string) error { return nil },
		Logger:    slog.New(slog.DiscardHandler),
	}

	cmd := NewUpcomingCommand(deps)

	err := cmd.Execute(t.Context(), &domain.CommandContext{Room: testRoomID}, map[string]any{
		testParamLimit: 5,
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if sentMessage == "" {
		t.Fatal("expected message with overflow")
	}
}

func TestUpcomingCommand_Execute_AllUpcoming_QueryError(t *testing.T) {
	var sentError string

	holodex := &upcomingStreamProviderStub{
		upcomingErr: errors.New("holodex api down"),
	}

	deps := &handlercore.Dependencies{
		Holodex:   holodex,
		Formatter: formatter.NewResponseFormatter("!", nil),
		SendMessage: func(_ context.Context, _, _ string) error {
			return nil
		},
		SendError: func(_ context.Context, _, message string) error {
			sentError = message
			return nil
		},
		Logger: slog.New(slog.DiscardHandler),
	}

	cmd := NewUpcomingCommand(deps)

	err := cmd.Execute(t.Context(), &domain.CommandContext{Room: testRoomID}, map[string]any{})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if sentError != messaging.ErrUpcomingStreamQueryFailed {
		t.Fatalf("sent error %q, want %q", sentError, messaging.ErrUpcomingStreamQueryFailed)
	}
}

func TestUpcomingCommand_Execute_MemberUpcoming_GoldenPath(t *testing.T) {
	var sentMessage string

	memberProvider := newContextAwareMemberProvider([]*domain.Member{{
		ChannelID: testChannelMiko,
		Name:      "미코",
	}})

	holodex := &upcomingStreamProviderStub{
		upcomingStreams: []*domain.Stream{
			{ID: "s1", Title: "미코 방송", ChannelID: testChannelMiko, ChannelName: "미코"},
			{ID: "s2", Title: "페코라 방송", ChannelID: "ch-peko", ChannelName: testMemberPekora},
		},
	}

	deps := &handlercore.Dependencies{
		Holodex:   holodex,
		Matcher:   matcher.NewMatcher(nilBaseContext(), memberProvider, nil, nil, nil, slog.New(slog.DiscardHandler)),
		Formatter: formatter.NewResponseFormatter("!", setupUpcomingTestRenderer(t)),
		SendMessage: func(_ context.Context, _, message string) error {
			sentMessage = message
			return nil
		},
		SendError: func(_ context.Context, _, _ string) error { return nil },
		Logger:    slog.New(slog.DiscardHandler),
	}

	cmd := NewUpcomingCommand(deps)

	err := cmd.Execute(t.Context(), &domain.CommandContext{Room: testRoomID}, map[string]any{
		paramMember: "미코",
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if sentMessage == "" {
		t.Fatal("expected non-empty member upcoming message")
	}
}

func TestUpcomingCommand_Execute_MemberUpcoming_NoStreams(t *testing.T) {
	var sentMessage string

	memberProvider := newContextAwareMemberProvider([]*domain.Member{{
		ChannelID: testChannelMiko,
		Name:      "미코",
	}})

	holodex := &upcomingStreamProviderStub{
		upcomingStreams: []*domain.Stream{
			{ID: "s1", Title: "페코라 방송", ChannelID: "ch-peko", ChannelName: testMemberPekora},
		},
	}

	deps := &handlercore.Dependencies{
		Holodex:   holodex,
		Matcher:   matcher.NewMatcher(nilBaseContext(), memberProvider, nil, nil, nil, slog.New(slog.DiscardHandler)),
		Formatter: formatter.NewResponseFormatter("!", nil),
		SendMessage: func(_ context.Context, _, message string) error {
			sentMessage = message
			return nil
		},
		SendError: func(_ context.Context, _, _ string) error { return nil },
		Logger:    slog.New(slog.DiscardHandler),
	}

	cmd := NewUpcomingCommand(deps)

	err := cmd.Execute(t.Context(), &domain.CommandContext{Room: testRoomID}, map[string]any{
		paramMember: "미코",
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if sentMessage == "" {
		t.Fatal("expected no-upcoming message for member")
	}
}

func TestUpcomingCommand_Execute_MemberUpcoming_QueryError(t *testing.T) {
	var sentError string

	memberProvider := newContextAwareMemberProvider([]*domain.Member{{
		ChannelID: testChannelMiko,
		Name:      "미코",
	}})

	holodex := &upcomingStreamProviderStub{
		upcomingErr: errors.New("api error"),
	}

	deps := &handlercore.Dependencies{
		Holodex:   holodex,
		Matcher:   matcher.NewMatcher(nilBaseContext(), memberProvider, nil, nil, nil, slog.New(slog.DiscardHandler)),
		Formatter: formatter.NewResponseFormatter("!", nil),
		SendMessage: func(_ context.Context, _, _ string) error {
			return nil
		},
		SendError: func(_ context.Context, _, message string) error {
			sentError = message
			return nil
		},
		Logger: slog.New(slog.DiscardHandler),
	}

	cmd := NewUpcomingCommand(deps)

	err := cmd.Execute(t.Context(), &domain.CommandContext{Room: testRoomID}, map[string]any{
		paramMember: "미코",
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if sentError != messaging.ErrUpcomingStreamQueryFailed {
		t.Fatalf("sent error %q, want %q", sentError, messaging.ErrUpcomingStreamQueryFailed)
	}
}

func TestUpcomingCommand_Execute_MemberNotFound(t *testing.T) {
	sendMessageCalled := false

	memberProvider := newContextAwareMemberProvider(nil)

	deps := &handlercore.Dependencies{
		Holodex:   &upcomingStreamProviderStub{},
		Matcher:   matcher.NewMatcher(nilBaseContext(), memberProvider, nil, nil, nil, slog.New(slog.DiscardHandler)),
		Formatter: formatter.NewResponseFormatter("!", nil),
		SendMessage: func(_ context.Context, _, _ string) error {
			sendMessageCalled = true
			return nil
		},
		SendError: func(_ context.Context, _, _ string) error {
			return nil
		},
		Logger: slog.New(slog.DiscardHandler),
	}

	cmd := NewUpcomingCommand(deps)
	if err := cmd.Execute(t.Context(), &domain.CommandContext{Room: testRoomID}, map[string]any{
		paramMember: "존재하지않는멤버",
	}); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if !sendMessageCalled {
		t.Fatal("expected SendMessage to be called for unknown member")
	}
}

func TestUpcomingCommand_Execute_NilDeps(t *testing.T) {
	cmd := NewUpcomingCommand(nil)

	err := cmd.Execute(t.Context(), &domain.CommandContext{Room: testRoomID}, nil)
	if err == nil {
		t.Fatal("expected error for nil deps")
	}
}
