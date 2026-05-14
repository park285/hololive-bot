package checker

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

func TestPgYouTubeLiveSessionSourceLoadRecentSessionsAndDispatchLookup(t *testing.T) {
	t.Parallel()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&domain.YouTubeLiveSession{}, &testAlarmDispatchEvent{}))

	now := time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC)
	liveStart := now.Add(-30 * time.Minute)
	upcomingStart := now.Add(4 * time.Minute)
	oldSeen := now.Add(-(sharedconstants.LiveCatchupWindow + time.Second))
	recentSeen := now.Add(-time.Minute)

	require.NoError(t, db.Create([]domain.YouTubeLiveSession{
		{
			VideoID:    "live-included",
			ChannelID:  "ch-1",
			Status:     domain.LiveStatusLive,
			Title:      " live title ",
			StartedAt:  &liveStart,
			LastSeenAt: recentSeen,
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
	require.NotNil(t, sessionsByID(sessions)["live-included"].Stream.Link)
	assert.Equal(t, "https://youtube.com/watch?v=live-included", *sessionsByID(sessions)["live-included"].Stream.Link)

	require.NoError(t, db.Create([]testAlarmDispatchEvent{
		{AlarmType: string(domain.AlarmTypeLive), StreamID: "live-included", CreatedAt: now.Add(-time.Hour)},
		{AlarmType: string(domain.AlarmTypeCommunity), StreamID: "upcoming-included", CreatedAt: now.Add(-time.Hour)},
		{AlarmType: string(domain.AlarmTypeLive), StreamID: "old-dispatch", CreatedAt: now.Add(-25 * time.Hour)},
	}).Error)

	dispatched, err := source.RecentlyDispatchedStreamIDs(t.Context(), []string{"live-included", "upcoming-included", "old-dispatch"}, now.Add(-24*time.Hour))
	require.NoError(t, err)
	assert.Contains(t, dispatched, "live-included")
	assert.NotContains(t, dispatched, "upcoming-included")
	assert.NotContains(t, dispatched, "old-dispatch")
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
