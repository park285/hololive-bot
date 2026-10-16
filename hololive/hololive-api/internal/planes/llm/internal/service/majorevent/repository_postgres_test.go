package majorevent_test

import (
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-api/internal/planes/llm/internal/service/majorevent"
	"github.com/kapu/hololive-api/internal/planes/llm/internal/service/majorevent/scraper"
	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
	databasemocks "github.com/kapu/hololive-shared/pkg/service/database/mocks"
)

// bot은 방 이름을 모르므로(Iris webhook에 방 제목 없음) 빈 이름으로 구독한다. 모르는 이름은 NULL로 저장하고, 재구독이
// 저장된 이름을 지우거나 방 ID로 덮어쓰지 않아야 한다.
func TestRepositoryPostgres_SubscribeKeepsKnownRoomNameWhenNameUnknown(t *testing.T) {
	pool := dbtest.NewPool(t)
	repository := majorevent.NewRepository(&databasemocks.Client{GetPoolFunc: func() *pgxpool.Pool { return pool }}, nil)
	ctx := t.Context()

	const (
		namedRoom   = "me-sub-named"
		unknownRoom = "me-sub-unknown"
	)

	subscribe := func(roomID, roomName string) {
		t.Helper()

		if err := repository.Subscribe(ctx, roomID, roomName); err != nil {
			t.Fatalf("Subscribe(%q, %q): %v", roomID, roomName, err)
		}
	}

	storedName := func(roomID string) (name string, isNull bool) {
		t.Helper()

		if err := pool.QueryRow(ctx,
			`SELECT COALESCE(room_name, ''), room_name IS NULL FROM major_event_subscriptions WHERE room_id = $1`, roomID,
		).Scan(&name, &isNull); err != nil {
			t.Fatalf("read room_name for %q: %v", roomID, err)
		}

		return name, isNull
	}

	subscribe(namedRoom, "홀로 방")
	subscribe(unknownRoom, "")

	if name, isNull := storedName(unknownRoom); !isNull {
		t.Fatalf("unknown room name stored as %q, want NULL", name)
	}

	subscribe(namedRoom, "")
	subscribe(namedRoom, "   ")

	if name, _ := storedName(namedRoom); name != "홀로 방" {
		t.Fatalf("re-subscribe with unknown name left room_name = %q, want 홀로 방", name)
	}

	subscribe(unknownRoom, "새 방")

	rooms, err := repository.GetSubscribedRooms(ctx)
	if err != nil {
		t.Fatalf("GetSubscribedRooms: %v", err)
	}

	got := map[string]string{}

	for _, room := range rooms {
		got[room.RoomID] = room.RoomName
	}

	if got[namedRoom] != "홀로 방" || got[unknownRoom] != "새 방" {
		t.Fatalf("GetSubscribedRooms names = %v, want %s=홀로 방 and %s=새 방", got, namedRoom, unknownRoom)
	}
}

func TestRepositoryPostgres_GetExistingExternalIDs(t *testing.T) {
	const oldNewsID = "old-news"

	pool := dbtest.NewPool(t)
	repository := majorevent.NewRepository(&databasemocks.Client{GetPoolFunc: func() *pgxpool.Pool { return pool }}, nil)
	ctx := t.Context()
	oldDate := time.Date(2020, time.January, 1, 0, 0, 0, 0, time.UTC)
	newDate := oldDate.AddDate(6, 0, 0)

	for _, event := range []*domain.MajorEvent{
		{ExternalID: oldNewsID, Type: domain.MajorEventTypeNews, PubDate: &oldDate},
		{ExternalID: "new-news", Type: domain.MajorEventTypeNews, PubDate: &newDate},
		{ExternalID: "undated-news", Type: domain.MajorEventTypeNews},
		{ExternalID: "other-type", Type: domain.MajorEventTypeEvent, PubDate: &oldDate},
	} {
		event.Title = event.ExternalID
		event.Link = "https://hololive.hololivepro.com/news/" + event.ExternalID
		event.Status = domain.MajorEventStatusActive
		event.Members = []string{}

		if err := repository.UpsertEvent(ctx, event); err != nil {
			t.Fatalf("UpsertEvent(%q): %v", event.ExternalID, err)
		}
	}

	ids, err := repository.GetExistingExternalIDs(ctx, domain.MajorEventTypeNews,
		[]string{oldNewsID, "undated-news", oldNewsID, "other-type", "missing"})
	if err != nil {
		t.Fatalf("GetExistingExternalIDs: %v", err)
	}

	slices.Sort(ids)

	if !slices.Equal(ids, []string{oldNewsID, "undated-news"}) {
		t.Fatalf("existing news IDs = %v; want requested IDs regardless of publication date", ids)
	}

	ids, err = repository.GetExistingExternalIDs(ctx, domain.MajorEventTypeEvent, []string{"other-type", oldNewsID})
	if err != nil || !slices.Equal(ids, []string{"other-type"}) {
		t.Fatalf("existing event IDs = %v, %v", ids, err)
	}

	ids, err = repository.GetExistingExternalIDs(ctx, domain.MajorEventTypeNews, nil)
	if err != nil || len(ids) != 0 {
		t.Fatalf("empty lookup = %v, %v", ids, err)
	}
}

