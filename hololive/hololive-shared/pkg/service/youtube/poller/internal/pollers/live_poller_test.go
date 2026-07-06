package pollers

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/livestatus"
)

type fakeLiveStatusProvider struct {
	streams  []*domain.Stream
	channels []string
	err      error
}

func (p *fakeLiveStatusProvider) GetChannelsLiveStatus(_ context.Context, channelIDs []string) ([]*domain.Stream, error) {
	p.channels = append([]string(nil), channelIDs...)
	if p.err != nil {
		return nil, p.err
	}
	return p.streams, nil
}

func TestLivePollerNeverEnqueuesLiveStreamOutbox(t *testing.T) {
	t.Run("baseline 이후 live 전환", func(t *testing.T) {
		db := newPollerBatchTestDB(t, &domain.YouTubeNotificationOutbox{})

		provider := &fakeLiveStatusProvider{}
		poller := NewLivePollerWithStatusProvider(provider, nil, db)

		require.NoError(t, poller.Poll(context.Background(), "UC_LIVE"))
		require.Equal(t, []string{"UC_LIVE"}, provider.channels)
		requireLiveOutboxEmpty(t, db)

		startedAt := time.Date(2026, 5, 13, 9, 30, 0, 0, time.UTC)
		viewers := 12345
		provider.streams = []*domain.Stream{{
			ID:          "live-after-baseline",
			ChannelID:   "UC_LIVE",
			Title:       "New Live",
			Status:      domain.StreamStatusLive,
			StartActual: &startedAt,
			ViewerCount: &viewers,
		}}

		require.NoError(t, poller.Poll(context.Background(), "UC_LIVE"))

		var session domain.YouTubeLiveSession
		require.NoError(t, db.First(&session, "video_id = ?", "live-after-baseline").Error)
		require.Equal(t, domain.LiveStatusLive, session.Status)
		require.NotNil(t, session.StartedAt)
		require.Equal(t, startedAt, session.StartedAt.UTC())
		require.NotNil(t, session.LiveFirstSeenAt)
		firstSeenAt := session.LiveFirstSeenAt.UTC()

		var sample domain.YouTubeLiveViewerSample
		require.NoError(t, db.First(&sample, "video_id = ?", "live-after-baseline").Error)
		require.Equal(t, viewers, sample.ConcurrentViewers)
		requireLiveOutboxEmpty(t, db)

		provider.streams[0].Title = "Updated Live"
		require.NoError(t, poller.Poll(context.Background(), "UC_LIVE"))
		require.NoError(t, db.First(&session, "video_id = ?", "live-after-baseline").Error)
		require.NotNil(t, session.LiveFirstSeenAt)
		require.Equal(t, firstSeenAt, session.LiveFirstSeenAt.UTC())
	})

	t.Run("baseline 중 이미 live", func(t *testing.T) {
		db := newPollerBatchTestDB(t, &domain.YouTubeNotificationOutbox{})

		startedAt := time.Date(2026, 5, 13, 9, 30, 0, 0, time.UTC)
		provider := &fakeLiveStatusProvider{
			streams: []*domain.Stream{{
				ID:          "live-baseline",
				ChannelID:   "UC_LIVE",
				Title:       "Already Live",
				Status:      domain.StreamStatusLive,
				StartActual: &startedAt,
			}},
		}
		poller := NewLivePollerWithStatusProvider(provider, nil, db)

		require.NoError(t, poller.Poll(context.Background(), "UC_LIVE"))

		var session domain.YouTubeLiveSession
		require.NoError(t, db.First(&session, "video_id = ?", "live-baseline").Error)
		require.Equal(t, domain.LiveStatusLive, session.Status)
		requireLiveOutboxEmpty(t, db)
	})

	t.Run("persisted upcoming to live", func(t *testing.T) {
		db := newPollerBatchTestDB(t, &domain.YouTubeNotificationOutbox{})

		scheduledAt := time.Date(2026, 5, 13, 9, 0, 0, 0, time.UTC)
		require.NoError(t, db.Create(&domain.YouTubeLiveSession{
			VideoID:            "live-after-restart",
			ChannelID:          "UC_LIVE",
			Status:             domain.LiveStatusUpcoming,
			Title:              "Persisted Upcoming",
			ScheduledStartTime: &scheduledAt,
		}).Error)

		startedAt := time.Date(2026, 5, 13, 9, 30, 0, 0, time.UTC)
		provider := &fakeLiveStatusProvider{
			streams: []*domain.Stream{{
				ID:             "live-after-restart",
				ChannelID:      "UC_LIVE",
				Title:          "Persisted Upcoming",
				Status:         domain.StreamStatusLive,
				StartScheduled: &scheduledAt,
				StartActual:    &startedAt,
			}},
		}
		poller := NewLivePollerWithStatusProvider(provider, nil, db)

		require.NoError(t, poller.Poll(context.Background(), "UC_LIVE"))
		var session domain.YouTubeLiveSession
		require.NoError(t, db.First(&session, "video_id = ?", "live-after-restart").Error)
		require.NotNil(t, session.LiveFirstSeenAt)
		requireLiveOutboxEmpty(t, db)
	})
}

