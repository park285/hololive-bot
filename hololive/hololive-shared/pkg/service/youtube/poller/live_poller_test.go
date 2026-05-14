package poller

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/kapu/hololive-shared/pkg/domain"
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
		db := newBatchTestDB(t, &domain.YouTubeNotificationOutbox{})
		require.NoError(t, db.AutoMigrate(&domain.YouTubeLiveSession{}, &domain.YouTubeLiveViewerSample{}))

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
		db := newBatchTestDB(t, &domain.YouTubeNotificationOutbox{})
		require.NoError(t, db.AutoMigrate(&domain.YouTubeLiveSession{}, &domain.YouTubeLiveViewerSample{}))

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
		db := newBatchTestDB(t, &domain.YouTubeNotificationOutbox{})
		require.NoError(t, db.AutoMigrate(&domain.YouTubeLiveSession{}, &domain.YouTubeLiveViewerSample{}))

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

func requireLiveOutboxEmpty(t *testing.T, db *gorm.DB) {
	t.Helper()

	var outboxes []domain.YouTubeNotificationOutbox
	require.NoError(t, db.Find(&outboxes).Error)
	require.Empty(t, outboxes)
}

func TestLivePollerPollPropagatesLiveStatusProviderError(t *testing.T) {
	db := newBatchTestDB(t, &domain.YouTubeNotificationOutbox{})
	require.NoError(t, db.AutoMigrate(&domain.YouTubeLiveSession{}, &domain.YouTubeLiveViewerSample{}))

	providerErr := errors.New("holodex unavailable")
	poller := NewLivePollerWithStatusProvider(&fakeLiveStatusProvider{err: providerErr}, nil, db)

	err := poller.Poll(context.Background(), "UC_LIVE")
	require.Error(t, err)
	require.ErrorIs(t, err, providerErr)
}
