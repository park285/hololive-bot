package handlers

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/kapu/hololive-shared/pkg/domain"
	serviceTemplate "github.com/kapu/hololive-shared/pkg/service/template"
	"gorm.io/gorm"

	"github.com/kapu/hololive-kakao-bot-go/internal/adapter"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/matcher"
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
	return nil, nil
}

func setupUpcomingTestRenderer(t *testing.T) *serviceTemplate.Renderer {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}

	if err := db.AutoMigrate(&domain.NotificationTemplate{}); err != nil {
		t.Fatalf("migrate template table: %v", err)
	}

	if err := db.Create([]domain.NotificationTemplate{
		{
			TemplateKey: domain.TemplateKeyCmdUpcomingStreams,
			Body:        "예정 목록 ({{.Hours}}시간)\n{{range .Streams}}{{.ChannelName}}|{{.Title}}\n{{end}}",
		},
	}).Error; err != nil {
		t.Fatalf("seed upcoming template: %v", err)
	}

	return serviceTemplate.NewRenderer(db, slog.New(slog.DiscardHandler))
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
			{ID: "s2", Title: "테스트 방송 2", ChannelName: "페코라"},
		},
	}

	deps := &Dependencies{
		Holodex:   holodex,
		Formatter: adapter.NewResponseFormatter("!", setupUpcomingTestRenderer(t)),
		SendMessage: func(_ context.Context, _, message string) error {
			sentMessage = message
			return nil
		},
		SendError: func(_ context.Context, _, _ string) error { return nil },
		Logger:    slog.New(slog.DiscardHandler),
	}

	cmd := NewUpcomingCommand(deps)
	err := cmd.Execute(t.Context(), &domain.CommandContext{Room: "room-1"}, map[string]any{})
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

	deps := &Dependencies{
		Holodex:   holodex,
		Formatter: adapter.NewResponseFormatter("!", setupUpcomingTestRenderer(t)),
		SendMessage: func(_ context.Context, _, message string) error {
			sentMessage = message
			return nil
		},
		SendError: func(_ context.Context, _, _ string) error { return nil },
		Logger:    slog.New(slog.DiscardHandler),
	}

	cmd := NewUpcomingCommand(deps)
	err := cmd.Execute(t.Context(), &domain.CommandContext{Room: "room-1"}, map[string]any{
		"limit": 5,
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

	deps := &Dependencies{
		Holodex:   holodex,
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

	cmd := NewUpcomingCommand(deps)
	err := cmd.Execute(t.Context(), &domain.CommandContext{Room: "room-1"}, map[string]any{})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if sentError != adapter.ErrUpcomingStreamQueryFailed {
		t.Fatalf("sent error %q, want %q", sentError, adapter.ErrUpcomingStreamQueryFailed)
	}
}

func TestUpcomingCommand_Execute_MemberUpcoming_GoldenPath(t *testing.T) {
	var sentMessage string

	memberProvider := newContextAwareMemberProvider([]*domain.Member{{
		ChannelID: "ch-miko",
		Name:      "미코",
	}})

	holodex := &upcomingStreamProviderStub{
		upcomingStreams: []*domain.Stream{
			{ID: "s1", Title: "미코 방송", ChannelID: "ch-miko", ChannelName: "미코"},
			{ID: "s2", Title: "페코라 방송", ChannelID: "ch-peko", ChannelName: "페코라"},
		},
	}

	deps := &Dependencies{
		Holodex:   holodex,
		Matcher:   matcher.NewMatcher(nilBaseContext(), memberProvider, nil, nil, nil, slog.New(slog.DiscardHandler)),
		Formatter: adapter.NewResponseFormatter("!", setupUpcomingTestRenderer(t)),
		SendMessage: func(_ context.Context, _, message string) error {
			sentMessage = message
			return nil
		},
		SendError: func(_ context.Context, _, _ string) error { return nil },
		Logger:    slog.New(slog.DiscardHandler),
	}

	cmd := NewUpcomingCommand(deps)
	err := cmd.Execute(t.Context(), &domain.CommandContext{Room: "room-1"}, map[string]any{
		"member": "미코",
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
		ChannelID: "ch-miko",
		Name:      "미코",
	}})

	holodex := &upcomingStreamProviderStub{
		upcomingStreams: []*domain.Stream{
			{ID: "s1", Title: "페코라 방송", ChannelID: "ch-peko", ChannelName: "페코라"},
		},
	}

	deps := &Dependencies{
		Holodex:   holodex,
		Matcher:   matcher.NewMatcher(nilBaseContext(), memberProvider, nil, nil, nil, slog.New(slog.DiscardHandler)),
		Formatter: adapter.NewResponseFormatter("!", nil),
		SendMessage: func(_ context.Context, _, message string) error {
			sentMessage = message
			return nil
		},
		SendError: func(_ context.Context, _, _ string) error { return nil },
		Logger:    slog.New(slog.DiscardHandler),
	}

	cmd := NewUpcomingCommand(deps)
	err := cmd.Execute(t.Context(), &domain.CommandContext{Room: "room-1"}, map[string]any{
		"member": "미코",
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
		ChannelID: "ch-miko",
		Name:      "미코",
	}})

	holodex := &upcomingStreamProviderStub{
		upcomingErr: errors.New("api error"),
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

	cmd := NewUpcomingCommand(deps)
	err := cmd.Execute(t.Context(), &domain.CommandContext{Room: "room-1"}, map[string]any{
		"member": "미코",
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if sentError != adapter.ErrUpcomingStreamQueryFailed {
		t.Fatalf("sent error %q, want %q", sentError, adapter.ErrUpcomingStreamQueryFailed)
	}
}

func TestUpcomingCommand_Execute_MemberNotFound(t *testing.T) {
	sendErrorCalled := false

	memberProvider := newContextAwareMemberProvider(nil)

	deps := &Dependencies{
		Holodex:   &upcomingStreamProviderStub{},
		Matcher:   matcher.NewMatcher(nilBaseContext(), memberProvider, nil, nil, nil, slog.New(slog.DiscardHandler)),
		Formatter: adapter.NewResponseFormatter("!", nil),
		SendMessage: func(_ context.Context, _, _ string) error {
			return nil
		},
		SendError: func(_ context.Context, _, _ string) error {
			sendErrorCalled = true
			return errors.New("member not found")
		},
		Logger: slog.New(slog.DiscardHandler),
	}

	cmd := NewUpcomingCommand(deps)
	_ = cmd.Execute(t.Context(), &domain.CommandContext{Room: "room-1"}, map[string]any{
		"member": "존재하지않는멤버",
	})

	if !sendErrorCalled {
		t.Fatal("expected SendError to be called for unknown member")
	}
}

func TestUpcomingCommand_Execute_NilDeps(t *testing.T) {
	cmd := NewUpcomingCommand(nil)

	err := cmd.Execute(t.Context(), &domain.CommandContext{Room: "room-1"}, nil)
	if err == nil {
		t.Fatal("expected error for nil deps")
	}
}
