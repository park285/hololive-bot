package scheduleposter

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
)

func TestMetricsExposeEveryLabelValueBeforeTheFirstEvent(t *testing.T) {
	t.Parallel()

	families, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)

	got := map[string][]string{}

	for _, family := range families {
		for _, metric := range family.GetMetric() {
			for _, label := range metric.GetLabel() {
				got[family.GetName()] = append(got[family.GetName()], label.GetName()+"="+label.GetValue())
			}

			if len(metric.GetLabel()) == 0 {
				got[family.GetName()] = append(got[family.GetName()], "")
			}
		}
	}

	for name, want := range map[string][]string{
		"hololive_schedule_poster_rows_total": {"result=succeeded", "result=failed", "result=stale", "result=store_error"},
		"hololive_schedule_poster_posts_total": {
			"outcome=not_relevant", "outcome=relevant", "outcome=matched", "outcome=judge_failed", "outcome=match_failed",
		},
		"hololive_schedule_poster_change_cycles_total":                     {"result=completed", "result=paused", "result=store_error"},
		"hololive_schedule_poster_change_last_completed_timestamp_seconds": {""},
	} {
		require.ElementsMatch(t, want, got[name], name)
	}
}