func TestLivePollerSaveLiveSessionPreservesExistingLiveFirstSeenAtOnConflict(t *testing.T) {
	db := newPollerBatchTestDB(t, &domain.YouTubeNotificationOutbox{})

	firstSeenAt := time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC)
	laterSeenAt := firstSeenAt.Add(45 * time.Second)
	require.NoError(t, db.Create(&domain.YouTubeLiveSession{
		VideoID:         "race-live",
		ChannelID:       "UC_LIVE",
		Status:          domain.LiveStatusLive,
		Title:           "Racing Live",
		LiveFirstSeenAt: &firstSeenAt,
		LastSeenAt:      firstSeenAt,
	}).Error)

	poller := NewLivePollerWithStatusProvider(nil, nil, db)
	stream := &domain.Stream{
		ID:        "race-live",
		ChannelID: "UC_LIVE",
		Title:     "Racing Live Updated",
		Status:    domain.StreamStatusLive,
	}

	require.NoError(t, poller.saveLiveSession(context.Background(), "UC_LIVE", stream, domain.LiveStatusLive, laterSeenAt))

	var session domain.YouTubeLiveSession
	require.NoError(t, db.First(&session, "video_id = ?", "race-live").Error)
	require.NotNil(t, session.LiveFirstSeenAt)
	require.Equal(t, firstSeenAt, session.LiveFirstSeenAt.UTC())
}

func TestLivePollerSaveLiveSessionPreservesExistingMetadataOnEmptyObservation(t *testing.T) {
	db := newPollerBatchTestDB(t, &domain.YouTubeNotificationOutbox{})

	firstSeenAt := time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)
	require.NoError(t, db.Create(&domain.YouTubeLiveSession{
		VideoID:      "metadata-live",
		ChannelID:    "UC_LIVE",
		Status:       domain.LiveStatusLive,
		Title:        "Metadata Live",
		TopicID:      "Rhythm_Heaven",
		ThumbnailURL: "https://i.ytimg.com/vi/metadata-live/maxresdefault.jpg",
		LastSeenAt:   firstSeenAt,
	}).Error)

	poller := NewLivePollerWithStatusProvider(nil, nil, db)
	stream := &domain.Stream{
		ID:        "metadata-live",
		ChannelID: "UC_LIVE",
		Title:     "Metadata Live Updated",
		Status:    domain.StreamStatusLive,
	}

	require.NoError(t, poller.saveLiveSession(context.Background(), "UC_LIVE", stream, domain.LiveStatusLive, firstSeenAt.Add(time.Minute)))

	var session domain.YouTubeLiveSession
	require.NoError(t, db.First(&session, "video_id = ?", "metadata-live").Error)
	require.Equal(t, "Rhythm_Heaven", session.TopicID)
	require.Equal(t, "https://i.ytimg.com/vi/metadata-live/maxresdefault.jpg", session.ThumbnailURL)
}

