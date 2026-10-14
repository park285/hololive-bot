package dbtest

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
)

const collectionRuntimeBundlesMigration = "313_youtube_collection_runtime_bundles.sql"

// 운영 view를 oracle에 쓰면 잘못된 매핑도 양쪽에서 같아지므로 기존 VALUES 계약을 독립적으로 보존한다.
const collectionRuntimeKindsOracleSQL = `VALUES
	('youtubejs_channel_live','live_snapshot',false),
	('youtubejs_channel_live_check','channel_live_check',false),
	('youtubejs_video_live','video_live_check',false),
	('community_collect','community_page',false),
	('youtubejs_content','video_list',true),
	('youtubejs_content','shorts_list',true),
	('youtubejs_channel_metadata','channel_profile',false),
	('youtubejs_channel_metadata','channel_photo',false)`

const collectionRuntimeBundlesOracleSQL = `WITH mapping(kind,observation_kind,rpc_per_kind) AS (` + collectionRuntimeKindsOracleSQL + `), expected AS (
	SELECT target.projection_generation,mapping.kind COLLATE "C" AS kind,target.subject_key COLLATE "C" AS subject_key,
	       CASE WHEN mapping.rpc_per_kind THEN count(target.observation_kind) ELSE 1 END AS rpc_calls,
	       min(target.poll_interval_ms) AS interval_ms,min(target.created_at) AS created_at,
	       min(coalesce(target.not_before,'-infinity'::timestamptz)) AS eligible_at
	FROM youtube_collection_targets target
	JOIN youtube_collection_projection_generations header ON header.generation=target.projection_generation
	JOIN mapping USING(observation_kind)
	WHERE target.enabled AND header.status IN ('CURRENT','STAGING')
	GROUP BY target.projection_generation,mapping.kind COLLATE "C",target.subject_key COLLATE "C",mapping.rpc_per_kind
)
SELECT coalesce((SELECT jsonb_agg(to_jsonb(expected) ORDER BY projection_generation,kind COLLATE "C",subject_key COLLATE "C") FROM expected),'[]'::jsonb)::text,
       coalesce((SELECT jsonb_agg(to_jsonb(bundle) ORDER BY projection_generation,kind COLLATE "C",subject_key COLLATE "C") FROM youtube_collection_runtime_bundles bundle),'[]'::jsonb)::text`

