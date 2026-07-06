package checking

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	sharedconstants "github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPgYouTubeLiveSessionSourceLoadRecentSessionsAndDispatchLookup(t *testing.T) {
	t.Parallel()

	pool := dbtest.NewPool(t)

	now := time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC)
	liveStart := now.Add(-30 * time.Minute)
	liveFirstSeen := now.Add(-10 * time.Minute)
	upcomingStart := now.Add(4 * time.Minute)
	oldSeen := now.Add(-(defaultPersistedLiveSessionRecentWindow + time.Second))
	recentSeen := now.Add(-time.Minute)

	insertLiveSessions(t, pool, []domain.YouTubeLiveSession{
		{
			VideoID:         "live-included",
			ChannelID:       "ch-1",
			Status:          domain.LiveStatusLive,
			Title:           " live title ",
			StartedAt:       &liveStart,
			LiveFirstSeenAt: &liveFirstSeen,
			TopicID:         "Rhythm_Heaven",
			ThumbnailURL:    "https://i.ytimg.com/vi/live-included/maxresdefault.jpg",
			LastSeenAt:      recentSeen,
		},
		{
			VideoID:            "upcoming-included",
			ChannelID:          "ch-1",
			Status:             domain.LiveStatusUpcoming,
			ScheduledStartTime: &upcomingStart,
			LastSeenAt:         recentSeen,
		},
		{
			VideoID:    "live-too-old",
			ChannelID:  "ch-1",
			Status:     domain.LiveStatusLive,
			StartedAt:  &liveStart,
			LastSeenAt: oldSeen,
		},
		{
			VideoID:    "other-channel",
			ChannelID:  "ch-2",
			Status:     domain.LiveStatusLive,
			StartedAt:  &liveStart,
			LastSeenAt: recentSeen,
		},
	})

	source := &PgYouTubeLiveSessionSource{pool: pool}
	sessions, err := source.LoadRecentSessions(t.Context(), []string{"ch-1"}, now)
	require.NoError(t, err)

	gotIDs := make([]string, 0, len(sessions))
	for _, session := range sessions {
		gotIDs = append(gotIDs, session.Stream.ID)
	}
	assert.ElementsMatch(t, []string{"live-included", "upcoming-included"}, gotIDs)
	assert.Equal(t, "live title", sessionsByID(sessions)["live-included"].Stream.Title)
	assert.Equal(t, liveFirstSeen, sessionsByID(sessions)["live-included"].LiveFirstSeenAt)
	require.NotNil(t, sessionsByID(sessions)["live-included"].Stream.TopicID)
	assert.Equal(t, "Rhythm_Heaven", *sessionsByID(sessions)["live-included"].Stream.TopicID)
	require.NotNil(t, sessionsByID(sessions)["live-included"].Stream.Thumbnail)
	assert.Equal(t, "https://i.ytimg.com/vi/live-included/maxresdefault.jpg", *sessionsByID(sessions)["live-included"].Stream.Thumbnail)
	require.NotNil(t, sessionsByID(sessions)["live-included"].Stream.Link)
	assert.Equal(t, "https://youtube.com/watch?v=live-included", *sessionsByID(sessions)["live-included"].Stream.Link)

	insertAlarmDispatchEvents(t, pool, []testAlarmDispatchEvent{
		{ID: 1, AlarmType: string(domain.AlarmTypeLive), StreamID: "live-included", CreatedAt: now.Add(-time.Hour)},
		{ID: 2, AlarmType: string(domain.AlarmTypeCommunity), StreamID: "upcoming-included", CreatedAt: now.Add(-time.Hour)},
		{ID: 3, AlarmType: string(domain.AlarmTypeLive), StreamID: "old-dispatch", CreatedAt: now.Add(-25 * time.Hour)},
		{ID: 4, AlarmType: string(domain.AlarmTypeLive), StreamID: "pending-live", CreatedAt: now.Add(-time.Hour)},
	})

	dispatched, err := source.RecentlyDispatchedStreamIDs(t.Context(), []string{"live-included", "upcoming-included", "old-dispatch"}, now.Add(-24*time.Hour))
	require.NoError(t, err)
	assert.Contains(t, dispatched, "live-included")
	assert.NotContains(t, dispatched, "upcoming-included")
	assert.NotContains(t, dispatched, "old-dispatch")

	sentAt := now.Add(-30 * time.Minute)
	oldSentAt := now.Add(-25 * time.Hour)
	insertAlarmDispatchDeliveries(t, pool, []testAlarmDispatchDelivery{
		{EventID: 1, RoomID: "room-1", Status: "sent", SentAt: &sentAt, CreatedAt: now.Add(-time.Hour)},
		{EventID: 1, RoomID: "room-2", Status: "retry", SentAt: nil, CreatedAt: now.Add(-time.Hour)},
		{EventID: 2, RoomID: "room-3", Status: "sent", SentAt: &sentAt, CreatedAt: now.Add(-time.Hour)},
		{EventID: 3, RoomID: "room-4", Status: "sent", SentAt: &oldSentAt, CreatedAt: now.Add(-25 * time.Hour)},
		{EventID: 4, RoomID: "room-5", Status: "pending", SentAt: nil, CreatedAt: now.Add(-time.Hour)},
	})

	sentRooms, err := source.RecentlySentLiveStreamRooms(t.Context(), []string{"live-included", "upcoming-included", "old-dispatch", "pending-live"}, now.Add(-24*time.Hour))
	require.NoError(t, err)
	assert.Contains(t, sentRooms["live-included"], "room-1")
	assert.NotContains(t, sentRooms["live-included"], "room-2")
	assert.NotContains(t, sentRooms, "upcoming-included")
	assert.NotContains(t, sentRooms, "old-dispatch")
	assert.NotContains(t, sentRooms, "pending-live")
}

