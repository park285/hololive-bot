package handlers

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/park285/iris-client-go/iris"

	"github.com/kapu/hololive-kakao-bot-go/internal/adapter/messaging/formatter"
)

type calendarRepoStub struct {
	entries []domain.CalendarEntry
	err     error
}

func (s *calendarRepoStub) FindMembersWithCelebrationsInMonth(_ context.Context, _, _ int) ([]domain.CalendarEntry, error) {
	return s.entries, s.err
}

type calendarImageRendererStub struct {
	data []byte
	err  error
}

func (s *calendarImageRendererStub) RenderCalendarImage(_, _ int, _ []domain.CalendarEntry) ([]byte, error) {
	return s.data, s.err
}

func TestCalendarCommand_Name(t *testing.T) {
	cmd := &CalendarCommand{}
	if cmd.Name() != "calendar" {
		t.Errorf("Name() = %q, want %q", cmd.Name(), "calendar")
	}
}

func TestCalendarCommand_Description(t *testing.T) {
	cmd := &CalendarCommand{}
	if cmd.Description() == "" {
		t.Error("Description() should not be empty")
	}
}

func TestCalendarCommand_Execute_TextFallback(t *testing.T) {
	var sentMessage string
	deps := &Dependencies{
		Formatter: formatter.NewResponseFormatter("!", nil),
		SendMessage: func(_ context.Context, _, msg string) error {
			sentMessage = msg
			return nil
		},
		SendError: func(_ context.Context, _, msg string) error { return nil },
		Logger:    slog.Default(),
	}

	repo := &calendarRepoStub{
		entries: []domain.CalendarEntry{
			{Kind: domain.CelebrationKindBirthday, Member: &domain.Member{ShortKoreanName: "페코라"}, Day: 15},
		},
	}

	cmd := NewCalendarCommand(deps, repo, nil)
	cmdCtx := &domain.CommandContext{Room: "test-room"}

	err := cmd.Execute(context.Background(), cmdCtx, map[string]any{"month": 6})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if sentMessage == "" {
		t.Error("expected message to be sent")
	}
}

func TestCalendarCommand_Execute_ImageSuccess(t *testing.T) {
	var sentImage []byte
	deps := &Dependencies{
		Formatter:   formatter.NewResponseFormatter("!", nil),
		SendMessage: func(_ context.Context, _, _ string) error { return nil },
		SendImage: func(_ context.Context, _ string, data []byte, _ ...iris.SendOption) error {
			sentImage = data
			return nil
		},
		SendError: func(_ context.Context, _, _ string) error { return nil },
		Logger:    slog.Default(),
	}

	repo := &calendarRepoStub{
		entries: []domain.CalendarEntry{
			{Kind: domain.CelebrationKindBirthday, Member: &domain.Member{ShortKoreanName: "페코라"}, Day: 15},
		},
	}
	renderer := &calendarImageRendererStub{data: []byte("png-data")}

	cmd := NewCalendarCommand(deps, repo, renderer)
	cmdCtx := &domain.CommandContext{Room: "test-room"}

	err := cmd.Execute(context.Background(), cmdCtx, map[string]any{"month": 6})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if sentImage == nil {
		t.Error("expected image to be sent")
	}
}

func TestCalendarCommand_Execute_ImageFailureFallsBackToText(t *testing.T) {
	var sentMessage string
	deps := &Dependencies{
		Formatter: formatter.NewResponseFormatter("!", nil),
		SendMessage: func(_ context.Context, _, msg string) error {
			sentMessage = msg
			return nil
		},
		SendImage: func(_ context.Context, _ string, _ []byte, _ ...iris.SendOption) error {
			t.Error("SendImage should not be called on render failure")
			return nil
		},
		SendError: func(_ context.Context, _, _ string) error { return nil },
		Logger:    slog.Default(),
	}

	repo := &calendarRepoStub{
		entries: []domain.CalendarEntry{
			{Kind: domain.CelebrationKindBirthday, Member: &domain.Member{ShortKoreanName: "미코"}, Day: 5},
		},
	}
	renderer := &calendarImageRendererStub{err: errors.New("font load failed")}

	cmd := NewCalendarCommand(deps, repo, renderer)
	cmdCtx := &domain.CommandContext{Room: "test-room"}

	err := cmd.Execute(context.Background(), cmdCtx, map[string]any{"month": 3})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if sentMessage == "" {
		t.Error("expected text fallback message to be sent")
	}
}

func TestCalendarCommand_Execute_RepoError(t *testing.T) {
	var sentError string
	deps := &Dependencies{
		Formatter:   formatter.NewResponseFormatter("!", nil),
		SendMessage: func(_ context.Context, _, _ string) error { return nil },
		SendError: func(_ context.Context, _, msg string) error {
			sentError = msg
			return nil
		},
		Logger: slog.Default(),
	}

	repo := &calendarRepoStub{err: errors.New("db connection lost")}

	cmd := NewCalendarCommand(deps, repo, nil)
	cmdCtx := &domain.CommandContext{Room: "test-room"}

	err := cmd.Execute(context.Background(), cmdCtx, nil)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if sentError == "" {
		t.Error("expected error message to be sent")
	}
}

func TestCalendarCommand_EnsureDeps_NilRepo(t *testing.T) {
	deps := &Dependencies{
		Formatter:   formatter.NewResponseFormatter("!", nil),
		SendMessage: func(_ context.Context, _, _ string) error { return nil },
		SendError:   func(_ context.Context, _, _ string) error { return nil },
		Logger:      slog.Default(),
	}

	cmd := NewCalendarCommand(deps, nil, nil)
	cmdCtx := &domain.CommandContext{Room: "test-room"}

	err := cmd.Execute(context.Background(), cmdCtx, nil)
	if err == nil {
		t.Error("expected error for nil repository")
	}
}