func TestLivePollerSaveLiveSessionStoresNewMetadataOnObservation(t *testing.T) {
	db := newPollerBatchTestDB(t, &domain.YouTubeNotificationOutbox{})

	now := time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)
	topicID := "Overwatch"
	thumbnailURL := "https://i.ytimg.com/vi/new-metadata/maxresdefault.jpg"
	poller := NewLivePollerWithStatusProvider(nil, nil, db)
	stream := &domain.Stream{
		ID:        "new-metadata",
		ChannelID: "UC_LIVE",
		Title:     "New Metadata Live",
		Status:    domain.StreamStatusLive,
		TopicID:   &topicID,
		Thumbnail: &thumbnailURL,
	}

	require.NoError(t, poller.saveLiveSession(context.Background(), "UC_LIVE", stream, domain.LiveStatusLive, now))

	var session domain.YouTubeLiveSession
	require.NoError(t, db.First(&session, "video_id = ?", "new-metadata").Error)
	require.Equal(t, topicID, session.TopicID)
	require.Equal(t, thumbnailURL, session.ThumbnailURL)
}

func TestLivePollerSaveLiveSessionClearsEndedAtOnLiveObservation(t *testing.T) {
	db := newPollerBatchTestDB(t, &domain.YouTubeNotificationOutbox{})

	endedAt := time.Date(2026, 7, 5, 11, 30, 0, 0, time.UTC)
	now := time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)
	require.NoError(t, db.Create(&domain.YouTubeLiveSession{
		VideoID:    "stale-ended-live",
		ChannelID:  "UC_LIVE",
		Status:     domain.LiveStatusEnded,
		Title:      "Stale Ended Live",
		EndedAt:    &endedAt,
		LastSeenAt: endedAt,
	}).Error)

	poller := NewLivePollerWithStatusProvider(nil, nil, db)
	stream := &domain.Stream{
		ID:        "stale-ended-live",
		ChannelID: "UC_LIVE",
		Title:     "Stale Ended Live",
		Status:    domain.StreamStatusLive,
	}

	require.NoError(t, poller.saveLiveSession(context.Background(), "UC_LIVE", stream, domain.LiveStatusLive, now))

	var session domain.YouTubeLiveSession
	require.NoError(t, db.First(&session, "video_id = ?", "stale-ended-live").Error)
	require.Equal(t, domain.LiveStatusLive, session.Status)
	require.Nil(t, session.EndedAt)
}

func requireLiveOutboxEmpty(t *testing.T, db *pollerBatchTestDB) {
	t.Helper()

	var outboxes []domain.YouTubeNotificationOutbox
	require.NoError(t, db.Find(&outboxes).Error)
	require.Empty(t, outboxes)
}

func TestLivePollerPollPropagatesLiveStatusProviderError(t *testing.T) {
	db := newPollerBatchTestDB(t, &domain.YouTubeNotificationOutbox{})

	providerErr := errors.New("holodex unavailable")
	poller := NewLivePollerWithStatusProvider(&fakeLiveStatusProvider{err: providerErr}, nil, db)

	err := poller.Poll(context.Background(), "UC_LIVE")
	require.Error(t, err)
	require.ErrorIs(t, err, providerErr)
}

func TestLivePollerPollUsesDetailedProviderForDeferredSoftSkip(t *testing.T) {
	db := newPollerBatchTestDB(t, &domain.YouTubeNotificationOutbox{})
	provider := &fakeLiveStatusWithFailuresProvider{
		failures: map[string]error{
			"UC_LIVE": livestatus.NewDeferred(livestatus.DeferredReasonPerCycleCap, "UC_LIVE", nil),
		},
	}
	poller := NewLivePollerWithStatusProvider(provider, nil, db)

	err := poller.Poll(context.Background(), "UC_LIVE")

	require.NoError(t, err)
	require.Equal(t, []string{"UC_LIVE"}, provider.channels)

	var sessions []domain.YouTubeLiveSession
	require.NoError(t, db.Find(&sessions).Error)
	require.Empty(t, sessions)
}
