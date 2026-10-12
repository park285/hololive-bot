package checking

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/alarmtiming/targetpolicy"
	"github.com/kapu/hololive-shared/pkg/domain"
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
	stream := &domain.Stream{
		ID:         streamID,
		ChannelID:  testChID1,
		Status:     domain.StreamStatusUpcoming,
		IsPremiere: false,
	}
	streamsByChannel := map[string][]*domain.Stream{
		testChID1: {stream},
	}

	checker := &YouTubeChecker{
		persistedLiveSource: &fakeYouTubeLiveSessionSource{
			confirmedPremiereIDs: map[string]struct{}{streamID: {}},
		},
	}

	require.NoError(t, checker.applyConfirmedPremiereClassification(t.Context(), streamsByChannel))
	assert.True(t, stream.IsPremiere)
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

func TestYouTubeCheckerBuildUpcomingNotificationsSelectsPremiereFiveMinuteCandidate(t *testing.T) {
	t.Parallel()

	checker, _, now := newYouTubeBuilderFixture(t)
	start := now.Add(5 * time.Minute)
	window := targetpolicy.EvaluationWindow{
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
	require.Len(t, notifications, 1)
	assert.Equal(t, 5, notifications[0].MinutesUntil)
	assert.True(t, notifications[0].Stream.IsPremiere)
}

func TestYouTubeCheckerBuildUpcomingNotificationsKeepsOrdinaryFiveMinuteCandidate(t *testing.T) {
	t.Parallel()

	checker, _, now := newYouTubeBuilderFixture(t)
	start := now.Add(5 * time.Minute)
	window := targetpolicy.EvaluationWindow{
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

func TestYouTubeCheckerBuildUpcomingNotificationsReportsPremiereScheduleChange(t *testing.T) {
	t.Parallel()

	checker, dedupService, now := newYouTubeBuilderFixture(t)
	previousScheduled := now.Add(5 * time.Minute)
	currentScheduled := now.Add(12 * time.Minute)

	require.NoError(t, dedupService.MarkAsNotified(t.Context(), "premiere-delay", previousScheduled, 5))

	window := targetpolicy.EvaluationWindow{
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
	require.Len(t, notifications, 1)
	assert.NotEmpty(t, notifications[0].ScheduleChangeMessage)
}

// 분 전 target을 모두 지난 뒤 발견된 Premiere는 발견 시 알림이 없으므로 일반 방송처럼 시작 알림을 받는다.
func TestYouTubeCheckerBuildLiveCatchupNotificationsIncludesPremiere(t *testing.T) {
	t.Parallel()

	for name, premiere := range map[string]bool{"premiere": true, "ordinary": false} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			checker, _, now := newYouTubeBuilderFixture(t)
			start := now.Add(-3 * time.Minute)

			notifications, err := checker.buildLiveCatchupNotifications(t.Context(), testChIDLive, &domain.Stream{
				ID:             name + "-live",
				Title:          name + " live",
				ChannelID:      testChIDLive,
				Status:         domain.StreamStatusLive,
				StartScheduled: &start,
				StartActual:    &start,
				IsPremiere:     premiere,
				Channel:        &domain.Channel{ID: testChIDLive, Name: testChannelName1},
			}, []string{testRoomShort1}, now, nil)
			require.NoError(t, err)
			require.Len(t, notifications, 1)
			assert.Equal(t, 5, notifications[0].MinutesUntil)
			assert.Equal(t, premiere, notifications[0].Stream.IsPremiere)
		})
	}
}
