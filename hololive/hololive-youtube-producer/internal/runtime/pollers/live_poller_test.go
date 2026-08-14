package pollers

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/livestatus"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping/parser"
)

type fakeLiveStatusProvider struct {
	streams  []*domain.Stream
	channels []string
	err      error
}

type fakeWatchLiveMetadataClient struct {
	metadata parser.WatchLiveMetadata
	errors   []error
	calls    []string
}

func (c *fakeWatchLiveMetadataClient) GetWatchLiveMetadata(_ context.Context, channelID, videoID string) (parser.WatchLiveMetadata, error) {
	c.calls = append(c.calls, channelID+"/"+videoID)
	if len(c.errors) > 0 {
		err := c.errors[0]
		c.errors = c.errors[1:]
		if err != nil {
			return parser.WatchLiveMetadata{}, err
		}
	}
	return c.metadata, nil
}

type sequenceWatchLiveMetadataClient struct {
	results []parser.WatchLiveMetadata
	calls   []string
}

func (c *sequenceWatchLiveMetadataClient) GetWatchLiveMetadata(_ context.Context, channelID, videoID string) (parser.WatchLiveMetadata, error) {
	c.calls = append(c.calls, channelID+"/"+videoID)
	result := c.results[0]
	c.results = c.results[1:]
	return result, nil
}

type blockingWatchLiveMetadataClient struct {
	entered chan struct{}
	release chan struct{}

	mu    sync.Mutex
	calls int
}

func (c *blockingWatchLiveMetadataClient) GetWatchLiveMetadata(_ context.Context, _, _ string) (parser.WatchLiveMetadata, error) {
	c.mu.Lock()
	c.calls++
	c.mu.Unlock()
	close(c.entered)
	<-c.release
	return parser.WatchLiveMetadata{LiveContent: parser.LiveContentFalse}, nil
}

func (c *blockingWatchLiveMetadataClient) callCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

func (p *fakeLiveStatusProvider) GetChannelsLiveStatus(_ context.Context, channelIDs []string) ([]*domain.Stream, error) {
	p.channels = append([]string(nil), channelIDs...)
	if p.err != nil {
		return nil, p.err
	}
	return p.streams, nil
}

func TestLivePollerPollFailsClosedWithoutHolodexProvider(t *testing.T) {
	db := newPollerBatchTestDB(t, &domain.YouTubeNotificationOutbox{})
	poller := NewLivePollerWithStatusProvider(nil, nil, db)
	err := poller.Poll(context.Background(), "UC_LIVE")
	require.Error(t, err)
	require.ErrorContains(t, err, "Holodex live status provider")
	require.NotEmpty(t, poller.PollBatch(context.Background(), []string{"UC_LIVE"}))
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

	require.NoError(t, poller.saveLiveSessionWithPremiere(context.Background(), "UC_LIVE", stream, domain.LiveStatusLive, laterSeenAt, premiereFromStream(stream)))

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

	require.NoError(t, poller.saveLiveSessionWithPremiere(context.Background(), "UC_LIVE", stream, domain.LiveStatusLive, firstSeenAt.Add(time.Minute), premiereFromStream(stream)))

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

	require.NoError(t, poller.saveLiveSessionWithPremiere(context.Background(), "UC_LIVE", stream, domain.LiveStatusLive, now, premiereFromStream(stream)))

	var session domain.YouTubeLiveSession
	require.NoError(t, db.First(&session, "video_id = ?", "new-metadata").Error)
	require.Equal(t, topicID, session.TopicID)
	require.Equal(t, thumbnailURL, session.ThumbnailURL)
}

func TestLivePollerProbesPremiereOnceAndBackfillsScheduledStart(t *testing.T) {
	db := newPollerBatchTestDB(t, &domain.YouTubeNotificationOutbox{})
	startTimestamp := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	provider := &fakeLiveStatusProvider{streams: []*domain.Stream{{
		ID:        "premiere-upcoming",
		ChannelID: "UC_PREMIERE",
		Title:     "Premiere Upcoming",
		Status:    domain.StreamStatusUpcoming,
	}}}
	client := &fakeWatchLiveMetadataClient{metadata: parser.WatchLiveMetadata{
		LiveContent:    parser.LiveContentFalse,
		StartTimestamp: &startTimestamp,
	}}
	poller := newLivePollerWithMetadataClient(provider, client, db)

	require.NoError(t, poller.Poll(context.Background(), "UC_PREMIERE"))
	require.NoError(t, poller.Poll(context.Background(), "UC_PREMIERE"))
	require.Equal(t, []string{"UC_PREMIERE/premiere-upcoming"}, client.calls)

	var session domain.YouTubeLiveSession
	require.NoError(t, db.First(&session, "video_id = ?", "premiere-upcoming").Error)
	require.NotNil(t, session.IsPremiere)
	require.True(t, *session.IsPremiere)
	require.NotNil(t, session.ScheduledStartTime)
	require.Equal(t, startTimestamp, session.ScheduledStartTime.UTC())
}

