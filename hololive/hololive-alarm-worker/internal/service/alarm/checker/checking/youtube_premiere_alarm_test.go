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

func TestYouTubeCheckerBuildUpcomingNotificationsPremiereFiveMinuteNotice(t *testing.T) {
	t.Parallel()

	const videoID = "premiere-upcoming"

	// 시작 5분 전 target을 지나는 창이다. 가장 이른 target 시각은 now와 같다.
	for name, tc := range map[string]struct {
		noticeAt map[string]time.Time
		want     int
	}{
		"notice missing after retention": {want: 1},
		"notice days before":             {noticeAt: map[string]time.Time{videoID: time.Date(2026, time.March, 2, 6, 0, 0, 0, time.UTC)}, want: 1},
		"notice just before target":      {noticeAt: map[string]time.Time{videoID: time.Date(2026, time.March, 5, 5, 59, 59, 0, time.UTC)}, want: 1},
		"notice at target boundary":      {noticeAt: map[string]time.Time{videoID: time.Date(2026, time.March, 5, 6, 0, 0, 0, time.UTC)}, want: 0},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			checker, _, now := newYouTubeBuilderFixture(t)

			checker.upcomingCandidates = premiereNoticeStore{noticeAt: tc.noticeAt}

			start := now.Add(5 * time.Minute)
			window := targetpolicy.EvaluationWindow{
				Start: now.Add(-75 * time.Second),
				End:   now,
			}

			notifications, err := checker.buildUpcomingNotifications(t.Context(), &domain.Stream{
				ID:             videoID,
				ChannelID:      testChIDShort1,
				Status:         domain.StreamStatusUpcoming,
				StartScheduled: &start,
				IsPremiere:     true,
				Channel:        &domain.Channel{ID: testChIDShort1, Name: testChannelName1},
			}, []string{testRoomShort1}, window)
			require.NoError(t, err)
			require.Len(t, notifications, tc.want)

			if tc.want > 0 {
				assert.Equal(t, 5, notifications[0].MinutesUntil)
				assert.True(t, notifications[0].Stream.IsPremiere)
			}
		})
	}
}

func TestYouTubeCheckerBuildUpcomingNotificationsPremiereNoticeLookupErrorFails(t *testing.T) {
	t.Parallel()

	checker, _, now := newYouTubeBuilderFixture(t)

	checker.upcomingCandidates = premiereNoticeStore{err: errors.New("notice lookup failed")}

	start := now.Add(5 * time.Minute)

	_, err := checker.buildUpcomingNotifications(t.Context(), &domain.Stream{
		ID:             "premiere-notice-error",
		ChannelID:      testChIDShort1,
		Status:         domain.StreamStatusUpcoming,
		StartScheduled: &start,
		IsPremiere:     true,
		Channel:        &domain.Channel{ID: testChIDShort1, Name: testChannelName1},
	}, []string{testRoomShort1}, targetpolicy.EvaluationWindow{Start: now.Add(-75 * time.Second), End: now})
	require.ErrorContains(t, err, "load premiere content notice")
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
	// 일정 변경은 이미 보낸 분 전 알림을 정정하므로 공개 예정 안내 시각과 무관하게 보낸다.
	checker.upcomingCandidates = premiereNoticeStore{noticeAt: map[string]time.Time{"premiere-delay": now}}

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
