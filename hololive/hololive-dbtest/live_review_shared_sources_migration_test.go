package dbtest

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLiveReviewSharedSourcesPreservesStoredDecisions(t *testing.T) {
	pool := NewPool(t)
	seedLiveReviewComparison(t, pool)
	seedRecordedLiveReviews(t, pool)
	seedLiveReviewTypedFacts(t, pool)

	beforeResults, beforeReceipts := liveReviewComparisonState(t, pool)

	afterResults, afterReceipts := liveReviewComparisonState(t, pool)
	require.JSONEq(t, beforeResults, afterResults)
	require.Equal(t, beforeReceipts, afterReceipts)
	assertLiveReviewReceiptFactsMatchReceipts(t, pool)

	for _, zone := range []string{"UTC", "Asia/Seoul", "America/New_York"} {
		t.Run(zone, func(t *testing.T) {
			assertLiveReviewTypedFactsForZone(t, pool, zone)
		})
	}
}