func TestLivePollerKeepsProbeBackfillAcrossFreshStreamInstances(t *testing.T) {
	db := newPollerBatchTestDB(t, &domain.YouTubeNotificationOutbox{})
	startTimestamp := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	client := &fakeWatchLiveMetadataClient{metadata: parser.WatchLiveMetadata{
		LiveContent:    parser.LiveContentFalse,
		StartTimestamp: &startTimestamp,
	}}
	poller := newLivePollerWithMetadataClient(nil, client, db)

	first := &domain.Stream{
		ID:        "premiere-fresh",
		ChannelID: "UC_PREMIERE",
		Title:     "Premiere",
		Status:    domain.StreamStatusUpcoming,
	}
	second := &domain.Stream{
		ID:        first.ID,
		ChannelID: first.ChannelID,
		Title:     first.Title,
		Status:    first.Status,
	}

	require.NoError(t, poller.pollStream(context.Background(), first.ChannelID, first, startTimestamp))
	require.NoError(t, poller.pollStream(context.Background(), second.ChannelID, second, startTimestamp.Add(time.Minute)))
	require.Equal(t, []string{"UC_PREMIERE/premiere-fresh"}, client.calls)
	require.True(t, second.IsPremiere)
	require.NotNil(t, second.StartScheduled)
	require.Equal(t, startTimestamp, second.StartScheduled.UTC())
}

func TestLivePollerRetriesFailedPremiereProbe(t *testing.T) {
	db := newPollerBatchTestDB(t, &domain.YouTubeNotificationOutbox{})
	provider := &fakeLiveStatusProvider{streams: []*domain.Stream{{
		ID:        "probe-retry",
		ChannelID: "UC_RETRY",
		Title:     "Probe Retry",
		Status:    domain.StreamStatusUpcoming,
	}}}
	client := &fakeWatchLiveMetadataClient{
		metadata: parser.WatchLiveMetadata{LiveContent: parser.LiveContentTrue},
		errors:   []error{errors.New("watch page unavailable"), nil},
	}
	poller := newLivePollerWithMetadataClient(provider, client, db)

	require.NoError(t, poller.Poll(context.Background(), "UC_RETRY"))
	var session domain.YouTubeLiveSession
	require.NoError(t, db.First(&session, "video_id = ?", "probe-retry").Error)
	require.Nil(t, session.IsPremiere)

	require.NoError(t, poller.Poll(context.Background(), "UC_RETRY"))
	require.Equal(t, []string{"UC_RETRY/probe-retry", "UC_RETRY/probe-retry"}, client.calls)
	require.NoError(t, db.First(&session, "video_id = ?", "probe-retry").Error)
	require.NotNil(t, session.IsPremiere)
	require.False(t, *session.IsPremiere)
}

func TestLivePollerRetriesUnknownPremiereProbe(t *testing.T) {
	db := newPollerBatchTestDB(t, &domain.YouTubeNotificationOutbox{})
	provider := &fakeLiveStatusProvider{streams: []*domain.Stream{{
		ID:        "unknown-retry",
		ChannelID: "UC_UNKNOWN",
		Title:     "Unknown Retry",
		Status:    domain.StreamStatusUpcoming,
	}}}
	client := &sequenceWatchLiveMetadataClient{results: []parser.WatchLiveMetadata{
		{LiveContent: parser.LiveContentUnknown},
		{LiveContent: parser.LiveContentFalse},
	}}
	poller := newLivePollerWithMetadataClient(provider, client, db)

	require.NoError(t, poller.Poll(context.Background(), "UC_UNKNOWN"))
	var session domain.YouTubeLiveSession
	require.NoError(t, db.First(&session, "video_id = ?", "unknown-retry").Error)
	require.Nil(t, session.IsPremiere)

	require.NoError(t, poller.Poll(context.Background(), "UC_UNKNOWN"))
	require.Equal(t, []string{"UC_UNKNOWN/unknown-retry", "UC_UNKNOWN/unknown-retry"}, client.calls)
	require.NoError(t, db.First(&session, "video_id = ?", "unknown-retry").Error)
	require.NotNil(t, session.IsPremiere)
	require.True(t, *session.IsPremiere)
}

