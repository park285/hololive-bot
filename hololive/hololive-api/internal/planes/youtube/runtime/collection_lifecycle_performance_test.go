package runtime

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
)

const (
	collectionLifecyclePopulation = 30000
	collectionLifecycleReviewed   = 500
)

func TestCollectionLifecycleSnapshotWithinRuntimeBudget(t *testing.T) {
	for _, withReceipts := range []bool{false, true} {
		name := "zero_receipts"

		if withReceipts {
			name = "many_receipts"
		}

		t.Run(name, func(t *testing.T) {
			pool := dbtest.NewPool(t)
			seedCollectionLifecyclePopulation(t, pool)

			var reviewed int64

			if withReceipts {
				seedCollectionLifecycleReceipts(t, pool)

				reviewed = collectionLifecycleReviewed
			}

			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()

			started := time.Now()
			rows, err := pool.Query(ctx, mustSQL("collection_target_observability.sql"))
			require.NoError(t, err)

			defer rows.Close()

			samples, err := scanCollectionTargets(rows)
			require.NoError(t, err)
			t.Logf("snapshot for %d live sessions, reviewed=%d: %s", collectionLifecyclePopulation, reviewed, time.Since(started))
			require.Len(t, samples, collectionTargetJobCount)
			require.EqualValues(t, collectionLifecyclePopulation, samples[0].retainedTotal)
			require.EqualValues(t, collectionLifecyclePopulation, samples[0].upcoming)
			require.Equal(t, collectionLifecyclePopulation-reviewed, samples[0].legacyUnreviewed)
			require.Equal(t, reviewed, samples[0].closedUnresolved)
			require.Zero(t, samples[0].stateMismatch)
		})
	}
}

func seedCollectionLifecyclePopulation(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	_, err := pool.Exec(t.Context(), `
        INSERT INTO youtube_collection_projection_generations
        (status,row_count,projection_sha256,valid_until,activated_at)
        VALUES ('CURRENT',1,repeat('a',64),now()+interval '1 hour',now());
        INSERT INTO youtube_collection_targets
        (projection_generation,subject_key,observation_kind,priority,poll_interval_ms,enabled,valid_until)
        SELECT generation,'load-channel','live_snapshot',20,120000,true,now()+interval '1 hour'
        FROM youtube_collection_projection_generations WHERE status='CURRENT';
        INSERT INTO youtube_live_sessions(video_id,channel_id,status,title,lifecycle_origin)
        SELECT 'load-'||n,'load-channel','UPCOMING',repeat('x',500),'legacy_unknown'
        FROM generate_series(1,30000) n;
        INSERT INTO youtube_live_reconciliation_heads(video_id,status)
        SELECT video_id,'UPCOMING' FROM youtube_live_sessions;
        ANALYZE youtube_collection_targets;
        ANALYZE youtube_live_sessions;
        ANALYZE youtube_live_reconciliation_heads;
        ANALYZE youtube_live_review_receipts;
    `)
	require.NoError(t, err)
}

func seedCollectionLifecycleReceipts(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	_, err := pool.Exec(t.Context(), `
        INSERT INTO youtube_video_availability
        (video_id,channel_id,provider,identity_confirmed,availability,method,unknown_reason,evidence_sha256,
         scheduled_for,effective_at,observed_at,received_at)
        SELECT 'load-'||n,'load-channel','youtubejs',false,'UNKNOWN','unknown','identity_missing',repeat('a',64),
               now(),now(),now(),now() FROM generate_series(1,500) n;
    `)
	require.NoError(t, err)

	_, err = pool.Exec(t.Context(), `
        BEGIN ISOLATION LEVEL SERIALIZABLE;
        SELECT record_youtube_live_review(md5('current-'||n)::uuid,'load-'||n,snapshot.snapshot_sha256,
                   'test-operator','현재 snapshot 검토')
        FROM generate_series(1,500) n
        CROSS JOIN LATERAL youtube_live_review_snapshot('load-'||n) snapshot;
        COMMIT;
        INSERT INTO youtube_live_review_receipts
        (receipt_id,video_id,snapshot_sha256,original_snapshot,evidence_refs,disposition,operator_id,reason)
        SELECT md5('historical-'||n||'-'||revision)::uuid,'load-'||n,
               encode(sha256(convert_to(jsonb_build_object('historical_revision',revision)::text,'UTF8')),'hex'),
               jsonb_build_object('historical_revision',revision),'{}','closed_unresolved','test-operator','과거 snapshot 검토'
        FROM generate_series(1,500) n CROSS JOIN generate_series(1,30) revision;
        ANALYZE youtube_live_review_receipts;
    `)
	require.NoError(t, err)
}
