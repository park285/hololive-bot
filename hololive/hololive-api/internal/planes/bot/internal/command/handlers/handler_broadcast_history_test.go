package handlers

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/park285/iris-client-go/iris"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers/handlercore"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/service/matcher"
)

type stubBroadcastHistoryRepository struct {
	listQuery handlercore.BroadcastHistoryQuery
	listCalls int
	listErr   error
	entries   []handlercore.BroadcastHistoryEntry
	truncated bool
	getQuery  handlercore.BroadcastThumbnailQuery
	getEntry  *handlercore.BroadcastHistoryEntry
	getErr    error
}

func (s *stubBroadcastHistoryRepository) ListEndedBroadcasts(_ context.Context, query *handlercore.BroadcastHistoryQuery) (handlercore.BroadcastHistoryResult, error) {
	s.listQuery = *query
	s.listCalls++
	if s.listErr != nil {
		return handlercore.BroadcastHistoryResult{}, s.listErr
	}
	return handlercore.BroadcastHistoryResult{Entries: s.entries, Truncated: s.truncated}, nil
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

func (s *stubBroadcastThumbnailDownloader) Download(_ context.Context, entry *handlercore.BroadcastHistoryEntry) (image []byte, contentType string, err error) {
	if entry == nil {
		return nil, "", errors.New("broadcast history entry is required")
	}
	s.entry = *entry
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
	before := time.Now().AddDate(0, 0, -maxBroadcastHistoryDays).Add(-2 * time.Second)
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
	after := time.Now().AddDate(0, 0, -maxBroadcastHistoryDays).Add(2 * time.Second)
	if repo.listQuery.Type != string(BroadcastTypeGame) || repo.listQuery.TopicID != "Forza" || repo.listQuery.Limit != 5 || repo.listQuery.IncludeAll {
		t.Fatalf("query = %+v", repo.listQuery)
	}
	if repo.listQuery.Since.Before(before) || repo.listQuery.Since.After(after) {
		t.Fatalf("Since = %s, want capped all range between %s and %s", repo.listQuery.Since, before, after)
	}
	if sent == "" {
		t.Fatal("expected message to be sent")
	}
	if !strings.Contains(sent, "기간: 최근 365일") || strings.Contains(sent, "기간: 전체") {
		t.Fatalf("sent message = %q, want capped 365-day range", sent)
	}
}

func TestBroadcastHistoryCommandReportsTruncatedResult(t *testing.T) {
	repo := &stubBroadcastHistoryRepository{truncated: true}
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

	if err := NewBroadcastHistoryCommand(deps).Execute(t.Context(), &domain.CommandContext{Room: "room"}, nil); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !strings.Contains(sent, "조회 예산") {
		t.Fatalf("sent message = %q, want truncation notice", sent)
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
	want := "알 수 없는 방송 타입입니다. 사용 가능: 게임, 잡담, 노래, ASMR, 멤버십, 이벤트, 경마, 동시시청, 뉴스, 기타, 미분류"
	if sent != want {
		t.Fatalf("invalid type message = %q, want %q", sent, want)
	}
}

func TestBroadcastHistoryCommandMemberAliasAndTypeQuery(t *testing.T) {
	repo := &stubBroadcastHistoryRepository{}
	memberProvider := newTrackedMemberProvider(&domain.Member{
		ChannelID: "ch-miko",
		Name:      "사쿠라 미코",
		Org:       "Hololive",
		Aliases:   &domain.Aliases{Ko: []string{"미코치"}},
	})
	var sent string
	deps := &Dependencies{
		BroadcastHistory: repo,
		Formatter:        adapter.NewResponseFormatter("!", nil),
		Matcher:          matcher.NewMatcher(nilBaseContext(), memberProvider, nil, nil, nil, slog.New(slog.DiscardHandler)),
		SendMessage: func(_ context.Context, _, message string) error {
			sent = message
			return nil
		},
		SendError: func(_ context.Context, _, _ string) error { return nil },
	}

	err := NewBroadcastHistoryCommand(deps).Execute(context.Background(), &domain.CommandContext{Room: "room"}, map[string]any{
		"member": "미코치",
		"type":   "게임",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if repo.listCalls != 1 {
		t.Fatalf("ListEndedBroadcasts calls = %d, want 1", repo.listCalls)
	}
	if repo.listQuery.ChannelID != "ch-miko" {
		t.Fatalf("ChannelID = %q, want ch-miko", repo.listQuery.ChannelID)
	}
	if repo.listQuery.Type != string(BroadcastTypeGame) {
		t.Fatalf("Type = %q, want %q", repo.listQuery.Type, BroadcastTypeGame)
	}
	if !strings.Contains(sent, "멤버: 사쿠라 미코") || !strings.Contains(sent, "타입: 게임") {
		t.Fatalf("sent message = %q, want resolved member and type filters", sent)
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

func TestPgBroadcastHistoryRepositoryStopsAtScanBudgetForTypeFilter(t *testing.T) {
	pool := dbtest.NewPool(t)
	ctx := t.Context()

	if _, err := pool.Exec(ctx, `DELETE FROM youtube_live_sessions`); err != nil {
		t.Fatalf("clear live sessions: %v", err)
	}

	base := time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)
	if _, err := pool.Exec(ctx, `
		INSERT INTO youtube_live_sessions(video_id, channel_id, status, title, ended_at, last_seen_at)
		SELECT 'talk' || lpad(gs::text, 7, '0'), 'channel-a', 'ENDED', '【雑談】test',
		       $1::timestamptz - (gs * interval '1 minute'),
		       $1::timestamptz - (gs * interval '1 minute')
		FROM generate_series(0, $2 - 1) AS gs
	`, base, maxBroadcastHistoryProcessedRows); err != nil {
		t.Fatalf("insert talk sessions: %v", err)
	}

	gameEndedAt := base.Add(-time.Duration(maxBroadcastHistoryProcessedRows+1) * time.Minute)
	if _, err := pool.Exec(ctx, `
		INSERT INTO youtube_live_sessions(video_id, channel_id, status, title, ended_at, last_seen_at)
		VALUES ($1, $2, 'ENDED', $3, $4, $4)
	`, "game001", "channel-a", "【Minecraft】test", gameEndedAt); err != nil {
		t.Fatalf("insert game session: %v", err)
	}

	repo := &pgBroadcastHistoryRepository{pool: pool}
	result, err := repo.ListEndedBroadcasts(ctx, &handlercore.BroadcastHistoryQuery{
		Type:  string(BroadcastTypeGame),
		Limit: 1,
		Since: base.Add(-24 * time.Hour),
	})
	if err != nil {
		t.Fatalf("ListEndedBroadcasts() error = %v", err)
	}
	if len(result.Entries) != 0 {
		t.Fatalf("len(entries) = %d, want 0 within scan budget", len(result.Entries))
	}
	if !result.Truncated {
		t.Fatal("Truncated = false, want true after scan budget")
	}
}

func TestPgBroadcastHistoryRepositoryPushesStableTopicFilterIntoSQL(t *testing.T) {
	pool := dbtest.NewPool(t)
	ctx := t.Context()

	if _, err := pool.Exec(ctx, `DELETE FROM youtube_live_sessions`); err != nil {
		t.Fatalf("clear live sessions: %v", err)
	}
	base := time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)
	if _, err := pool.Exec(ctx, `
		INSERT INTO youtube_live_sessions(video_id, channel_id, status, title, topic_id, ended_at, last_seen_at)
		SELECT 'topic' || lpad(gs::text, 6, '0'), 'channel-a', 'ENDED', 'test', 'talking',
		       $1::timestamptz - (gs * interval '1 minute'),
		       $1::timestamptz - (gs * interval '1 minute')
		FROM generate_series(0, $2) AS gs
	`, base, maxBroadcastHistoryProcessedRows); err != nil {
		t.Fatalf("insert topic sessions: %v", err)
	}

	repo := &pgBroadcastHistoryRepository{pool: pool}
	result, err := repo.ListEndedBroadcasts(ctx, &handlercore.BroadcastHistoryQuery{
		TopicID: "missing-topic",
		Limit:   1,
		Since:   base.Add(-24 * time.Hour),
	})
	if err != nil {
		t.Fatalf("ListEndedBroadcasts() error = %v", err)
	}
	if len(result.Entries) != 0 || result.Truncated {
		t.Fatalf("result = %+v, want exhausted empty result without scan truncation", result)
	}
}

func TestPgBroadcastHistoryRepositoryElapsedBudgetReturnsTruncatedResult(t *testing.T) {
	repo := &pgBroadcastHistoryRepository{
		queryTimeout: 20 * time.Millisecond,
		pageLoader: func(ctx context.Context, _ *handlercore.BroadcastHistoryQuery, _ *time.Time, _ *time.Time, _ string, _ int) ([]handlercore.BroadcastHistoryEntry, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}

	started := time.Now()
	result, err := repo.ListEndedBroadcasts(t.Context(), &handlercore.BroadcastHistoryQuery{Limit: 1})
	if err != nil {
		t.Fatalf("ListEndedBroadcasts() error = %v", err)
	}
	if !result.Truncated {
		t.Fatal("Truncated = false, want true after elapsed budget")
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("elapsed = %s, want bounded query time", elapsed)
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

func TestPgBroadcastHistoryRepositoryDeduplicatesSharedChannelMembers(t *testing.T) {
	pool := dbtest.NewPool(t)
	ctx := t.Context()

	if _, err := pool.Exec(ctx, `DELETE FROM youtube_live_sessions`); err != nil {
		t.Fatalf("clear live sessions: %v", err)
	}

	channelID := "shared-channel-history-test"
	if _, err := pool.Exec(ctx, `DELETE FROM members WHERE channel_id = $1`, channelID); err != nil {
		t.Fatalf("clear test members: %v", err)
	}

	if _, err := pool.Exec(ctx, `
		INSERT INTO members(slug, channel_id, english_name, korean_name, short_korean_name, status, is_graduated, org, sync_source)
		VALUES
			('history-shared-fuwawa', $1, 'Fuwawa Abyssgard', '후와와 어비스가드', '후와와', 'active', false, 'Hololive', 'manual'),
			('history-shared-mococo', $1, 'Mococo Abyssgard', '모코코 어비스가드', '모코코', 'active', false, 'Hololive', 'manual')
	`, channelID); err != nil {
		t.Fatalf("insert shared channel members: %v", err)
	}

	endedAt := time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)
	if _, err := pool.Exec(ctx, `
		INSERT INTO youtube_live_sessions(video_id, channel_id, status, title, ended_at, last_seen_at)
		VALUES ('shared001', $1, 'ENDED', '【雑談】test', $2, $2)
	`, channelID, endedAt); err != nil {
		t.Fatalf("insert shared channel session: %v", err)
	}

	repo := &pgBroadcastHistoryRepository{pool: pool}
	result, err := repo.ListEndedBroadcasts(ctx, &handlercore.BroadcastHistoryQuery{
		ChannelID: channelID,
		Limit:     10,
		Since:     endedAt.Add(-time.Hour),
	})
	if err != nil {
		t.Fatalf("ListEndedBroadcasts() error = %v", err)
	}
	if len(result.Entries) != 1 {
		t.Fatalf("entries = %d, want 1: %#v", len(result.Entries), result.Entries)
	}
	if result.Entries[0].MemberName != "후와와 / 모코코" {
		t.Fatalf("MemberName = %q, want 후와와 / 모코코", result.Entries[0].MemberName)
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