type collectionBundleDB interface {
	QueryRow(context.Context, string, ...any) pgx.Row
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

func execCollectionBundleSQL(t *testing.T, db collectionBundleDB, query string) {
	t.Helper()

	_, err := db.Exec(t.Context(), query)
	require.NoError(t, err)
}

func assertCollectionRuntimeBundles(t *testing.T, db collectionBundleDB) {
	t.Helper()

	var expected, actual string

	require.NoError(t, db.QueryRow(t.Context(), collectionRuntimeBundlesOracleSQL).Scan(&expected, &actual))
	require.JSONEq(t, expected, actual)
}

func seedCollectionBundleHeaders(t *testing.T, db collectionBundleDB) {
	t.Helper()

	execCollectionBundleSQL(t, db, `INSERT INTO youtube_collection_projection_generations
		(generation,status,row_count,projection_sha256,valid_until,created_at,activated_at) OVERRIDING SYSTEM VALUE
		VALUES (101,'CURRENT',0,repeat('a',64),now()+interval '1 hour','2026-01-01','2026-01-02'),
		       (102,'STAGING',0,repeat('b',64),now()+interval '1 hour','2026-01-01',NULL),
		       (103,'RETIRED',0,repeat('c',64),'2026-01-03','2026-01-01','2026-01-02')`)
}

func seedCollectionBundleTargets(t *testing.T, db collectionBundleDB) {
	t.Helper()

	execCollectionBundleSQL(t, db, `INSERT INTO youtube_collection_targets
		(projection_generation,subject_key,observation_kind,priority,poll_interval_ms,enabled,created_at,not_before,member_since_generation)
		SELECT header.generation,fixture.subject,fixture.kind,40,fixture.cadence,fixture.enabled,
		       fixture.created::timestamptz,fixture.eligible::timestamptz,header.generation
		FROM youtube_collection_projection_generations header
		CROSS JOIN (VALUES
		 ('channel','video_list',120000,true,'2026-01-04','2099-01-04'),
		 ('channel','shorts_list',60000,true,'2026-01-03',NULL),
		 ('channel','channel_profile',240000,true,'2026-01-02','2099-01-02'),
		 ('channel','channel_photo',180000,true,'2026-01-05','2099-01-01'),
		 ('channel','live_snapshot',30000,true,'2026-01-01',NULL),
		 ('channel','channel_live_check',45000,true,'2026-01-01',NULL),
		 ('video','video_live_check',15000,true,'2026-01-01',NULL),
		 ('channel','community_page',300000,true,'2026-01-01',NULL),
		 ('disabled','video_list',1000,false,'2025-01-01',NULL),
		 ('channel','schedule_snapshot',1000,true,'2025-01-01',NULL),
		 ('Channel','video_list',90000,true,'2026-01-01',NULL),
		 ('é','video_list',90000,true,'2026-01-01',NULL),
		 (U&'e\0301','video_list',90000,true,'2026-01-01',NULL)
		) fixture(subject,kind,cadence,enabled,created,eligible)`)
}

func TestCollectionRuntimeBundlesExactAggregation(t *testing.T) {
	pool := NewPool(t)
	seedCollectionBundleHeaders(t, pool)
	seedCollectionBundleTargets(t, pool)
	assertCollectionRuntimeBundles(t, pool)

	var expected, actual string

	require.NoError(t, pool.QueryRow(t.Context(), `WITH expected(kind,observation_kind,rpc_per_kind) AS (`+
		collectionRuntimeKindsOracleSQL+`) SELECT
		(SELECT jsonb_agg(to_jsonb(expected) ORDER BY observation_kind COLLATE "C") FROM expected)::text,
		(SELECT jsonb_agg(to_jsonb(mapping) ORDER BY observation_kind COLLATE "C") FROM youtube_collection_runtime_kinds mapping)::text`).Scan(&expected, &actual))
	require.JSONEq(t, expected, actual)
}

func collectionBundleTupleSnapshot(t *testing.T, db collectionBundleDB) string {
	t.Helper()

	var snapshot string

	require.NoError(t, db.QueryRow(t.Context(), `SELECT coalesce(jsonb_agg(jsonb_build_array(to_jsonb(bundle),xmin::text,ctid::text)
		ORDER BY projection_generation,kind COLLATE "C",subject_key COLLATE "C"),'[]'::jsonb)::text
		FROM youtube_collection_runtime_bundles bundle`).Scan(&snapshot))

	return snapshot
}

func TestCollectionRuntimeBundlesTechnicalWritesAndReentry(t *testing.T) {
	pool := NewPool(t)
	seedCollectionBundleHeaders(t, pool)
	seedCollectionBundleTargets(t, pool)

	tx, err := pool.Begin(t.Context())
	require.NoError(t, err)

	defer rollbackLiveReviewReceiptFactsTx(t, tx)

	execCollectionBundleSQL(t, tx, `SELECT lock_youtube_collection_runtime_bundles()`)

	before := collectionBundleTupleSnapshot(t, tx)

	var guardBefore, guardAfter string

	require.NoError(t, tx.QueryRow(t.Context(), `SELECT ctid::text FROM youtube_collection_projection_guard`).Scan(&guardBefore))
	execCollectionBundleSQL(t, tx, `SELECT lock_youtube_collection_runtime_bundles();
		UPDATE youtube_collection_projection_generations SET validity_refreshed_at=now(),valid_until=now()+interval '2 hours',row_count=13;
		UPDATE youtube_collection_targets SET priority=41,member_since_generation=1;
		UPDATE youtube_collection_targets SET poll_interval_ms=poll_interval_ms,not_before=not_before;
		UPDATE youtube_collection_targets SET poll_interval_ms=250000 WHERE observation_kind='channel_profile';
		UPDATE youtube_collection_targets SET created_at='2026-01-06',not_before='2099-01-05' WHERE observation_kind='video_list' AND subject_key='channel'`)
	require.Equal(t, before, collectionBundleTupleSnapshot(t, tx), "동일한 집계 결과는 원본 의미 필드가 바뀌어도 다시 쓰지 않습니다")
	require.NoError(t, tx.QueryRow(t.Context(), `SELECT ctid::text FROM youtube_collection_projection_guard`).Scan(&guardAfter))
	require.Equal(t, guardBefore, guardAfter, "같은 트랜잭션의 guard 재진입은 새 행 버전을 만들지 않습니다")
}

func TestCollectionRuntimeBundlesImmediateChangesAndRollback(t *testing.T) {
	pool := NewPool(t)
	seedCollectionBundleHeaders(t, pool)
	seedCollectionBundleTargets(t, pool)

	before := collectionBundleTupleSnapshot(t, pool)

	for _, tc := range []struct {
		name string
		sql  string
	}{
		{"subject_key", `UPDATE youtube_collection_targets SET subject_key='moved' WHERE subject_key='channel' AND observation_kind='shorts_list'`},
		{"observation_kind", `UPDATE youtube_collection_targets SET observation_kind='channel_photo' WHERE subject_key='Channel'`},
		{"generation_key", `UPDATE youtube_collection_targets SET projection_generation=102,subject_key='moved' WHERE projection_generation=101 AND subject_key='channel'`},
		{"retired_generation_key", `UPDATE youtube_collection_targets SET projection_generation=103,subject_key='moved' WHERE projection_generation=101 AND subject_key='channel'`},
		{"upsert", `INSERT INTO youtube_collection_targets(projection_generation,subject_key,observation_kind,priority,poll_interval_ms,enabled)
			VALUES (101,'channel','shorts_list',40,1000,true),(101,'new-subject','shorts_list',40,60000,true)
			ON CONFLICT(projection_generation,subject_key,observation_kind) DO UPDATE SET poll_interval_ms=excluded.poll_interval_ms`},
		{"disable", `UPDATE youtube_collection_targets SET enabled=false WHERE observation_kind='shorts_list'`},
		{"enable", `UPDATE youtube_collection_targets SET enabled=true WHERE subject_key='disabled'`},
		{"minimums", `UPDATE youtube_collection_targets SET poll_interval_ms=1000,created_at='2025-01-01',not_before=NULL WHERE observation_kind='channel_photo'`},
		{"retire", `UPDATE youtube_collection_projection_generations SET status='RETIRED' WHERE generation=101`},
		{"activate", `UPDATE youtube_collection_projection_generations SET status='RETIRED' WHERE generation=101; UPDATE youtube_collection_projection_generations SET status='CURRENT',activated_at=now() WHERE generation=102`},
		{"restage", `UPDATE youtube_collection_projection_generations SET status='STAGING',activated_at=NULL WHERE generation=103`},
		{"target_delete", `DELETE FROM youtube_collection_targets WHERE observation_kind='shorts_list'`},
		{"last_target_delete", `DELETE FROM youtube_collection_targets WHERE subject_key='channel' AND observation_kind IN ('video_list','shorts_list')`},
		{"header_cascade", `DELETE FROM youtube_collection_projection_generations WHERE generation=101`},
		{"target_truncate", `TRUNCATE youtube_collection_targets CASCADE`},
		{"header_truncate", `TRUNCATE youtube_collection_projection_generations CASCADE`},
		{"empty_statement", `UPDATE youtube_collection_targets SET enabled=false WHERE false; DELETE FROM youtube_collection_projection_generations WHERE false`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tx, err := pool.Begin(t.Context())
			require.NoError(t, err)

			defer rollbackLiveReviewReceiptFactsTx(t, tx)

			execCollectionBundleSQL(t, tx, tc.sql)
			assertCollectionRuntimeBundles(t, tx)
			require.NoError(t, tx.Rollback(t.Context()))
			require.Equal(t, before, collectionBundleTupleSnapshot(t, pool))
		})
	}
}

