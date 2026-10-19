package dbtest

import (
	"testing"

	"github.com/stretchr/testify/require"
)

const liveRuntimeTriggerKeyAuditMigration = "325_youtube_live_runtime_trigger_key_audit.sql"

func TestLiveRuntimeTriggerKeyAuditMigrationPreservesDataAndSecurity(t *testing.T) {
	pool, dir := projectionMigrationPoolBefore(t, liveRuntimeTriggerKeyAuditMigration)
	seedLiveReviewComparison(t, pool)
	seedRecordedLiveReviews(t, pool)

	beforeState, beforeReceipts := liveReviewComparisonState(t, pool)
	beforeSecurity := liveRuntimeRefreshSecurity(t, pool)

	for range 2 {
		require.NoError(t, applyMigrationFile(t.Context(), pool, dir, liveRuntimeTriggerKeyAuditMigration))
		assertLiveRuntimeState(t, pool)

		afterState, afterReceipts := liveReviewComparisonState(t, pool)
		require.JSONEq(t, beforeState, afterState)
		require.Equal(t, beforeReceipts, afterReceipts)
		require.JSONEq(t, beforeSecurity, liveRuntimeRefreshSecurity(t, pool))
	}
}