func TestRepositoryPostgres_ScrapeRecoversPartialPersistence(t *testing.T) {
	pool := dbtest.NewPool(t)
	repository := majorevent.NewRepository(&databasemocks.Client{GetPoolFunc: func() *pgxpool.Pool { return pool }}, nil)
	ctx := t.Context()

	const feed = `<?xml version="1.0"?>
<rss version="2.0"><channel><title>News</title>
<item><title>Older A</title><guid>scrape-older-a</guid><link>https://hololive.hololivepro.com/news/a/</link><pubDate>Thu, 09 Jan 2025 05:00:00 +0000</pubDate></item>
<item><title>Newer B</title><guid>scrape-newer-b</guid><link>https://hololive.hololivepro.com/news/b/</link><pubDate>Fri, 10 Jan 2025 05:00:00 +0000</pubDate></item>
</channel></rss>`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if _, err := w.Write([]byte(feed)); err != nil {
			t.Errorf("write RSS feed: %v", err)
		}
	}))
	t.Cleanup(server.Close)

	service, err := scraper.NewService(repository,
		scraper.NewFeedFetcher(server.Client(), scraper.DefaultFeedFetcherConfig()), scraper.NewRSSParser(),
		scraper.ServiceConfig{
			Sources:         []scraper.FeedSource{{Name: "news", EventType: domain.MajorEventTypeNews, FeedURL: server.URL}},
			FeedConcurrency: 1, IncrementalLimit: 50,
		}, slog.New(slog.DiscardHandler))
	require.NoError(t, err)

	// 격리된 DB에서 A만 실제 저장 오류를 내고 B의 commit은 유지한다.
	_, err = pool.Exec(ctx, `ALTER TABLE major_events ADD CONSTRAINT test_reject_older_a CHECK (external_id <> 'scrape-older-a')`)
	require.NoError(t, err)

	result, err := service.Scrape(ctx)
	pgErr, ok := errors.AsType[*pgconn.PgError](err)

	require.True(t, ok, "partial failure must retain the PostgreSQL cause: %v", err)
	require.Equal(t, "23514", pgErr.Code)
	require.Equal(t, 1, result.StoredEvents)
	require.Equal(t, 1, result.FailedEvents)
	require.Zero(t, result.FeedsFailed)

	var (
		beforeID        int
		beforeUpdatedAt time.Time
	)

	require.NoError(t, pool.QueryRow(ctx, `SELECT id, updated_at FROM major_events WHERE external_id = 'scrape-newer-b'`).Scan(&beforeID, &beforeUpdatedAt))

	_, err = pool.Exec(ctx, `ALTER TABLE major_events DROP CONSTRAINT test_reject_older_a`)
	require.NoError(t, err)

	result, err = service.Scrape(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, result.StoredEvents)
	require.Zero(t, result.FailedEvents)
	require.Equal(t, 1, result.SkippedKnown)

	var (
		afterID, count int
		afterUpdatedAt time.Time
	)

	require.NoError(t, pool.QueryRow(ctx, `SELECT id, updated_at FROM major_events WHERE external_id = 'scrape-newer-b'`).Scan(&afterID, &afterUpdatedAt))
	require.Equal(t, beforeID, afterID)
	require.True(t, beforeUpdatedAt.Equal(afterUpdatedAt), "known B must not be rewritten")

	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM major_events WHERE external_id = ANY($1::text[])`, []string{"scrape-older-a", "scrape-newer-b"}).Scan(&count))
	require.Equal(t, 2, count)

	result, err = service.Scrape(ctx)
	require.NoError(t, err)
	require.Zero(t, result.StoredEvents)
	require.Equal(t, 2, result.SkippedKnown)
}
