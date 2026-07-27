package pollers

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestMarkEndedSessionsFencePreservesConcurrentlyObservedSession(t *testing.T) {
	db := newPollerBatchTestDB(t, &domain.YouTubeNotificationOutbox{})

	pollStartedAt := time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC)
	peerObservedAt := pollStartedAt.Add(5 * time.Second)
	require.NoError(t, db.Create(&domain.YouTubeLiveSession{
		VideoID:    "fresh-live",
		ChannelID:  "UC_FENCE",
		Status:     domain.LiveStatusLive,
		Title:      "Concurrently Observed Live",
		LastSeenAt: peerObservedAt,
	}).Error)

	poller := NewLivePollerWithStatusProvider(nil, nil, db)
	poller.markEndedSessions(context.Background(), "UC_FENCE", []*domain.Stream{}, pollStartedAt)

	var session domain.YouTubeLiveSession
	require.NoError(t, db.First(&session, "video_id = ?", "fresh-live").Error)
	require.Equal(t, domain.LiveStatusLive, session.Status,
		"poll 시작 이후 다른 poller가 LIVE로 관측한 세션은 stale 목록-부재로 종료되면 안 된다")
	require.Nil(t, session.EndedAt)
	require.Equal(t, peerObservedAt, session.LastSeenAt.UTC())
}

func TestMarkEndedSessionsEndsGenuinelyDisappearedSession(t *testing.T) {
	db := newPollerBatchTestDB(t, &domain.YouTubeNotificationOutbox{})

	pollStartedAt := time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC)
	lastObservedAt := pollStartedAt.Add(-(liveSessionLastSeenMinAdvance + time.Minute))
	require.NoError(t, db.Create(&domain.YouTubeLiveSession{
		VideoID:    "gone-live",
		ChannelID:  "UC_FENCE",
		Status:     domain.LiveStatusLive,
		Title:      "Genuinely Ended Live",
		LastSeenAt: lastObservedAt,
	}).Error)

	poller := NewLivePollerWithStatusProvider(nil, nil, db)
	poller.markEndedSessions(context.Background(), "UC_FENCE", []*domain.Stream{}, pollStartedAt)

	var session domain.YouTubeLiveSession
	require.NoError(t, db.First(&session, "video_id = ?", "gone-live").Error)
	require.Equal(t, domain.LiveStatusEnded, session.Status,
		"fence 시각 이전에 마지막으로 관측된 실종 스트림은 계속 ENDED 되어야 한다")
	require.NotNil(t, session.EndedAt)
}

func TestMarkEndedSessionsEndsSessionLastSeenAtFenceBoundary(t *testing.T) {
	db := newPollerBatchTestDB(t, &domain.YouTubeNotificationOutbox{})

	pollStartedAt := time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC)
	require.NoError(t, db.Create(&domain.YouTubeLiveSession{
		VideoID:    "boundary-live",
		ChannelID:  "UC_FENCE",
		Status:     domain.LiveStatusLive,
		Title:      "Boundary Live",
		LastSeenAt: pollStartedAt.Add(-liveSessionLastSeenMinAdvance),
	}).Error)

	poller := NewLivePollerWithStatusProvider(nil, nil, db)
	poller.markEndedSessions(context.Background(), "UC_FENCE", []*domain.Stream{}, pollStartedAt)

	var session domain.YouTubeLiveSession
	require.NoError(t, db.First(&session, "video_id = ?", "boundary-live").Error)
	require.Equal(t, domain.LiveStatusEnded, session.Status,
		"last_seen_at == poll 시작 - MinAdvance 시각은 fence 경계(<=)에 포함되어 ENDED 되어야 한다")
	require.NotNil(t, session.EndedAt)
}

func TestMarkEndedSessionsKeepsSessionInsideMinAdvanceSlack(t *testing.T) {
	db := newPollerBatchTestDB(t, &domain.YouTubeNotificationOutbox{})

	pollStartedAt := time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC)
	require.NoError(t, db.Create(&domain.YouTubeLiveSession{
		VideoID:    "slack-live",
		ChannelID:  "UC_FENCE",
		Status:     domain.LiveStatusLive,
		Title:      "Recently Observed Live",
		LastSeenAt: pollStartedAt.Add(-liveSessionLastSeenMinAdvance + time.Second),
	}).Error)

	poller := NewLivePollerWithStatusProvider(nil, nil, db)
	poller.markEndedSessions(context.Background(), "UC_FENCE", []*domain.Stream{}, pollStartedAt)

	var session domain.YouTubeLiveSession
	require.NoError(t, db.First(&session, "video_id = ?", "slack-live").Error)
	require.Equal(t, domain.LiveStatusLive, session.Status,
		"skip 가드로 last_seen_at 기록이 MinAdvance만큼 늦을 수 있으므로 그 폭 안의 세션은 종료하면 안 된다")
	require.Nil(t, session.EndedAt)
}