func TestPgYouTubeLiveSessionSourceLoadRecentLiveChannelIDs(t *testing.T) {
	t.Parallel()

	pool := dbtest.NewPool(t)

	now := time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC)
	recentSeen := now.Add(-time.Minute)
	oldSeen := now.Add(-20 * time.Minute)

	insertLiveSessions(t, pool, []domain.YouTubeLiveSession{
		{VideoID: "live-recent", ChannelID: "ch-1", Status: domain.LiveStatusLive, LastSeenAt: recentSeen},
		{VideoID: "live-old", ChannelID: "ch-2", Status: domain.LiveStatusLive, LastSeenAt: oldSeen},
		{VideoID: "ended-recent", ChannelID: "ch-3", Status: domain.LiveStatusEnded, LastSeenAt: recentSeen},
		{VideoID: "outside-request", ChannelID: "ch-4", Status: domain.LiveStatusLive, LastSeenAt: recentSeen},
	})

	source := &PgYouTubeLiveSessionSource{pool: pool}
	channels, err := source.LoadRecentLiveChannelIDs(t.Context(), []string{"ch-1", "ch-2", "ch-3"}, now)
	require.NoError(t, err)
	assert.Equal(t, []string{"ch-1"}, channels)
}

func TestPgYouTubeLiveSessionSourceLiveRecentWindowIndependentFromCatchupWindow(t *testing.T) {
	t.Parallel()

	pool := dbtest.NewPool(t)

	now := time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC)
	start := now.Add(-30 * time.Minute)
	lastSeenOutsideCatchup := now.Add(-(sharedconstants.LiveCatchupWindow + time.Minute))

	insertLiveSessions(t, pool, []domain.YouTubeLiveSession{{
		VideoID:    "live-window-indep",
		ChannelID:  "ch-1",
		Status:     domain.LiveStatusLive,
		StartedAt:  &start,
		LastSeenAt: lastSeenOutsideCatchup,
	}})

	source := &PgYouTubeLiveSessionSource{pool: pool}
	sessions, err := source.LoadRecentSessions(t.Context(), []string{"ch-1"}, now)
	require.NoError(t, err)
	require.Len(t, sessions, 1)
	assert.Equal(t, "live-window-indep", sessions[0].Stream.ID)
}

type testAlarmDispatchEvent struct {
	ID        int64
	AlarmType string
	StreamID  string
	CreatedAt time.Time
}

type testAlarmDispatchDelivery struct {
	EventID   int64
	RoomID    string
	Status    string
	SentAt    *time.Time
	CreatedAt time.Time
}

type liveSessionPool interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
}

func insertLiveSessions(t *testing.T, pool liveSessionPool, sessions []domain.YouTubeLiveSession) {
	t.Helper()

	for i := range sessions {
		session := &sessions[i]
		_, err := pool.Exec(t.Context(), `
			INSERT INTO youtube_live_sessions(
				video_id, channel_id, status, title, scheduled_start_time,
				started_at, ended_at, live_first_seen_at, topic_id, thumbnail_url, last_seen_at
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		`,
			session.VideoID,
			session.ChannelID,
			session.Status,
			session.Title,
			session.ScheduledStartTime,
			session.StartedAt,
			session.EndedAt,
			session.LiveFirstSeenAt,
			session.TopicID,
			session.ThumbnailURL,
			session.LastSeenAt,
		)
		require.NoError(t, err)
	}
}

func insertAlarmDispatchEvents(t *testing.T, pool liveSessionPool, events []testAlarmDispatchEvent) {
	t.Helper()

	for _, event := range events {
		_, err := pool.Exec(t.Context(), `
			INSERT INTO alarm_dispatch_events(
				id, event_key, payload_hash, alarm_type, channel_id, stream_id, payload, created_at, updated_at
			)
			VALUES ($1, $2, $3, $4::alarm_type, $5, $6, '{}'::jsonb, $7, $7)
		`,
			event.ID,
			"event-"+event.StreamID,
			"0000000000000000000000000000000000000000000000000000000000000001",
			event.AlarmType,
			"channel-"+event.StreamID,
			event.StreamID,
			event.CreatedAt,
		)
		require.NoError(t, err)
	}
}

func insertAlarmDispatchDeliveries(t *testing.T, pool liveSessionPool, deliveries []testAlarmDispatchDelivery) {
	t.Helper()

	for index, delivery := range deliveries {
		_, err := pool.Exec(t.Context(), `
			INSERT INTO alarm_dispatch_deliveries(
				event_id, room_id, dedupe_key, status, sent_at, created_at, updated_at
			)
			VALUES ($1, $2, $3, $4, $5, $6, $6)
		`,
			delivery.EventID,
			delivery.RoomID,
			fmt.Sprintf("dedupe-%d-%s", index, delivery.RoomID),
			delivery.Status,
			delivery.SentAt,
			delivery.CreatedAt,
		)
		require.NoError(t, err)
	}
}

func sessionsByID(sessions []PersistedYouTubeLiveSession) map[string]PersistedYouTubeLiveSession {
	byID := make(map[string]PersistedYouTubeLiveSession, len(sessions))
	for _, session := range sessions {
		if session.Stream == nil {
			continue
		}
		byID[session.Stream.ID] = session
	}
	return byID
}
