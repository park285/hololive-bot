package checking

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/dispatchoutbox"
	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/tier"
	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/alarmtiming/targetpolicy"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestUpcomingCandidateVirtualTimePreservesSelectedCategoryUntilStart(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		now := time.Now()
		start := now.Add(5 * time.Minute)
		candidate := dispatchoutbox.UpcomingCandidate{Notification: domain.AlarmNotification{MinutesUntil: 5, Stream: &domain.Stream{ID: "virtual-stream", StartScheduled: &start}}}

		time.Sleep(65 * time.Second)

		// 현재 crossing에서는 5분을 다시 선정할 수 없어도 저장된 후보는 유효하다.
		_, crossed := targetpolicy.NewTargetMinutePolicy([]int{5}).HighestCrossed(start, targetpolicy.ResolveEvaluationWindow(now.Add(time.Minute), time.Now(), 75*time.Second))
		require.False(t, crossed)
		require.Empty(t, upcomingCandidateTermination(&candidate, nil, time.Now()))
		require.Equal(t, 5, candidate.Notification.MinutesUntil)
		time.Sleep(235 * time.Second)
		require.Equal(t, "expired", upcomingCandidateTermination(&candidate, nil, time.Now()))
	})
}

func TestUpcomingCandidateOnlyExplicitFactsTerminate(t *testing.T) {
	now := time.Now()
	start := now.Add(5 * time.Minute)
	candidate := dispatchoutbox.UpcomingCandidate{Notification: domain.AlarmNotification{Stream: &domain.Stream{ID: "selected", StartScheduled: &start}}}
	require.Empty(t, upcomingCandidateTermination(&candidate, nil, now))

	changed := start.Add(time.Minute)
	require.Equal(t, "schedule_changed", upcomingCandidateTermination(&candidate, []*domain.Stream{{ID: "selected", Status: domain.StreamStatusUpcoming, StartScheduled: &changed}}, now))
	require.Equal(t, "stream_ended", upcomingCandidateTermination(&candidate, []*domain.Stream{{ID: "selected", Status: domain.StreamStatusPast}}, now))
}

type failedCandidateStore struct{}

func (failedCandidateStore) Stage(context.Context, string, time.Time, []*domain.AlarmNotification) error {
	return errors.New("staging unavailable")
}

func (failedCandidateStore) Pending(context.Context, time.Time) ([]dispatchoutbox.UpcomingCandidate, error) {
	return nil, nil
}
func (failedCandidateStore) Finish(context.Context, string, string, time.Time) error { return nil }

func TestYouTubeCandidateStagingFailureDoesNotAdvanceEvaluation(t *testing.T) {
	scheduler := tier.NewTieredScheduler(newCheckerTestLogger())
	checker := &YouTubeChecker{tierScheduler: scheduler, upcomingCandidates: failedCandidateStore{}, logger: newCheckerTestLogger()}
	_, err := checker.collectDueYouTubeNotifications(t.Context(), []string{"staging-channel"}, nil, nil, map[string][]string{"staging-channel": {"room"}}, nil, time.Now())
	require.ErrorContains(t, err, "staging unavailable")
	require.True(t, scheduler.LastCheckedAt("staging-channel").IsZero())
}

func TestUpcomingRecoveryTerminatesRemovedSubscription(t *testing.T) {
	pool := dbtest.NewPool(t)
	now := time.Now().UTC()
	start := now.Add(5 * time.Minute)
	store := dispatchoutbox.NewUpcomingCandidates(pool)
	notification := domain.NewAlarmNotification("removed-room", &domain.Channel{ID: "removed-channel"}, &domain.Stream{ID: "removed-stream", ChannelID: "removed-channel", Status: domain.StreamStatusUpcoming, StartScheduled: &start}, 5, nil, "")
	require.NoError(t, store.Stage(t.Context(), "removed-channel", now, []*domain.AlarmNotification{notification}))

	checker := &YouTubeChecker{subscriptionDB: pool, upcomingCandidates: store}
	result, err := checker.recoverUpcomingCandidates(t.Context(), nil, now.Add(65*time.Second))
	require.NoError(t, err)
	require.Empty(t, result)

	var outcome string

	require.NoError(t, pool.QueryRow(t.Context(), "SELECT outcome FROM alarm_upcoming_candidates WHERE room_id = 'removed-room'").Scan(&outcome))
	require.Equal(t, "subscription_removed", outcome)
}

type recoveringCandidateStore struct {
	failedCandidateStore

	unavailable bool
	staged      []*domain.AlarmNotification
}

func (s *recoveringCandidateStore) Stage(_ context.Context, _ string, _ time.Time, notifications []*domain.AlarmNotification) error {
	if s.unavailable {
		return errors.New("staging unavailable")
	}

	s.staged = notifications

	return nil
}

func TestUpcomingPrecommitFailureRetainsSelectionBeyondLookback(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		store := &recoveringCandidateStore{unavailable: true}
		checker := &YouTubeChecker{upcomingCandidates: store}
		now := time.Now()
		start := now.Add(5 * time.Minute)
		notification := domain.NewAlarmNotification("room", &domain.Channel{ID: "channel"}, &domain.Stream{ID: "video", StartScheduled: &start, Status: domain.StreamStatusUpcoming}, 5, nil, "")
		work := youtubeChannelCheckWork{channelID: "channel"}
		require.Error(t, checker.stageUpcomingChannel(t.Context(), &work, now, []*domain.AlarmNotification{notification}))
		time.Sleep(90 * time.Second)

		store.unavailable = false

		require.NoError(t, checker.flushUnstagedUpcoming(t.Context()))
		require.Len(t, store.staged, 1)
		require.Equal(t, 5, store.staged[0].MinutesUntil)
		require.True(t, store.staged[0].Stream.StartScheduled.Equal(start))
		require.Empty(t, checker.unstagedUpcoming)
	})
}

func TestUpcomingRecoveryPersistsScheduleChangeTermination(t *testing.T) {
	pool := dbtest.NewPool(t)
	now := time.Now().UTC()
	start := now.Add(5 * time.Minute)
	store := dispatchoutbox.NewUpcomingCandidates(pool)
	notification := domain.NewAlarmNotification("changed-room", &domain.Channel{ID: "changed-channel"}, &domain.Stream{ID: "changed-stream", ChannelID: "changed-channel", Status: domain.StreamStatusUpcoming, StartScheduled: &start}, 5, nil, "")
	require.NoError(t, store.Stage(t.Context(), "changed-channel", now, []*domain.AlarmNotification{notification}))

	checker := &YouTubeChecker{subscriptionDB: pool, upcomingCandidates: store}
	result, err := checker.recoverUpcomingCandidates(t.Context(), map[string][]*domain.Stream{"changed-channel": {{ID: "changed-stream", Status: domain.StreamStatusUpcoming, StartScheduled: new(start.Add(time.Minute))}}}, now.Add(65*time.Second))
	require.NoError(t, err)
	require.Empty(t, result)

	var outcome string

	require.NoError(t, pool.QueryRow(t.Context(), "SELECT outcome FROM alarm_upcoming_candidates WHERE room_id = 'changed-room'").Scan(&outcome))
	require.Equal(t, "schedule_changed", outcome)
}
