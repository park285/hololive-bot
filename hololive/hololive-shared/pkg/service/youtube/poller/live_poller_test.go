package poller

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

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

func TestLivePollerPollUsesLiveStatusProviderAndEnqueuesLiveOutboxAfterBaseline(t *testing.T) {
	db := newBatchTestDB(t, &domain.YouTubeNotificationOutbox{})
	require.NoError(t, db.AutoMigrate(&domain.YouTubeLiveSession{}, &domain.YouTubeLiveViewerSample{}))

	scheduledAt := time.Date(2026, 5, 13, 9, 0, 0, 0, time.UTC)
	startedAt := time.Date(2026, 5, 13, 9, 30, 0, 0, time.UTC)
	viewers := 12345
	thumbnail := "https://img.test/live.jpg"
	provider := &fakeLiveStatusProvider{
		streams: []*domain.Stream{{
			ID:             "live-1",
			ChannelID:      "UC_LIVE",
			Title:          "Live One",
			Status:         domain.StreamStatusUpcoming,
			StartScheduled: &scheduledAt,
			Thumbnail:      &thumbnail,
			ViewerCount:    &viewers,
		}},
	}
	poller := NewLivePollerWithStatusProvider(provider, nil, db)

	require.NoError(t, poller.Poll(context.Background(), "UC_LIVE"))
	require.Equal(t, []string{"UC_LIVE"}, provider.channels)

	var session domain.YouTubeLiveSession
	require.NoError(t, db.First(&session, "video_id = ?", "live-1").Error)
	require.Equal(t, domain.LiveStatusUpcoming, session.Status)

	var outboxes []domain.YouTubeNotificationOutbox
	require.NoError(t, db.Find(&outboxes).Error)
	require.Empty(t, outboxes)

	provider.streams[0].Status = domain.StreamStatusLive
	provider.streams[0].StartActual = &startedAt
	require.NoError(t, poller.Poll(context.Background(), "UC_LIVE"))

	require.NoError(t, db.First(&session, "video_id = ?", "live-1").Error)
	require.Equal(t, domain.LiveStatusLive, session.Status)
	require.NotNil(t, session.StartedAt)
	require.Equal(t, startedAt, session.StartedAt.UTC())

	require.NoError(t, db.Find(&outboxes).Error)
	require.Len(t, outboxes, 1)
	require.Equal(t, domain.OutboxKindLiveStream, outboxes[0].Kind)
	require.Equal(t, domain.OutboxStatusPending, outboxes[0].Status)
	require.Equal(t, "UC_LIVE", outboxes[0].ChannelID)
	require.Equal(t, "live-1", outboxes[0].ContentID)
	require.Contains(t, outboxes[0].Payload, `"video_id":"live-1"`)
	require.Contains(t, outboxes[0].Payload, `"title":"Live One"`)

	var sample domain.YouTubeLiveViewerSample
	require.NoError(t, db.First(&sample, "video_id = ?", "live-1").Error)
	require.Equal(t, viewers, sample.ConcurrentViewers)

	require.NoError(t, poller.Poll(context.Background(), "UC_LIVE"))
	require.NoError(t, db.Find(&outboxes).Error)
	require.Len(t, outboxes, 1)
}

func TestLivePollerPollBaselineLiveDoesNotEnqueueOutbox(t *testing.T) {
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

	var outboxes []domain.YouTubeNotificationOutbox
	require.NoError(t, db.Find(&outboxes).Error)
	require.Empty(t, outboxes)
}

func TestLivePollerPollBaselineLiveEnqueuesWhenPersistedSessionWasUpcoming(t *testing.T) {
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

	var outboxes []domain.YouTubeNotificationOutbox
	require.NoError(t, db.Find(&outboxes).Error)
	require.Len(t, outboxes, 1)
	require.Equal(t, domain.OutboxKindLiveStream, outboxes[0].Kind)
	require.Equal(t, "live-after-restart", outboxes[0].ContentID)
}

func TestLivePollerPollUnseenLiveAfterBaselineEnqueuesOutbox(t *testing.T) {
	db := newBatchTestDB(t, &domain.YouTubeNotificationOutbox{})
	require.NoError(t, db.AutoMigrate(&domain.YouTubeLiveSession{}, &domain.YouTubeLiveViewerSample{}))

	provider := &fakeLiveStatusProvider{}
	poller := NewLivePollerWithStatusProvider(provider, nil, db)

	require.NoError(t, poller.Poll(context.Background(), "UC_LIVE"))

	startedAt := time.Date(2026, 5, 13, 9, 30, 0, 0, time.UTC)
	provider.streams = []*domain.Stream{{
		ID:          "live-after-baseline",
		ChannelID:   "UC_LIVE",
		Title:       "New Live",
		Status:      domain.StreamStatusLive,
		StartActual: &startedAt,
	}}

	require.NoError(t, poller.Poll(context.Background(), "UC_LIVE"))

	var outboxes []domain.YouTubeNotificationOutbox
	require.NoError(t, db.Find(&outboxes).Error)
	require.Len(t, outboxes, 1)
	require.Equal(t, "live-after-baseline", outboxes[0].ContentID)
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
