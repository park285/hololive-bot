package pollers

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestLivePollerSaveLiveSessionDoesNotResurrectEndedSession(t *testing.T) {
	db := newPollerBatchTestDB(t, &domain.YouTubeNotificationOutbox{})

	endedAt := time.Date(2026, 7, 5, 11, 30, 0, 0, time.UTC)
	now := time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)
	require.NoError(t, db.Create(&domain.YouTubeLiveSession{
		VideoID:    "ended-live",
		ChannelID:  "UC_LIVE",
		Status:     domain.LiveStatusEnded,
		Title:      "Ended Live",
		EndedAt:    &endedAt,
		LastSeenAt: endedAt,
	}).Error)

	poller := NewLivePollerWithStatusProvider(nil, nil, db)
	stream := &domain.Stream{
		ID:        "ended-live",
		ChannelID: "UC_LIVE",
		Title:     "Ended Live",
		Status:    domain.StreamStatusLive,
	}

	require.NoError(t, poller.saveLiveSession(context.Background(), "UC_LIVE", stream, domain.LiveStatusLive, now))

	var session domain.YouTubeLiveSession
	require.NoError(t, db.First(&session, "video_id = ?", "ended-live").Error)
	require.Equal(t, domain.LiveStatusEnded, session.Status,
		"active-active producer의 지연 LIVE 관측이 ENDED 세션을 부활시키면 안 된다")
	require.NotNil(t, session.EndedAt)
	require.Equal(t, endedAt, session.EndedAt.UTC())
}

func TestLivePollerSaveLiveSessionDoesNotRegressLiveToUpcoming(t *testing.T) {
	db := newPollerBatchTestDB(t, &domain.YouTubeNotificationOutbox{})

	startedAt := time.Date(2026, 7, 5, 11, 0, 0, 0, time.UTC)
	now := time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)
	require.NoError(t, db.Create(&domain.YouTubeLiveSession{
		VideoID:    "live-regress",
		ChannelID:  "UC_LIVE",
		Status:     domain.LiveStatusLive,
		Title:      "Live Now",
		StartedAt:  &startedAt,
		LastSeenAt: startedAt,
	}).Error)

	poller := NewLivePollerWithStatusProvider(nil, nil, db)
	stream := &domain.Stream{
		ID:        "live-regress",
		ChannelID: "UC_LIVE",
		Title:     "Live Now",
		Status:    domain.StreamStatusUpcoming,
	}

	require.NoError(t, poller.saveLiveSession(context.Background(), "UC_LIVE", stream, domain.LiveStatusUpcoming, now))

	var session domain.YouTubeLiveSession
	require.NoError(t, db.First(&session, "video_id = ?", "live-regress").Error)
	require.Equal(t, domain.LiveStatusLive, session.Status)
	require.NotNil(t, session.StartedAt)
	require.Equal(t, startedAt, session.StartedAt.UTC())
}

func TestLivePollerSaveLiveSessionPreservesExistingStartedAt(t *testing.T) {
	db := newPollerBatchTestDB(t, &domain.YouTubeNotificationOutbox{})

	firstStartedAt := time.Date(2026, 7, 5, 11, 0, 0, 0, time.UTC)
	lateStartActual := firstStartedAt.Add(10 * time.Minute)
	now := time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)
	require.NoError(t, db.Create(&domain.YouTubeLiveSession{
		VideoID:    "started-at-keep",
		ChannelID:  "UC_LIVE",
		Status:     domain.LiveStatusLive,
		Title:      "Live",
		StartedAt:  &firstStartedAt,
		LastSeenAt: firstStartedAt,
	}).Error)

	poller := NewLivePollerWithStatusProvider(nil, nil, db)
	stream := &domain.Stream{
		ID:          "started-at-keep",
		ChannelID:   "UC_LIVE",
		Title:       "Live",
		Status:      domain.StreamStatusLive,
		StartActual: &lateStartActual,
	}

	require.NoError(t, poller.saveLiveSession(context.Background(), "UC_LIVE", stream, domain.LiveStatusLive, now))

	var session domain.YouTubeLiveSession
	require.NoError(t, db.First(&session, "video_id = ?", "started-at-keep").Error)
	require.NotNil(t, session.StartedAt)
	require.Equal(t, firstStartedAt, session.StartedAt.UTC())
}

func TestLivePollerSaveLiveSessionKeepsMaxLastSeenAt(t *testing.T) {
	db := newPollerBatchTestDB(t, &domain.YouTubeNotificationOutbox{})

	freshSeenAt := time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)
	staleObservation := freshSeenAt.Add(-5 * time.Minute)
	require.NoError(t, db.Create(&domain.YouTubeLiveSession{
		VideoID:    "last-seen-max",
		ChannelID:  "UC_LIVE",
		Status:     domain.LiveStatusLive,
		Title:      "Live",
		LastSeenAt: freshSeenAt,
	}).Error)

	poller := NewLivePollerWithStatusProvider(nil, nil, db)
	stream := &domain.Stream{
		ID:        "last-seen-max",
		ChannelID: "UC_LIVE",
		Title:     "Live",
		Status:    domain.StreamStatusLive,
	}

	require.NoError(t, poller.saveLiveSession(context.Background(), "UC_LIVE", stream, domain.LiveStatusLive, staleObservation))

	var session domain.YouTubeLiveSession
	require.NoError(t, db.First(&session, "video_id = ?", "last-seen-max").Error)
	require.Equal(t, freshSeenAt, session.LastSeenAt.UTC(),
		"지연 도착한 과거 관측이 last_seen_at을 되감으면 안 된다(GREATEST)")
}

func TestLivePollerMarkSessionEndedOnlyEndsLiveSessions(t *testing.T) {
	db := newPollerBatchTestDB(t, &domain.YouTubeNotificationOutbox{})

	now := time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)
	require.NoError(t, db.Create(&domain.YouTubeLiveSession{
		VideoID:    "upcoming-not-ended",
		ChannelID:  "UC_LIVE",
		Status:     domain.LiveStatusUpcoming,
		Title:      "Upcoming",
		LastSeenAt: now.Add(-time.Hour),
	}).Error)

	poller := NewLivePollerWithStatusProvider(nil, nil, db)
	ended := poller.markSessionEnded(context.Background(), "upcoming-not-ended", now)

	require.False(t, ended, "LIVE가 아닌 세션은 markSessionEnded 대상이 아니다")
	var session domain.YouTubeLiveSession
	require.NoError(t, db.First(&session, "video_id = ?", "upcoming-not-ended").Error)
	require.Equal(t, domain.LiveStatusUpcoming, session.Status)
	require.Nil(t, session.EndedAt)
}