func TestLivePollerConcurrentProbePersistsNonNullDecision(t *testing.T) {
	db := newPollerBatchTestDB(t, &domain.YouTubeNotificationOutbox{})
	client := &blockingWatchLiveMetadataClient{
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
	poller := newLivePollerWithMetadataClient(nil, client, db)
	now := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	newStream := func() *domain.Stream {
		return &domain.Stream{
			ID:        "concurrent-probe",
			ChannelID: "UC_CONCURRENT",
			Title:     "Concurrent Probe",
			Status:    domain.StreamStatusUpcoming,
		}
	}

	firstDone := make(chan error, 1)
	go func() {
		firstDone <- poller.pollStream(context.Background(), "UC_CONCURRENT", newStream(), now)
	}()
	<-client.entered

	secondDone := make(chan error, 1)
	go func() {
		secondDone <- poller.pollStream(context.Background(), "UC_CONCURRENT", newStream(), now.Add(time.Second))
	}()
	require.NoError(t, <-secondDone)

	var session domain.YouTubeLiveSession
	require.NoError(t, db.First(&session, "video_id = ?", "concurrent-probe").Error)
	require.Nil(t, session.IsPremiere)

	close(client.release)
	require.NoError(t, <-firstDone)
	require.Equal(t, 1, client.callCount())
	require.NoError(t, db.First(&session, "video_id = ?", "concurrent-probe").Error)
	require.NotNil(t, session.IsPremiere)
	require.True(t, *session.IsPremiere)
}

func TestLivePollerUsesStreamPremiereFallbackWithoutProbeClient(t *testing.T) {
	db := newPollerBatchTestDB(t, &domain.YouTubeNotificationOutbox{})
	poller := NewLivePollerWithStatusProvider(nil, nil, db)
	stream := &domain.Stream{
		ID:         "fallback-premiere",
		ChannelID:  "UC_FALLBACK",
		Title:      "Fallback Premiere",
		Status:     domain.StreamStatusUpcoming,
		IsPremiere: true,
	}

	require.NoError(t, poller.pollStream(context.Background(), stream.ChannelID, stream, time.Now()))

	var session domain.YouTubeLiveSession
	require.NoError(t, db.First(&session, "video_id = ?", stream.ID).Error)
	require.NotNil(t, session.IsPremiere)
	require.True(t, *session.IsPremiere)
}

func TestLivePollerProbePreservesExistingScheduledStart(t *testing.T) {
	db := newPollerBatchTestDB(t, &domain.YouTubeNotificationOutbox{})
	existingStart := time.Date(2026, 8, 2, 11, 30, 0, 0, time.UTC)
	probeStart := existingStart.Add(30 * time.Minute)
	client := &fakeWatchLiveMetadataClient{metadata: parser.WatchLiveMetadata{
		LiveContent:    parser.LiveContentFalse,
		StartTimestamp: &probeStart,
	}}
	poller := newLivePollerWithMetadataClient(nil, client, db)
	stream := &domain.Stream{
		ID:             "preserve-schedule",
		ChannelID:      "UC_SCHEDULE",
		Title:          "Preserve Schedule",
		Status:         domain.StreamStatusUpcoming,
		StartScheduled: &existingStart,
	}

	require.NoError(t, poller.pollStream(context.Background(), stream.ChannelID, stream, probeStart))

	var session domain.YouTubeLiveSession
	require.NoError(t, db.First(&session, "video_id = ?", stream.ID).Error)
	require.NotNil(t, session.ScheduledStartTime)
	require.Equal(t, existingStart, session.ScheduledStartTime.UTC())
}

func TestLivePollerWarmsPremiereProbeFromExistingSession(t *testing.T) {
	db := newPollerBatchTestDB(t, &domain.YouTubeNotificationOutbox{})
	isPremiere := true
	now := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	require.NoError(t, db.Create(&domain.YouTubeLiveSession{
		VideoID:    "known-premiere",
		ChannelID:  "UC_KNOWN",
		Status:     domain.LiveStatusUpcoming,
		Title:      "Known Premiere",
		IsPremiere: &isPremiere,
		LastSeenAt: now,
	}).Error)
	provider := &fakeLiveStatusProvider{streams: []*domain.Stream{{
		ID:        "known-premiere",
		ChannelID: "UC_KNOWN",
		Title:     "Known Premiere",
		Status:    domain.StreamStatusUpcoming,
	}}}
	client := &fakeWatchLiveMetadataClient{metadata: parser.WatchLiveMetadata{LiveContent: parser.LiveContentTrue}}
	poller := newLivePollerWithMetadataClient(provider, client, db)

	require.NoError(t, poller.Poll(context.Background(), "UC_KNOWN"))
	require.Empty(t, client.calls)

	var session domain.YouTubeLiveSession
	require.NoError(t, db.First(&session, "video_id = ?", "known-premiere").Error)
	require.NotNil(t, session.IsPremiere)
	require.True(t, *session.IsPremiere)
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
