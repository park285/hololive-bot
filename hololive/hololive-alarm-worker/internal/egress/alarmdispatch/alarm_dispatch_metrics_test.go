package alarmdispatch

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestObserveAlarmDispatchBacklogObservationFailureIncrementsCounter(t *testing.T) {
	alarmDispatchRunnerMetrics()

	before := alarmDispatchCounterMetricValue(t, "alarm_dispatch_pg_backlog_observation_failed_total")

	observeAlarmDispatchBacklogObservationFailure()

	assert.InDelta(t, before+1, alarmDispatchCounterMetricValue(t, "alarm_dispatch_pg_backlog_observation_failed_total"), 0)
}

func alarmDispatchCounterMetricValue(t *testing.T, name string) float64 {
	t.Helper()
	alarmDispatchRunnerMetrics()

	families, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)

	for _, family := range families {
		if family.GetName() == name {
			require.Len(t, family.Metric, 1)

			return family.Metric[0].GetCounter().GetValue()
		}
	}

	require.FailNow(t, "alarm dispatch counter metric not found", name)

	return 0
}
