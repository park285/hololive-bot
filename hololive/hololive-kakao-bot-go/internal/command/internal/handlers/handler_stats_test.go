package handlers

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/stats"

	"github.com/kapu/hololive-kakao-bot-go/internal/adapter"
)

type statsRepoStub struct {
	gainers  []domain.RankEntry
	gainErr  error
	graphErr error
}

func (s *statsRepoStub) GetTopGainers(_ context.Context, _ time.Time, _ int) ([]domain.RankEntry, error) {
	return s.gainers, s.gainErr
}

func (s *statsRepoStub) GetSubscriberGraph(_ context.Context, _ string, _ int) (*stats.SubscriberGraphData, error) {
	return nil, s.graphErr
}

func TestStatsCommand_Name(t *testing.T) {
	cmd := NewStatsCommand(nil)
	if cmd.Name() != "stats" {
		t.Fatalf("Name() = %q, want %q", cmd.Name(), "stats")
	}
}

func TestStatsCommand_Description(t *testing.T) {
	cmd := NewStatsCommand(nil)
	if cmd.Description() == "" {
		t.Fatal("Description() should not be empty")
	}
}

func TestStatsCommand_Execute_TopGainers_GoldenPath(t *testing.T) {
	var sentMessage string

	repo := &statsRepoStub{
		gainers: []domain.RankEntry{
			{ChannelID: "ch-1", MemberName: "미코", Value: 5000, Rank: 1},
			{ChannelID: "ch-2", MemberName: "페코라", Value: 3000, Rank: 2},
		},
	}

	deps := &Dependencies{
		StatsRepository: repo,
		Formatter:       adapter.NewResponseFormatter("!", nil),
		SendMessage: func(_ context.Context, _, message string) error {
			sentMessage = message
			return nil
		},
		SendError: func(_ context.Context, _, _ string) error { return nil },
		Logger:    slog.New(slog.DiscardHandler),
	}

	cmd := NewStatsCommand(deps)
	err := cmd.Execute(t.Context(), &domain.CommandContext{Room: "room-1"}, map[string]any{
		"action": "gainers",
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if sentMessage == "" {
		t.Fatal("expected non-empty stats message")
	}
}

func TestStatsCommand_Execute_DefaultActionIsGainers(t *testing.T) {
	var sentMessage string

	repo := &statsRepoStub{
		gainers: []domain.RankEntry{
			{ChannelID: "ch-1", MemberName: "미코", Value: 1000, Rank: 1},
		},
	}

	deps := &Dependencies{
		StatsRepository: repo,
		Formatter:       adapter.NewResponseFormatter("!", nil),
		SendMessage: func(_ context.Context, _, message string) error {
			sentMessage = message
			return nil
		},
		SendError: func(_ context.Context, _, _ string) error { return nil },
		Logger:    slog.New(slog.DiscardHandler),
	}

	cmd := NewStatsCommand(deps)
	err := cmd.Execute(t.Context(), &domain.CommandContext{Room: "room-1"}, map[string]any{})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if sentMessage == "" {
		t.Fatal("expected gainers message with empty action param")
	}
}

func TestStatsCommand_Execute_UnknownAction(t *testing.T) {
	var sentError string

	deps := &Dependencies{
		StatsRepository: &statsRepoStub{},
		Formatter:       adapter.NewResponseFormatter("!", nil),
		SendMessage: func(_ context.Context, _, _ string) error {
			return nil
		},
		SendError: func(_ context.Context, _, message string) error {
			sentError = message
			return nil
		},
		Logger: slog.New(slog.DiscardHandler),
	}

	cmd := NewStatsCommand(deps)
	err := cmd.Execute(t.Context(), &domain.CommandContext{Room: "room-1"}, map[string]any{
		"action": "invalid_action",
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if sentError != adapter.ErrUnknownStatsPeriod {
		t.Fatalf("sent error %q, want %q", sentError, adapter.ErrUnknownStatsPeriod)
	}
}

func TestStatsCommand_Execute_RepoError(t *testing.T) {
	var sentError string

	repo := &statsRepoStub{
		gainErr: errors.New("db connection failed"),
	}

	deps := &Dependencies{
		StatsRepository: repo,
		Formatter:       adapter.NewResponseFormatter("!", nil),
		SendMessage: func(_ context.Context, _, _ string) error {
			return nil
		},
		SendError: func(_ context.Context, _, message string) error {
			sentError = message
			return nil
		},
		Logger: slog.New(slog.DiscardHandler),
	}

	cmd := NewStatsCommand(deps)
	err := cmd.Execute(t.Context(), &domain.CommandContext{Room: "room-1"}, map[string]any{
		"action": "gainers",
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if sentError != adapter.ErrStatsQueryFailed {
		t.Fatalf("sent error %q, want %q", sentError, adapter.ErrStatsQueryFailed)
	}
}

func TestStatsCommand_Execute_NoData(t *testing.T) {
	var sentMessage string

	repo := &statsRepoStub{
		gainers: []domain.RankEntry{},
	}

	deps := &Dependencies{
		StatsRepository: repo,
		Formatter:       adapter.NewResponseFormatter("!", nil),
		SendMessage: func(_ context.Context, _, message string) error {
			sentMessage = message
			return nil
		},
		SendError: func(_ context.Context, _, _ string) error { return nil },
		Logger:    slog.New(slog.DiscardHandler),
	}

	cmd := NewStatsCommand(deps)
	err := cmd.Execute(t.Context(), &domain.CommandContext{Room: "room-1"}, map[string]any{
		"action": "gainers",
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if sentMessage != adapter.MsgNoStatsData {
		t.Fatalf("sent message %q, want %q", sentMessage, adapter.MsgNoStatsData)
	}
}

func TestStatsCommand_Execute_NilDeps(t *testing.T) {
	cmd := NewStatsCommand(nil)

	err := cmd.Execute(t.Context(), &domain.CommandContext{Room: "room-1"}, nil)
	if err == nil {
		t.Fatal("expected error for nil deps")
	}
}

func TestStatsCommand_Execute_NilStatsRepo(t *testing.T) {
	deps := &Dependencies{
		Formatter: adapter.NewResponseFormatter("!", nil),
		SendMessage: func(_ context.Context, _, _ string) error {
			return nil
		},
		SendError: func(_ context.Context, _, _ string) error { return nil },
		Logger:    slog.New(slog.DiscardHandler),
	}

	cmd := NewStatsCommand(deps)

	err := cmd.Execute(t.Context(), &domain.CommandContext{Room: "room-1"}, nil)
	if err == nil {
		t.Fatal("expected error for nil StatsRepository")
	}
}

func TestStatsCommand_Execute_WithPeriodParam(t *testing.T) {
	var sentMessage string

	repo := &statsRepoStub{
		gainers: []domain.RankEntry{
			{ChannelID: "ch-1", MemberName: "미코", Value: 1000, Rank: 1},
		},
	}

	deps := &Dependencies{
		StatsRepository: repo,
		Formatter:       adapter.NewResponseFormatter("!", nil),
		SendMessage: func(_ context.Context, _, message string) error {
			sentMessage = message
			return nil
		},
		SendError: func(_ context.Context, _, _ string) error { return nil },
		Logger:    slog.New(slog.DiscardHandler),
	}

	cmd := NewStatsCommand(deps)
	err := cmd.Execute(t.Context(), &domain.CommandContext{Room: "room-1"}, map[string]any{
		"action": "gainers",
		"period": "monthly",
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if sentMessage == "" {
		t.Fatal("expected non-empty stats message with period param")
	}
}
