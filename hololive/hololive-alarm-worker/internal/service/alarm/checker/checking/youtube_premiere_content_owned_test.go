package checking

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
	sharedchecker "github.com/kapu/hololive-shared/pkg/service/alarm/checker"
)

func TestMergePersistedLiveSessionStreamsAppliesPremiereTrueOR(t *testing.T) {
	t.Parallel()

	channelID := "ch-premiere-merge"
	streamID := "stream-premiere-merge"
	streamsByChannel := map[string][]*domain.Stream{
		channelID: {{
			ID:         streamID,
			ChannelID:  channelID,
			Status:     domain.StreamStatusUpcoming,
			IsPremiere: false,
		}},
	}

	mergePersistedLiveSessionStreams(streamsByChannel, []PersistedYouTubeLiveSession{{
		Stream: &domain.Stream{
			ID:         streamID,
			ChannelID:  channelID,
			Status:     domain.StreamStatusUpcoming,
			IsPremiere: true,
		},
	}})

	require.Len(t, streamsByChannel[channelID], 1)
	assert.True(t, streamsByChannel[channelID][0].IsPremiere)
}

func TestApplyConfirmedPremiereClassificationMarksHolodexStream(t *testing.T) {
	t.Parallel()

	streamID := "holodex-premiere"
	streamsByChannel := map[string][]*domain.Stream{
		testChID1: {{
			ID:         streamID,
			ChannelID:  testChID1,
			Status:     domain.StreamStatusUpcoming,
			IsPremiere: false,
		}},
	}

	checker := &YouTubeChecker{
		persistedLiveSource: &fakeYouTubeLiveSessionSource{
			confirmedPremiereIDs: map[string]struct{}{streamID: {}},
		},
	}

	require.NoError(t, checker.applyConfirmedPremiereClassification(t.Context(), streamsByChannel))
	assert.True(t, streamsByChannel[testChID1][0].IsPremiere)
}

func TestApplyConfirmedPremiereClassificationFailsClosedOnLookupError(t *testing.T) {
	t.Parallel()

	checker := &YouTubeChecker{
		persistedLiveSource: &fakeYouTubeLiveSessionSource{
			confirmedPremiereErr: errors.New("premiere lookup failed"),
		},
	}

	err := checker.applyConfirmedPremiereClassification(t.Context(), map[string][]*domain.Stream{
		testChID1: {{ID: "vid-1", ChannelID: testChID1}},
	})
	require.Error(t, err)
	assert.ErrorContains(t, err, "load confirmed premiere ids")
}

func TestYouTubeCheckerBuildUpcomingNotificationsSkipsPremiereInFiveMinuteWindow(t *testing.T) {
	t.Parallel()

	checker, _, now := newYouTubeBuilderFixture(t)
	start := now.Add(5 * time.Minute)
	window := sharedchecker.EvaluationWindow{
		Start: now.Add(-75 * time.Second),
		End:   now,
	}

	notifications, err := checker.buildUpcomingNotifications(t.Context(), &domain.Stream{
		ID:             "premiere-upcoming",
		ChannelID:      testChIDShort1,
		Status:         domain.StreamStatusUpcoming,
		StartScheduled: &start,
		IsPremiere:     true,
		Channel:        &domain.Channel{ID: testChIDShort1, Name: testChannelName1},
	}, []string{testRoomShort1}, window)
	require.NoError(t, err)
	assert.Nil(t, notifications)
}

func TestYouTubeCheckerBuildUpcomingNotificationsKeepsOrdinaryFiveMinuteCandidate(t *testing.T) {
	t.Parallel()

	checker, _, now := newYouTubeBuilderFixture(t)
	start := now.Add(5 * time.Minute)
	window := sharedchecker.EvaluationWindow{
		Start: now.Add(-75 * time.Second),
		End:   now,
	}

	notifications, err := checker.buildUpcomingNotifications(t.Context(), &domain.Stream{
		ID:             "ordinary-upcoming",
		ChannelID:      testChIDShort1,
		Status:         domain.StreamStatusUpcoming,
		StartScheduled: &start,
		Channel:        &domain.Channel{ID: testChIDShort1, Name: testChannelName1},
	}, []string{testRoomShort1}, window)
	require.NoError(t, err)
	require.Len(t, notifications, 1)
	assert.Equal(t, 5, notifications[0].MinutesUntil)
}

func TestYouTubeCheckerBuildUpcomingNotificationsSkipsPremiereScheduleChange(t *testing.T) {
	t.Parallel()

	checker, dedupService, now := newYouTubeBuilderFixture(t)
	previousScheduled := now.Add(5 * time.Minute)
	currentScheduled := now.Add(12 * time.Minute)

	require.NoError(t, dedupService.MarkAsNotified(t.Context(), "premiere-delay", previousScheduled, 5))

	window := sharedchecker.EvaluationWindow{
		Start: now.Add(-75 * time.Second),
		End:   now,
	}

	notifications, err := checker.buildUpcomingNotifications(t.Context(), &domain.Stream{
		ID:             "premiere-delay",
		ChannelID:      testChIDShort1,
		Status:         domain.StreamStatusUpcoming,
		StartScheduled: &currentScheduled,
		IsPremiere:     true,
		Channel:        &domain.Channel{ID: testChIDShort1, Name: testChannelName1},
	}, []string{testRoomShort1}, window)
	require.NoError(t, err)
	assert.Nil(t, notifications)
}

func TestYouTubeCheckerBuildLiveCatchupNotificationsSkipsPremiere(t *testing.T) {
	t.Parallel()

	checker, _, now := newYouTubeBuilderFixture(t)
	start := now.Add(-3 * time.Minute)

	notifications, err := checker.buildLiveCatchupNotifications(t.Context(), testChIDLive, &domain.Stream{
		ID:             "premiere-live",
		Title:          "premiere live",
		ChannelID:      testChIDLive,
		Status:         domain.StreamStatusLive,
		StartScheduled: &start,
		StartActual:    &start,
		IsPremiere:     true,
		Channel:        &domain.Channel{ID: testChIDLive, Name: testChannelName1},
	}, []string{testRoomShort1}, now, nil)
	require.NoError(t, err)
	assert.Nil(t, notifications)
}

func TestYouTubeCheckerBuildLiveCatchupNotificationsKeepsOrdinaryLive(t *testing.T) {
	t.Parallel()

	checker, _, now := newYouTubeBuilderFixture(t)
	start := now.Add(-3 * time.Minute)

	notifications, err := checker.buildLiveCatchupNotifications(t.Context(), testChIDLive, &domain.Stream{
		ID:             "ordinary-live",
		Title:          "ordinary live",
		ChannelID:      testChIDLive,
		Status:         domain.StreamStatusLive,
		StartScheduled: &start,
		StartActual:    &start,
		Channel:        &domain.Channel{ID: testChIDLive, Name: testChannelName1},
	}, []string{testRoomShort1}, now, nil)
	require.NoError(t, err)
	require.Len(t, notifications, 1)
	assert.Equal(t, 5, notifications[0].MinutesUntil)
}
