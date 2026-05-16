package checking

import (
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	sharedconstants "github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type testAlarmDispatchEvent struct {
	ID        int64  `gorm:"primaryKey"`
	AlarmType string `gorm:"size:20"`
	StreamID  string
	CreatedAt time.Time
}

func (testAlarmDispatchEvent) TableName() string {
	return "alarm_dispatch_events"
}

type testAlarmDispatchDelivery struct {
	ID        int64 `gorm:"primaryKey"`
	EventID   int64
	RoomID    string
	Status    string
	SentAt    *time.Time
	CreatedAt time.Time
}

func (testAlarmDispatchDelivery) TableName() string {
	return "alarm_dispatch_deliveries"
}

func TestPgYouTubeLiveSessionSourceLoadRecentSessionsAndDispatchLookup(t *testing.T) {
	t.Parallel()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&domain.YouTubeLiveSession{}, &testAlarmDispatchEvent{}, &testAlarmDispatchDelivery{}))

	now := time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC)
	liveStart := now.Add(-30 * time.Minute)
	liveFirstSeen := now.Add(-10 * time.Minute)
	upcomingStart := now.Add(4 * time.Minute)
	oldSeen := now.Add(-(defaultPersistedLiveSessionRecentWindow + time.Second))
	recentSeen := now.Add(-time.Minute)

	require.NoError(t, db.Create([]domain.YouTubeLiveSession{
		{
			VideoID:         "live-included",
			ChannelID:       "ch-1",
			Status:          domain.LiveStatusLive,
			Title:           " live title ",
			StartedAt:       &liveStart,
			LiveFirstSeenAt: &liveFirstSeen,
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
	}).Error)

	source := &PgYouTubeLiveSessionSource{db: db}
	sessions, err := source.LoadRecentSessions(t.Context(), []string{"ch-1"}, now)
	require.NoError(t, err)

	gotIDs := make([]string, 0, len(sessions))
	for _, session := range sessions {
		gotIDs = append(gotIDs, session.Stream.ID)
	}
	assert.ElementsMatch(t, []string{"live-included", "upcoming-included"}, gotIDs)
	assert.Equal(t, "live title", sessionsByID(sessions)["live-included"].Stream.Title)
	assert.Equal(t, liveFirstSeen, sessionsByID(sessions)["live-included"].LiveFirstSeenAt)
	require.NotNil(t, sessionsByID(sessions)["live-included"].Stream.Link)
	assert.Equal(t, "https://youtube.com/watch?v=live-included", *sessionsByID(sessions)["live-included"].Stream.Link)

	require.NoError(t, db.Create([]testAlarmDispatchEvent{
		{ID: 1, AlarmType: string(domain.AlarmTypeLive), StreamID: "live-included", CreatedAt: now.Add(-time.Hour)},
		{ID: 2, AlarmType: string(domain.AlarmTypeCommunity), StreamID: "upcoming-included", CreatedAt: now.Add(-time.Hour)},
		{ID: 3, AlarmType: string(domain.AlarmTypeLive), StreamID: "old-dispatch", CreatedAt: now.Add(-25 * time.Hour)},
		{ID: 4, AlarmType: string(domain.AlarmTypeLive), StreamID: "pending-live", CreatedAt: now.Add(-time.Hour)},
	}).Error)

	dispatched, err := source.RecentlyDispatchedStreamIDs(t.Context(), []string{"live-included", "upcoming-included", "old-dispatch"}, now.Add(-24*time.Hour))
	require.NoError(t, err)
	assert.Contains(t, dispatched, "live-included")
	assert.NotContains(t, dispatched, "upcoming-included")
	assert.NotContains(t, dispatched, "old-dispatch")

	sentAt := now.Add(-30 * time.Minute)
	oldSentAt := now.Add(-25 * time.Hour)
	require.NoError(t, db.Create([]testAlarmDispatchDelivery{
		{EventID: 1, RoomID: "room-1", Status: "sent", SentAt: &sentAt, CreatedAt: now.Add(-time.Hour)},
		{EventID: 1, RoomID: "room-2", Status: "retry", SentAt: nil, CreatedAt: now.Add(-time.Hour)},
		{EventID: 2, RoomID: "room-3", Status: "sent", SentAt: &sentAt, CreatedAt: now.Add(-time.Hour)},
		{EventID: 3, RoomID: "room-4", Status: "sent", SentAt: &oldSentAt, CreatedAt: now.Add(-25 * time.Hour)},
		{EventID: 4, RoomID: "room-5", Status: "pending", SentAt: nil, CreatedAt: now.Add(-time.Hour)},
	}).Error)

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

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&domain.YouTubeLiveSession{}))

	now := time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC)
	recentSeen := now.Add(-time.Minute)
	oldSeen := now.Add(-20 * time.Minute)

	require.NoError(t, db.Create([]domain.YouTubeLiveSession{
		{VideoID: "live-recent", ChannelID: "ch-1", Status: domain.LiveStatusLive, LastSeenAt: recentSeen},
		{VideoID: "live-old", ChannelID: "ch-2", Status: domain.LiveStatusLive, LastSeenAt: oldSeen},
		{VideoID: "ended-recent", ChannelID: "ch-3", Status: domain.LiveStatusEnded, LastSeenAt: recentSeen},
		{VideoID: "outside-request", ChannelID: "ch-4", Status: domain.LiveStatusLive, LastSeenAt: recentSeen},
	}).Error)

	source := &PgYouTubeLiveSessionSource{db: db}
	channels, err := source.LoadRecentLiveChannelIDs(t.Context(), []string{"ch-1", "ch-2", "ch-3"}, now)
	require.NoError(t, err)
	assert.Equal(t, []string{"ch-1"}, channels)
}

func TestPgYouTubeLiveSessionSourceLiveRecentWindowIndependentFromCatchupWindow(t *testing.T) {
	t.Parallel()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&domain.YouTubeLiveSession{}))

	now := time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC)
	start := now.Add(-30 * time.Minute)
	lastSeenOutsideCatchup := now.Add(-(sharedconstants.LiveCatchupWindow + time.Minute))

	require.NoError(t, db.Create(domain.YouTubeLiveSession{
		VideoID:    "live-window-independent",
		ChannelID:  "ch-1",
		Status:     domain.LiveStatusLive,
		StartedAt:  &start,
		LastSeenAt: lastSeenOutsideCatchup,
	}).Error)

	source := &PgYouTubeLiveSessionSource{db: db}
	sessions, err := source.LoadRecentSessions(t.Context(), []string{"ch-1"}, now)
	require.NoError(t, err)
	require.Len(t, sessions, 1)
	assert.Equal(t, "live-window-independent", sessions[0].Stream.ID)
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
