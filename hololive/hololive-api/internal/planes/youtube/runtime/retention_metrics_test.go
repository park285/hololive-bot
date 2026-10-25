package runtime

import (
	"errors"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-api/internal/youtube/sourceobservation"
)

func TestRetentionMetricsKeepPartialSuccessAndOnlyMarkFailedTable(t *testing.T) {
	const completed, failed = "source_observation_queue", "source_observation_collisions"

	deleted := youtubeRetentionDeletedTotal.WithLabelValues(completed)
	completedErrors := youtubeRetentionErrorsTotal.WithLabelValues(completed)
	failedErrors := youtubeRetentionErrorsTotal.WithLabelValues(failed)
	beforeDeleted, beforeCompletedErrors, beforeFailedErrors := testutil.ToFloat64(deleted), testutil.ToFloat64(completedErrors), testutil.ToFloat64(failedErrors)

	youtubeRetentionBacklogAgeSeconds.WithLabelValues("source_observations").Set(3600)

	recordRetentionTick(sourceobservation.RetentionResult{
		Table: completed, FailedTable: failed, Deleted: 3,
		ByTable: []sourceobservation.RetentionResult{{Table: completed, Deleted: 3}},
	}, errors.New("later table failed"))

	require.InDelta(t, beforeDeleted+3, testutil.ToFloat64(deleted), 0)
	require.InDelta(t, beforeCompletedErrors, testutil.ToFloat64(completedErrors), 0)
	require.InDelta(t, beforeFailedErrors+1, testutil.ToFloat64(failedErrors), 0)
	require.False(t, youtubeRetentionBacklogAgeSeconds.DeleteLabelValues("source_observations"), "unmeasured later steps must not keep stale backlog values")
}

func TestRetentionBacklogMetricClearsZeroAndOmitsUnknown(t *testing.T) {
	const table = "source_observations"

	t.Cleanup(func() { youtubeRetentionBacklogAgeSeconds.DeleteLabelValues(table) })
	recordRetentionPart(sourceobservation.RetentionResult{Table: table, BacklogAge: time.Hour, BacklogKnown: true})
	require.InDelta(t, 3600, testutil.ToFloat64(youtubeRetentionBacklogAgeSeconds.WithLabelValues(table)), 0)

	recordRetentionPart(sourceobservation.RetentionResult{Table: table, BacklogKnown: true})
	require.InDelta(t, 0, testutil.ToFloat64(youtubeRetentionBacklogAgeSeconds.WithLabelValues(table)), 0)

	recordRetentionPart(sourceobservation.RetentionResult{Table: table})
	require.False(t, youtubeRetentionBacklogAgeSeconds.DeleteLabelValues(table), "unknown backlog must not retain a stale or false-zero sample")
}

// 원본 단계가 앞 창을 커밋한 뒤 실패해도 그 창들의 삭제·검사·순회 완료를 센다.
func TestRetentionMetricsCountCommittedSourceWindowsOfFailedTick(t *testing.T) {
	const table = "source_observations"

	deleted := youtubeRetentionDeletedTotal.WithLabelValues(table)
	failures := youtubeRetentionErrorsTotal.WithLabelValues(table)
	beforeDeleted, beforeFailures := testutil.ToFloat64(deleted), testutil.ToFloat64(failures)
	beforeScanned, beforeTraversals := testutil.ToFloat64(youtubeRetentionSourceScannedTotal), testutil.ToFloat64(youtubeRetentionSourceTraversalsTotal)

	recordRetentionTick(sourceobservation.RetentionResult{
		Table: table, FailedTable: table, Deleted: 2,
		ByTable: []sourceobservation.RetentionResult{{Table: table, Deleted: 2, Scanned: 1500, Traversals: 1}},
	}, errors.New("later window failed"))

	require.InDelta(t, beforeDeleted+2, testutil.ToFloat64(deleted), 0)
	require.InDelta(t, beforeFailures+1, testutil.ToFloat64(failures), 0)
	require.InDelta(t, beforeScanned+1500, testutil.ToFloat64(youtubeRetentionSourceScannedTotal), 0)
	require.InDelta(t, beforeTraversals+1, testutil.ToFloat64(youtubeRetentionSourceTraversalsTotal), 0)
}
