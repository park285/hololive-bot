package handlers

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/park285/iris-client-go/iris"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers/handlercore"
)

type stubBroadcastHistoryRepository struct {
	listQuery handlercore.BroadcastHistoryQuery
	listCalls int
	listErr   error
	entries   []handlercore.BroadcastHistoryEntry
	getQuery  handlercore.BroadcastThumbnailQuery
	getEntry  *handlercore.BroadcastHistoryEntry
	getErr    error
}

func (s *stubBroadcastHistoryRepository) ListEndedBroadcasts(_ context.Context, query handlercore.BroadcastHistoryQuery) ([]handlercore.BroadcastHistoryEntry, error) {
	s.listQuery = query
	s.listCalls++
	if s.listErr != nil {
		return nil, s.listErr
	}
	return s.entries, nil
}

func (s *stubBroadcastHistoryRepository) GetEndedBroadcast(_ context.Context, query handlercore.BroadcastThumbnailQuery) (*handlercore.BroadcastHistoryEntry, error) {
	s.getQuery = query
	if s.getErr != nil {
		return nil, s.getErr
	}
	return s.getEntry, nil
}

type stubBroadcastThumbnailDownloader struct {
	entry handlercore.BroadcastHistoryEntry
	err   error
}

func (s *stubBroadcastThumbnailDownloader) Download(_ context.Context, entry handlercore.BroadcastHistoryEntry) ([]byte, string, error) {
	s.entry = entry
	if s.err != nil {
		return nil, "", s.err
	}
	return []byte("jpeg"), "image/jpeg", nil
}

