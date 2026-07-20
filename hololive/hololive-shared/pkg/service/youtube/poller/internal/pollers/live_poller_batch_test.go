package pollers

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/internal/service/youtube/livestatus"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestLivePollerPollBatchUsesSingleProviderCallAndPersistsPerChannel(t *testing.T) {
	db := newPollerBatchTestDB(t, &domain.YouTubeNotificationOutbox{})
	startedAt := time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC)
	provider := &fakeLiveStatusProvider{
		streams: []*domain.Stream{
			{
				ID:          "live-a",
				ChannelID:   "UC_A",
				Title:       "A",
				Status:      domain.StreamStatusLive,
				StartActual: &startedAt,
			},
			{
				ID:        "upcoming-b",
				ChannelID: "UC_B",
				Title:     "B",
				Status:    domain.StreamStatusUpcoming,
			},
		},
	}
	poller := NewLivePollerWithStatusProvider(provider, nil, db)

	errs := poller.PollBatch(context.Background(), []string{"UC_A", "UC_B", "UC_A"})

	require.Empty(t, errs)
	require.Equal(t, []string{"UC_A", "UC_B"}, provider.channels)

	var sessions []domain.YouTubeLiveSession
	require.NoError(t, db.Order("video_id").Find(&sessions).Error)
	require.Len(t, sessions, 2)
	require.Equal(t, "live-a", sessions[0].VideoID)
	require.Equal(t, "upcoming-b", sessions[1].VideoID)
	requireLiveOutboxEmpty(t, db)
}

type fakeLiveStatusWithFailuresProvider struct {
	streams  []*domain.Stream
	failures map[string]error
	channels []string
}

func (p *fakeLiveStatusWithFailuresProvider) GetChannelsLiveStatus(_ context.Context, channelIDs []string) ([]*domain.Stream, error) {
	p.channels = append([]string(nil), channelIDs...)
	return p.streams, nil
}

func (p *fakeLiveStatusWithFailuresProvider) GetChannelsLiveStatusWithFailures(_ context.Context, channelIDs []string) ([]*domain.Stream, map[string]error, error) {
	p.channels = append([]string(nil), channelIDs...)
	return p.streams, p.failures, nil
}

func TestLivePollerPollBatchKeepsSessionsForFetchFailedChannels(t *testing.T) {
	db := newPollerBatchTestDB(t, &domain.YouTubeNotificationOutbox{})
	startedAt := time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC)
	provider := &fakeLiveStatusWithFailuresProvider{
		streams: []*domain.Stream{
			{
				ID:          "live-b",
				ChannelID:   "UC_B",
				Title:       "B",
				Status:      domain.StreamStatusLive,
				StartActual: &startedAt,
			},
		},
	}
	poller := NewLivePollerWithStatusProvider(provider, nil, db)

	errs := poller.PollBatch(context.Background(), []string{"UC_A", "UC_B"})
	require.Empty(t, errs)

	var session domain.YouTubeLiveSession
	require.NoError(t, db.First(&session, "video_id = ?", "live-b").Error)
	require.Equal(t, domain.LiveStatusLive, session.Status)

	fetchErr := errors.New("transient fetch failure")
	provider.streams = []*domain.Stream{
		{
			ID:          "live-a",
			ChannelID:   "UC_A",
			Title:       "A",
			Status:      domain.StreamStatusLive,
			StartActual: &startedAt,
		},
	}
	provider.failures = map[string]error{"UC_B": fetchErr}

	errs = poller.PollBatch(context.Background(), []string{"UC_A", "UC_B"})

	require.Len(t, errs, 1)
	require.ErrorIs(t, errs["UC_B"], fetchErr)

	require.NoError(t, db.First(&session, "video_id = ?", "live-b").Error)
	require.Equal(t, domain.LiveStatusLive, session.Status,
		"fetch 실패 채널의 live session이 종료되면 안 된다")

	var sessionA domain.YouTubeLiveSession
	require.NoError(t, db.First(&sessionA, "video_id = ?", "live-a").Error)
	require.Equal(t, domain.LiveStatusLive, sessionA.Status)
}

func TestLivePollerPollBatchTreatsDeferredFailuresAsSoftSkips(t *testing.T) {
	db := newPollerBatchTestDB(t, &domain.YouTubeNotificationOutbox{})
	provider := &fakeLiveStatusWithFailuresProvider{
		failures: map[string]error{
			"UC_A": livestatus.NewDeferred(livestatus.DeferredReasonPerCycleCap, "UC_A", nil),
			"UC_B": livestatus.NewDeferred(livestatus.DeferredReasonAdmissionDeferred, "UC_B", nil),
		},
	}
	poller := NewLivePollerWithStatusProvider(provider, nil, db)

	errs := poller.PollBatch(context.Background(), []string{"UC_A", "UC_B"})

	require.Empty(t, errs)

	var sessions []domain.YouTubeLiveSession
	require.NoError(t, db.Find(&sessions).Error)
	require.Empty(t, sessions)
}

func TestLivePollerPollBatchAttributesEmptyChannelIDStreamToSingleChannel(t *testing.T) {
	db := newPollerBatchTestDB(t, &domain.YouTubeNotificationOutbox{})
	startedAt := time.Date(2026, 6, 6, 12, 0, 0, 0, time.UTC)
	provider := &fakeLiveStatusWithFailuresProvider{
		streams: []*domain.Stream{
			{
				ID:          "live-anon",
				ChannelID:   "",
				Title:       "Anon",
				Status:      domain.StreamStatusLive,
				StartActual: &startedAt,
			},
		},
	}
	poller := NewLivePollerWithStatusProvider(provider, nil, db)

	errs := poller.PollBatch(context.Background(), []string{"UC_ONLY"})

	require.Empty(t, errs)
	var session domain.YouTubeLiveSession
	require.NoError(t, db.First(&session, "video_id = ?", "live-anon").Error)
	require.Equal(t, "UC_ONLY", session.ChannelID)
}

func TestLivePollerPollBatchDropsStreamsOutsideRequestedChannelSet(t *testing.T) {
	db := newPollerBatchTestDB(t, &domain.YouTubeNotificationOutbox{})
	startedAt := time.Date(2026, 6, 6, 12, 0, 0, 0, time.UTC)
	provider := &fakeLiveStatusWithFailuresProvider{
		streams: []*domain.Stream{
			{
				ID:          "live-a",
				ChannelID:   "UC_A",
				Title:       "A",
				Status:      domain.StreamStatusLive,
				StartActual: &startedAt,
			},
			{
				ID:          "live-x",
				ChannelID:   "UC_X",
				Title:       "X",
				Status:      domain.StreamStatusLive,
				StartActual: &startedAt,
			},
			{
				ID:          "live-anon",
				ChannelID:   "",
				Title:       "Anon",
				Status:      domain.StreamStatusLive,
				StartActual: &startedAt,
			},
		},
	}
	poller := NewLivePollerWithStatusProvider(provider, nil, db)

	errs := poller.PollBatch(context.Background(), []string{"UC_A", "UC_B"})

	require.Empty(t, errs)
	var sessions []domain.YouTubeLiveSession
	require.NoError(t, db.Order("video_id").Find(&sessions).Error)
	require.Len(t, sessions, 1, "out-of-set and empty-ChannelID streams must be dropped in multi-channel batches")
	require.Equal(t, "live-a", sessions[0].VideoID)
}
