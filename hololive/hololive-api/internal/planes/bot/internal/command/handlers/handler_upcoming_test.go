package handlers

import (
	"context"
	"errors"
	"log/slog"
	"strings"
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

func newUpcomingTestMatcher(members []*domain.Member) *matcher.Matcher {
	return matcher.NewMatcher(newContextAwareMemberProvider(members), nil, slog.New(slog.DiscardHandler))
}

func TestUpcomingCommand_Execute_AllUpcoming_GoldenPath(t *testing.T) {
	var sentMessage string

	sora := &domain.Stream{ID: "s1", Title: "테스트 방송 1", ChannelID: testChannelSora, ChannelName: "Sora Ch. ときのそら"}
	holodex := &upcomingStreamProviderStub{
		upcomingStreams: []*domain.Stream{
			sora,
			{ID: "s2", Title: "테스트 방송 2", ChannelID: "ch-unregistered", ChannelName: "Guest Ch."},
		},
	}

	deps := &handlercore.Dependencies{
		Holodex: holodex,
		Matcher: newUpcomingTestMatcher([]*domain.Member{
			{ChannelID: testChannelSora, Name: "Tokino Sora", NameKo: "토키노 소라", ShortKoreanName: "소라"},
		}),
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

	// members 등록 채널은 정본 표시명, 미등록 채널은 응답 이름을 쓰며 공유될 수 있는 원천 스트림은 바꾸지 않는다.
	for _, want := range []string{"소라|테스트 방송 1", "Guest Ch.|테스트 방송 2"} {
		if !strings.Contains(sentMessage, want) {
			t.Fatalf("upcoming message = %q, want %q", sentMessage, want)
		}
	}

	if sora.ChannelName != "Sora Ch. ときのそら" {
		t.Fatalf("source stream channel name mutated: %q", sora.ChannelName)
	}
}

func TestUpcomingCommand_Execute_DisplayNameLookupError(t *testing.T) {
	var sentError string

	deps := &handlercore.Dependencies{
		Holodex:   &upcomingStreamProviderStub{upcomingStreams: []*domain.Stream{{ID: "s1", ChannelID: testChannelSora, ChannelName: "Sora Ch."}}},
		Matcher:   matcher.NewMatcher(&failedMemberDataProvider{err: errors.New("member repository unavailable")}, nil, slog.New(slog.DiscardHandler)),
		Formatter: formatter.NewResponseFormatter("!", nil),
		SendMessage: func(_ context.Context, _, _ string) error {
			t.Fatal("upcoming list must not be sent with source names after display name lookup failure")

			return nil
		},
		SendError: func(_ context.Context, _, message string) error {
			sentError = message
			return nil
		},
		Logger: slog.New(slog.DiscardHandler),
	}

	if err := NewUpcomingCommand(deps).Execute(t.Context(), &domain.CommandContext{Room: testRoomID}, map[string]any{}); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if sentError != messaging.ErrUpcomingStreamQueryFailed {
		t.Fatalf("sent error %q, want %q", sentError, messaging.ErrUpcomingStreamQueryFailed)
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
		Matcher:   newUpcomingTestMatcher(nil),
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
		Matcher:   newUpcomingTestMatcher(nil),
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
		Matcher:   matcher.NewMatcher(memberProvider, nil, slog.New(slog.DiscardHandler)),
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
		Matcher:   matcher.NewMatcher(memberProvider, nil, slog.New(slog.DiscardHandler)),
		Formatter: newSeededTestFormatter(t),
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
		Matcher:   matcher.NewMatcher(memberProvider, nil, slog.New(slog.DiscardHandler)),
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
		Matcher:   matcher.NewMatcher(memberProvider, nil, slog.New(slog.DiscardHandler)),
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
