package dbtest

import (
	"testing"

	"github.com/stretchr/testify/require"
)

const liveReviewSharedSourcesMigration = "310_youtube_live_review_shared_sources.sql"

// 타입 투영은 기존의 모든 필드·시간대·재적용에서도 판단과 원본을 보존한다.
func TestLiveReviewSharedSourcesPreservesStoredDecisions(t *testing.T) {
	pool, dir := projectionMigrationPoolBefore(t, liveReviewSharedSourcesMigration)
	seedLiveReviewComparison(t, pool)
	seedRecordedLiveReviews(t, pool)
	seedLiveReviewTypedFacts(t, pool)

	beforeResults, beforeReceipts := liveReviewComparisonState(t, pool)

	for range 2 {
		require.NoError(t, applyMigrationFile(t.Context(), pool, dir, liveReviewSharedSourcesMigration))

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
	}

	for _, zone := range []string{"UTC", "Asia/Seoul", "America/New_York"} {
		t.Run(zone, func(t *testing.T) {
			assertLiveReviewTypedFactsForZone(t, pool, zone)
		})
	}
}

// 같은 이름의 다른 타입·일반 열은 기존 영수증을 잘못 닫는 대신 migration을 중단한다.
func TestLiveReviewSharedSourcesRejectsIncompatibleObjects(t *testing.T) {
	for _, tc := range []struct {
		name string
		ddl  string
	}{
		{
			name: "session_field_order",
			ddl: `CREATE TYPE youtube_live_review_session_value AS (
				channel_id text,video_id text,status text,lifecycle_origin text,title text,topic_id text,
				is_premiere boolean,scheduled_start_time numeric,started_at numeric,ended_at numeric,
				live_first_seen_at numeric)`,
		},
		{
			name: "existing_plain_column",
			ddl:  `ALTER TABLE youtube_live_review_receipt_facts ADD COLUMN semantic_value text`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pool, dir := projectionMigrationPoolBefore(t, liveReviewSharedSourcesMigration)
			seedLiveReviewComparison(t, pool)

			beforeResults, beforeReceipts := liveReviewComparisonState(t, pool)
			_, err := pool.Exec(t.Context(), tc.ddl)
			require.NoError(t, err)

			require.Error(t, applyMigrationFile(t.Context(), pool, dir, liveReviewSharedSourcesMigration))

			afterResults, afterReceipts := liveReviewComparisonState(t, pool)
			require.JSONEq(t, beforeResults, afterResults)
			require.Equal(t, beforeReceipts, afterReceipts)
		})
	}
}