func TestCollectionRuntimeBundlesExpiryRemainsQueryTime(t *testing.T) {
	pool := NewPool(t)
	seedCollectionBundleHeaders(t, pool)
	seedCollectionBundleTargets(t, pool)

	before := collectionBundleTupleSnapshot(t, pool)

	// 같은 저장 bundle을 소비하더라도 CURRENT 헤더의 유효기간은 매 문장마다 판정한다.
	const query = `SELECT count(*) FROM youtube_collection_runtime_bundles bundle
		JOIN youtube_collection_projection_generations header ON header.generation=bundle.projection_generation
		WHERE header.status='CURRENT' AND header.valid_until>statement_timestamp()`

	var count int

	require.NoError(t, pool.QueryRow(t.Context(), query).Scan(&count))
	require.Positive(t, count)
	execCollectionBundleSQL(t, pool, `UPDATE youtube_collection_projection_generations SET valid_until=clock_timestamp()+interval '100 milliseconds' WHERE status='CURRENT'`)
	require.Eventually(t, func() bool {
		err := pool.QueryRow(t.Context(), query).Scan(&count)

		return err == nil && count == 0
	}, 5*time.Second, 10*time.Millisecond)
	require.Equal(t, before, collectionBundleTupleSnapshot(t, pool))
}

func TestCollectionRuntimeBundlesSingleBundleWritePreservesPeers(t *testing.T) {
	pool := NewPool(t)
	seedCollectionBundleHeaders(t, pool)
	seedCollectionBundleTargets(t, pool)

	const peerQuery = `SELECT jsonb_agg(jsonb_build_array(to_jsonb(bundle),xmin::text,ctid::text)
		ORDER BY projection_generation,kind COLLATE "C",subject_key COLLATE "C")::text
		FROM youtube_collection_runtime_bundles bundle
		WHERE (projection_generation,kind,subject_key)<>(101,'youtubejs_content','channel')`

	var before, after string

	require.NoError(t, pool.QueryRow(t.Context(), peerQuery).Scan(&before))
	execCollectionBundleSQL(t, pool, `UPDATE youtube_collection_targets SET not_before='2099-01-01'
		WHERE projection_generation=101 AND subject_key='channel' AND observation_kind='shorts_list'`)
	require.NoError(t, pool.QueryRow(t.Context(), peerQuery).Scan(&after))
	require.Equal(t, before, after, "단일 bundle 변경은 다른 subject/kind/generation tuple을 다시 쓰지 않습니다")
	assertCollectionRuntimeBundles(t, pool)
}
