package handlers

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/park285/iris-client-go/iris"

	"github.com/kapu/hololive-kakao-bot-go/internal/adapter/messaging/formatter"
)

type calendarRepoStub struct {
	entries []domain.CalendarEntry
	err     error
	calls   int
	months  []int
	years   []int
}

func (s *calendarRepoStub) FindMembersWithCelebrationsInMonth(_ context.Context, month, year int) ([]domain.CalendarEntry, error) {
	s.calls++
	s.months = append(s.months, month)
	s.years = append(s.years, year)
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

func TestCalendarCommand_Execute_NextMonthAcrossYear(t *testing.T) {
	deps := &Dependencies{
		Formatter:   formatter.NewResponseFormatter("!", nil),
		SendMessage: func(_ context.Context, _, _ string) error { return nil },
		SendError:   func(_ context.Context, _, _ string) error { return nil },
		Logger:      slog.Default(),
	}
	repo := &calendarRepoStub{}
	cmd := NewCalendarCommand(deps, repo, nil)
	cmd.now = func() time.Time {
		return time.Date(2026, time.December, 15, 12, 0, 0, 0, time.FixedZone("KST", 9*60*60))
	}

	err := cmd.Execute(context.Background(), &domain.CommandContext{Room: "test-room"}, map[string]any{"monthOffset": 1})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if len(repo.months) != 1 || repo.months[0] != 1 {
		t.Fatalf("repo months = %v, want [1]", repo.months)
	}
	if len(repo.years) != 1 || repo.years[0] != 2027 {
		t.Fatalf("repo years = %v, want [2027]", repo.years)
	}
}

func TestCalendarCommand_Execute_PreviousMonthAcrossYear(t *testing.T) {
	deps := &Dependencies{
		Formatter:   formatter.NewResponseFormatter("!", nil),
		SendMessage: func(_ context.Context, _, _ string) error { return nil },
		SendError:   func(_ context.Context, _, _ string) error { return nil },
		Logger:      slog.Default(),
	}
	repo := &calendarRepoStub{}
	cmd := NewCalendarCommand(deps, repo, nil)
	cmd.now = func() time.Time {
		return time.Date(2026, time.January, 15, 12, 0, 0, 0, time.FixedZone("KST", 9*60*60))
	}

	err := cmd.Execute(context.Background(), &domain.CommandContext{Room: "test-room"}, map[string]any{"monthOffset": -1})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if len(repo.months) != 1 || repo.months[0] != 12 {
		t.Fatalf("repo months = %v, want [12]", repo.months)
	}
	if len(repo.years) != 1 || repo.years[0] != 2025 {
		t.Fatalf("repo years = %v, want [2025]", repo.years)
	}
}

func TestCachedCelebrationCalendarFinder_ReusesSnapshotAcrossInstances(t *testing.T) {
	now := time.Date(2026, 6, 4, 12, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	entries := []domain.CalendarEntry{
		{Kind: domain.CelebrationKindBirthday, Member: &domain.Member{ShortKoreanName: "페코라"}, Day: 15},
	}

	repo := &calendarRepoStub{entries: entries}
	finder := newCachedCelebrationCalendarFinder(repo, dir, time.Hour, func() time.Time { return now })
	if finder == nil {
		t.Fatal("newCachedCelebrationCalendarFinder() returned nil")
	}

	first, err := finder.FindMembersWithCelebrationsInMonth(context.Background(), 6, 2026)
	if err != nil {
		t.Fatalf("first FindMembersWithCelebrationsInMonth() error = %v", err)
	}
	if repo.calls != 1 {
		t.Fatalf("repo calls after first lookup = %d, want 1", repo.calls)
	}

	cachedRepo := &calendarRepoStub{err: errors.New("db should not be called")}
	cachedFinder := newCachedCelebrationCalendarFinder(cachedRepo, dir, time.Hour, func() time.Time {
		return now.Add(30 * time.Minute)
	})
	if cachedFinder == nil {
		t.Fatal("newCachedCelebrationCalendarFinder() returned nil")
	}

	second, err := cachedFinder.FindMembersWithCelebrationsInMonth(context.Background(), 6, 2026)
	if err != nil {
		t.Fatalf("second FindMembersWithCelebrationsInMonth() error = %v", err)
	}
	if cachedRepo.calls != 0 {
		t.Fatalf("repo calls after cached lookup = %d, want 0", cachedRepo.calls)
	}
	if len(second) != len(first) || second[0].Member.ShortKoreanName != "페코라" {
		t.Fatalf("cached entries = %#v, want first snapshot", second)
	}
}

func TestCachedCelebrationCalendarFinder_RefreshesExpiredSnapshot(t *testing.T) {
	now := time.Date(2026, 6, 4, 12, 0, 0, 0, time.UTC)
	dir := t.TempDir()

	repo := &calendarRepoStub{
		entries: []domain.CalendarEntry{
			{Kind: domain.CelebrationKindBirthday, Member: &domain.Member{ShortKoreanName: "페코라"}, Day: 15},
		},
	}
	finder := newCachedCelebrationCalendarFinder(repo, dir, time.Hour, func() time.Time { return now })
	if finder == nil {
		t.Fatal("newCachedCelebrationCalendarFinder() returned nil")
	}
	if _, err := finder.FindMembersWithCelebrationsInMonth(context.Background(), 6, 2026); err != nil {
		t.Fatalf("first FindMembersWithCelebrationsInMonth() error = %v", err)
	}

	refreshedRepo := &calendarRepoStub{
		entries: []domain.CalendarEntry{
			{Kind: domain.CelebrationKindBirthday, Member: &domain.Member{ShortKoreanName: "미코"}, Day: 5},
		},
	}
	refreshedFinder := newCachedCelebrationCalendarFinder(refreshedRepo, dir, time.Hour, func() time.Time {
		return now.Add(2 * time.Hour)
	})
	if refreshedFinder == nil {
		t.Fatal("newCachedCelebrationCalendarFinder() returned nil")
	}

	refreshed, err := refreshedFinder.FindMembersWithCelebrationsInMonth(context.Background(), 6, 2026)
	if err != nil {
		t.Fatalf("refreshed FindMembersWithCelebrationsInMonth() error = %v", err)
	}
	if refreshedRepo.calls != 1 {
		t.Fatalf("repo calls after expired lookup = %d, want 1", refreshedRepo.calls)
	}
	if len(refreshed) != 1 || refreshed[0].Member.ShortKoreanName != "미코" {
		t.Fatalf("refreshed entries = %#v, want refreshed repo snapshot", refreshed)
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

func TestCalendarCommand_Execute_ImageSendFailureFallsBackToText(t *testing.T) {
	var sentMessage string
	deps := &Dependencies{
		Formatter: formatter.NewResponseFormatter("!", nil),
		SendMessage: func(_ context.Context, _, msg string) error {
			sentMessage = msg
			return nil
		},
		SendImage: func(_ context.Context, _ string, _ []byte, _ ...iris.SendOption) error {
			return errors.New("iris image upload failed")
		},
		SendError: func(_ context.Context, _, _ string) error { return nil },
		Logger:    slog.Default(),
	}

	repo := &calendarRepoStub{
		entries: []domain.CalendarEntry{
			{Kind: domain.CelebrationKindBirthday, Member: &domain.Member{ShortKoreanName: "미코"}, Day: 5},
		},
	}
	renderer := &calendarImageRendererStub{data: []byte("png-data")}

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