func TestBroadcastHistoryCommandExecute(t *testing.T) {
	endedAt := time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)
	repo := &stubBroadcastHistoryRepository{
		entries: []handlercore.BroadcastHistoryEntry{
			{
				VideoID:       "AqxEw3kXcgU",
				MemberName:    "테스트",
				Title:         "【Forza】test",
				TopicID:       "Forza",
				BroadcastType: string(BroadcastTypeGame),
				EndedAt:       &endedAt,
				LastSeenAt:    endedAt,
			},
		},
	}
	var sent string
	deps := &Dependencies{
		BroadcastHistory: repo,
		Formatter:        adapter.NewResponseFormatter("!", nil),
		SendMessage: func(_ context.Context, _, message string) error {
			sent = message
			return nil
		},
		SendError: func(_ context.Context, _, _ string) error { return nil },
	}

	cmd := NewBroadcastHistoryCommand(deps)
	err := cmd.Execute(context.Background(), &domain.CommandContext{Room: "room"}, map[string]any{
		"type":  "게임",
		"topic": "Forza",
		"limit": 5,
		"all":   true,
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if repo.listCalls != 1 {
		t.Fatalf("ListEndedBroadcasts calls = %d, want 1", repo.listCalls)
	}
	if repo.listQuery.Type != string(BroadcastTypeGame) || repo.listQuery.TopicID != "Forza" || repo.listQuery.Limit != 5 || !repo.listQuery.IncludeAll {
		t.Fatalf("query = %+v", repo.listQuery)
	}
	if sent == "" {
		t.Fatal("expected message to be sent")
	}
}

func TestBroadcastHistoryCommandInvalidType(t *testing.T) {
	repo := &stubBroadcastHistoryRepository{}
	var sent string
	deps := &Dependencies{
		BroadcastHistory: repo,
		Formatter:        adapter.NewResponseFormatter("!", nil),
		SendMessage: func(_ context.Context, _, message string) error {
			sent = message
			return nil
		},
		SendError: func(_ context.Context, _, _ string) error { return nil },
	}

	err := NewBroadcastHistoryCommand(deps).Execute(context.Background(), &domain.CommandContext{Room: "room"}, map[string]any{
		"type": "not-a-type",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if repo.listCalls != 0 {
		t.Fatalf("ListEndedBroadcasts calls = %d, want 0", repo.listCalls)
	}
	if sent == "" {
		t.Fatal("expected invalid type message")
	}
}

func TestBroadcastHistoryCommandDefaultDaysIsOneWeek(t *testing.T) {
	repo := &stubBroadcastHistoryRepository{}
	deps := &Dependencies{
		BroadcastHistory: repo,
		Formatter:        adapter.NewResponseFormatter("!", nil),
		SendMessage:      func(_ context.Context, _, _ string) error { return nil },
		SendError:        func(_ context.Context, _, _ string) error { return nil },
	}
	before := time.Now().AddDate(0, 0, -defaultBroadcastHistoryDays).Add(-2 * time.Second)

	err := NewBroadcastHistoryCommand(deps).Execute(context.Background(), &domain.CommandContext{Room: "room"}, map[string]any{})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	after := time.Now().AddDate(0, 0, -defaultBroadcastHistoryDays).Add(2 * time.Second)

	if repo.listQuery.IncludeAll {
		t.Fatal("IncludeAll = true, want false")
	}
	if repo.listQuery.Since.Before(before) || repo.listQuery.Since.After(after) {
		t.Fatalf("Since = %s, want around one week ago between %s and %s", repo.listQuery.Since, before, after)
	}
}

func TestBroadcastHistoryCommandListErrorSendsOneUserMessage(t *testing.T) {
	repo := &stubBroadcastHistoryRepository{listErr: errors.New("db down")}
	var sent []string
	deps := &Dependencies{
		BroadcastHistory: repo,
		Formatter:        adapter.NewResponseFormatter("!", nil),
		SendMessage: func(_ context.Context, _, message string) error {
			sent = append(sent, message)
			return nil
		},
		SendError: func(_ context.Context, _, message string) error {
			t.Fatalf("unexpected generic error response: %s", message)
			return nil
		},
		Logger: slog.New(slog.DiscardHandler),
	}

	err := NewBroadcastHistoryCommand(deps).Execute(context.Background(), &domain.CommandContext{Room: "room"}, map[string]any{})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if len(sent) != 1 {
		t.Fatalf("sent messages = %d, want 1: %#v", len(sent), sent)
	}
}

func TestPgBroadcastHistoryRepositoryListEndedBroadcastsScansPastFirstPageForTypeFilter(t *testing.T) {
	pool := dbtest.NewPool(t)
	ctx := t.Context()

	if _, err := pool.Exec(ctx, `DELETE FROM youtube_live_sessions`); err != nil {
		t.Fatalf("clear live sessions: %v", err)
	}

	base := time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)
	for i := 0; i < broadcastHistoryPageSize; i++ {
		endedAt := base.Add(-time.Duration(i) * time.Minute)
		if _, err := pool.Exec(ctx, `
			INSERT INTO youtube_live_sessions(video_id, channel_id, status, title, ended_at, last_seen_at)
			VALUES ($1, $2, 'ENDED', $3, $4, $4)
		`, fmt.Sprintf("talk%03d", i), "channel-a", "【雑談】test", endedAt); err != nil {
			t.Fatalf("insert talk session %d: %v", i, err)
		}
	}

	gameEndedAt := base.Add(-time.Duration(broadcastHistoryPageSize+1) * time.Minute)
	if _, err := pool.Exec(ctx, `
		INSERT INTO youtube_live_sessions(video_id, channel_id, status, title, ended_at, last_seen_at)
		VALUES ($1, $2, 'ENDED', $3, $4, $4)
	`, "game001", "channel-a", "【Minecraft】test", gameEndedAt); err != nil {
		t.Fatalf("insert game session: %v", err)
	}

	repo := &pgBroadcastHistoryRepository{pool: pool}
	entries, err := repo.ListEndedBroadcasts(ctx, handlercore.BroadcastHistoryQuery{
		Type:  string(BroadcastTypeGame),
		Limit: 1,
		Since: base.Add(-24 * time.Hour),
	})
	if err != nil {
		t.Fatalf("ListEndedBroadcasts() error = %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("len(entries) = %d, want 1", len(entries))
	}
	if entries[0].VideoID != "game001" {
		t.Fatalf("entry video_id = %q, want game001", entries[0].VideoID)
	}
}

func TestPgBroadcastHistoryRepositoryUsesLiveEventMetadataFallback(t *testing.T) {
	pool := dbtest.NewPool(t)
	ctx := t.Context()

	if _, err := pool.Exec(ctx, `DELETE FROM youtube_live_sessions WHERE video_id = 'fallback001'`); err != nil {
		t.Fatalf("clear live session: %v", err)
	}
	endedAt := time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)
	if _, err := pool.Exec(ctx, `
		INSERT INTO youtube_live_sessions(video_id, channel_id, status, title, ended_at, last_seen_at)
		VALUES ($1, $2, 'ENDED', $3, $4, $4)
	`, "fallback001", "channel-a", "test", endedAt); err != nil {
		t.Fatalf("insert live session: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO alarm_dispatch_events(event_key, payload_hash, alarm_type, channel_id, stream_id, category, payload)
		VALUES (
			'broadcast-history-fallback-001',
			'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
			'LIVE'::alarm_type,
			'channel-a',
			'fallback001',
			'',
			'{"notification":{"stream":{"topic_id":"minecraft","thumbnail":"https://i.ytimg.com/vi/fallback001/maxresdefault.jpg"}}}'::jsonb
		)
		ON CONFLICT (event_key) DO NOTHING
	`); err != nil {
		t.Fatalf("insert live event: %v", err)
	}

	repo := &pgBroadcastHistoryRepository{pool: pool}
	entry, err := repo.GetEndedBroadcast(ctx, handlercore.BroadcastThumbnailQuery{VideoID: "fallback001"})
	if err != nil {
		t.Fatalf("GetEndedBroadcast() error = %v", err)
	}
	if entry == nil {
		t.Fatal("entry = nil, want live session")
	}
	if entry.TopicID != "minecraft" {
		t.Fatalf("TopicID = %q, want minecraft", entry.TopicID)
	}
	if entry.ThumbnailURL != "https://i.ytimg.com/vi/fallback001/maxresdefault.jpg" {
		t.Fatalf("ThumbnailURL = %q, want maxres fallback URL", entry.ThumbnailURL)
	}
	if entry.BroadcastType != string(BroadcastTypeGame) || entry.BroadcastTypeSource != "topic" {
		t.Fatalf("classification = {%q %q}, want {game topic}", entry.BroadcastType, entry.BroadcastTypeSource)
	}
}

func TestBroadcastThumbnailCommandExecute(t *testing.T) {
	entry := handlercore.BroadcastHistoryEntry{VideoID: "AqxEw3kXcgU", Title: "test"}
	repo := &stubBroadcastHistoryRepository{getEntry: &entry}
	downloader := &stubBroadcastThumbnailDownloader{}
	var sentImage []byte
	deps := &Dependencies{
		BroadcastHistory:    repo,
		ThumbnailDownloader: downloader,
		SendMessage:         func(_ context.Context, _, _ string) error { return nil },
		SendError:           func(_ context.Context, _, _ string) error { return nil },
		SendImage: func(_ context.Context, _ string, imageData []byte, _ ...iris.SendOption) error {
			sentImage = imageData
			return nil
		},
	}

	err := NewBroadcastThumbnailCommand(deps).Execute(context.Background(), &domain.CommandContext{Room: "room"}, map[string]any{
		"video_id": "AqxEw3kXcgU",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if repo.getQuery.VideoID != "AqxEw3kXcgU" {
		t.Fatalf("GetEndedBroadcast video_id = %q", repo.getQuery.VideoID)
	}
	if downloader.entry.VideoID != "AqxEw3kXcgU" {
		t.Fatalf("downloader entry video_id = %q", downloader.entry.VideoID)
	}
	if string(sentImage) != "jpeg" {
		t.Fatalf("sent image = %q, want jpeg", string(sentImage))
	}
}

func TestBroadcastThumbnailCommandLookupErrorSendsOneUserMessage(t *testing.T) {
	repo := &stubBroadcastHistoryRepository{getErr: errors.New("db down")}
	var sent []string
	deps := &Dependencies{
		BroadcastHistory:    repo,
		ThumbnailDownloader: &stubBroadcastThumbnailDownloader{},
		SendMessage: func(_ context.Context, _, message string) error {
			sent = append(sent, message)
			return nil
		},
		SendError: func(_ context.Context, _, message string) error {
			t.Fatalf("unexpected generic error response: %s", message)
			return nil
		},
		SendImage: func(_ context.Context, _ string, _ []byte, _ ...iris.SendOption) error {
			t.Fatal("unexpected image send")
			return nil
		},
		Logger: slog.New(slog.DiscardHandler),
	}

	err := NewBroadcastThumbnailCommand(deps).Execute(context.Background(), &domain.CommandContext{Room: "room"}, map[string]any{
		"video_id": "AqxEw3kXcgU",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if len(sent) != 1 {
		t.Fatalf("sent messages = %d, want 1: %#v", len(sent), sent)
	}
}

func TestBroadcastThumbnailCommandDownloadErrorSendsOneUserMessage(t *testing.T) {
	entry := handlercore.BroadcastHistoryEntry{VideoID: "AqxEw3kXcgU", Title: "test"}
	repo := &stubBroadcastHistoryRepository{getEntry: &entry}
	var sent []string
	deps := &Dependencies{
		BroadcastHistory:    repo,
		ThumbnailDownloader: &stubBroadcastThumbnailDownloader{err: errors.New("thumbnail timeout")},
		SendMessage: func(_ context.Context, _, message string) error {
			sent = append(sent, message)
			return nil
		},
		SendError: func(_ context.Context, _, message string) error {
			t.Fatalf("unexpected generic error response: %s", message)
			return nil
		},
		SendImage: func(_ context.Context, _ string, _ []byte, _ ...iris.SendOption) error {
			t.Fatal("unexpected image send")
			return nil
		},
		Logger: slog.New(slog.DiscardHandler),
	}

	err := NewBroadcastThumbnailCommand(deps).Execute(context.Background(), &domain.CommandContext{Room: "room"}, map[string]any{
		"video_id": "AqxEw3kXcgU",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if len(sent) != 1 {
		t.Fatalf("sent messages = %d, want 1: %#v", len(sent), sent)
	}
}
