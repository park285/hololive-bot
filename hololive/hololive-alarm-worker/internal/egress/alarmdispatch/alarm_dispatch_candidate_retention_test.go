package alarmdispatch

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/dispatchoutbox"
	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestAlarmDispatchRetentionIncludesTerminatedCandidates(t *testing.T) {
	pool := dbtest.NewPool(t)
	candidates := dispatchoutbox.NewUpcomingCandidates(pool)
	start := time.Now().UTC().Add(time.Hour)
	n := domain.NewAlarmNotification("cleanup-room", &domain.Channel{ID: "cleanup-channel"}, &domain.Stream{ID: "cleanup-stream", ChannelID: "cleanup-channel", Status: domain.StreamStatusUpcoming, StartScheduled: &start}, 5, nil, "")
	require.NoError(t, candidates.Stage(t.Context(), "cleanup-channel", start.Add(-time.Hour), []*domain.AlarmNotification{n}))

	key := dispatchoutbox.BuildDedupeKeyFromEnvelope(&domain.AlarmQueueEnvelope{Notification: *n})
	require.NoError(t, candidates.Finish(t.Context(), map[string][]string{"subscription_removed": {key}}, start.Add(-72*time.Hour)))

	store := alarmDispatchMaintenancePgxStore{db: pool, beginner: pool}
	before := alarmDispatchRetentionMetricValues(t)
	counts, err := store.DeleteOrphanEvents(t.Context(), 1, 1)
	require.NoError(t, err)
	require.Equal(t, alarmDispatchOrphanEventCounts{upcomingCandidates: 1}, counts)
	require.Equal(t, before, alarmDispatchRetentionMetricValues(t), "store returns counts; the sweep owns committed observations")

	var remaining int

	require.NoError(t, pool.QueryRow(t.Context(), "SELECT count(*) FROM alarm_upcoming_candidates").Scan(&remaining))
	require.Zero(t, remaining)
}
