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

	var (
		compared int
		equal    bool
	)

	require.NoError(t, pool.QueryRow(t.Context(), `
		SELECT count(*),bool_and(to_jsonb(semantic_value)=facts)
		FROM youtube_live_review_receipt_facts`).Scan(&compared, &equal))
	require.GreaterOrEqual(t, compared, 14)
	require.True(t, equal)

	for _, zone := range []string{"UTC", "Asia/Seoul", "America/New_York"} {
		t.Run(zone, func(t *testing.T) {
			assertLiveReviewTypedFactsForZone(t, pool, zone)
		})
	}
}

// 같은 이름의 다른 타입·일반 열은 기존 영수증을 잘못 닫는 대신 migration을 중단한다.
