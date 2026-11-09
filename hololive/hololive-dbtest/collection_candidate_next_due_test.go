package dbtest

import (
	"database/sql"
	"io/fs"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

const (
	candidateNextDueMigration = "005_youtube_collection_candidate_next_due.sql"
	candidateHintJobKey       = "collector:youtubejs:youtubejs_content:channel:a"
)

const subjectHintQuery = `SELECT is_current,subject_key,min_interval_ms,max_interval_ms,next_due_at FROM public.youtube_collection_job_candidates(
	101, ARRAY['video_list','shorts_list'], 'youtubejs', 'youtubejs_content', $1::text[], 1)`

const globalHintQuery = `SELECT is_current,subject_key,min_interval_ms,max_interval_ms,next_due_at FROM public.youtube_collection_job_global_candidates(
	101, ARRAY['video_list','shorts_list'], $2, 'channel:a',
	'collector:youtubejs:youtubejs_content:channel:a', $1::text[])`

type candidateHintCase struct {
	name           string
	state          string
	admission      time.Duration
	due            time.Duration
	want           time.Duration
	excluded       bool
	disabled       bool
	secondAdmitted bool
	candidate      bool
}

func TestCollectionCandidateNextDueBoundaries(t *testing.T) {
	pool := NewPool(t)

	for _, global := range []bool{false, true} {
		for _, tc := range []candidateHintCase{
			{name: "후보와 미래 bundle 없음", disabled: true},
			{name: "지금 due는 힌트에서 제외", candidate: true},
			{name: "not_before 미입장", admission: time.Hour, want: time.Hour},
			{name: "제외 키의 미래 due 무시", admission: time.Hour, excluded: true},
			{name: "IDLE next_due", state: "IDLE", due: 30 * time.Second, want: 30 * time.Second},
			{name: "IDLE 단축 주기", state: "IDLE", due: time.Hour, want: 2 * time.Minute},
			{name: "DEFERRED retry_not_before", state: "DEFERRED", due: 40 * time.Second, want: 40 * time.Second},
			{name: "ACTIVE lease_expires_at", state: "ACTIVE", due: 50 * time.Second, want: 50 * time.Second},
			{name: "입장이 lease보다 늦음", state: "ACTIVE", due: time.Minute, admission: time.Hour, want: time.Hour},
			{name: "lease가 입장보다 늦음", state: "DEFERRED", due: time.Hour, admission: time.Minute, want: time.Hour},
			{name: "bundle 하나만 입장해도 후보", admission: time.Hour, secondAdmitted: true, candidate: true},
			{name: "만료 ACTIVE는 후보", state: "ACTIVE", due: -time.Second, candidate: true},
			{name: "기한 경과 DEFERRED는 후보", state: "DEFERRED", due: -time.Second, candidate: true},
			{name: "기한 경과 IDLE는 후보", state: "IDLE", due: -time.Second, candidate: true},
		} {
			name := "subject/" + tc.name

			if global {
				name = "global/" + tc.name
			}

			t.Run(name, func(t *testing.T) {
				tx, err := pool.Begin(t.Context())
				require.NoError(t, err)

				defer rollbackLiveReviewReceiptFactsTx(t, tx)

				checkCandidateHint(t, tx, global, tc)
			})
		}
	}
}

func checkCandidateHint(t *testing.T, tx pgx.Tx, global bool, tc candidateHintCase) {
	t.Helper()
	seedCollectionBundleHeaders(t, tx)

	var now time.Time

	require.NoError(t, tx.QueryRow(t.Context(), `SELECT statement_timestamp()`).Scan(&now))

	var admission any

	if tc.admission != 0 {
		admission = now.Add(tc.admission)
	}

	_, err := tx.Exec(t.Context(), `INSERT INTO youtube_collection_targets
		(projection_generation,subject_key,observation_kind,priority,poll_interval_ms,enabled,not_before,member_since_generation)
		VALUES (101,'channel:a','video_list',40,120000,$1,$2,101)`, !tc.disabled, admission)
	require.NoError(t, err)

	if tc.secondAdmitted {
		execCollectionBundleSQL(t, tx, `INSERT INTO youtube_collection_targets
			(projection_generation,subject_key,observation_kind,priority,poll_interval_ms,enabled,member_since_generation)
			VALUES (101,'channel:a','shorts_list',40,120000,true,101)`)
	}

	if tc.state != "" {
		seedCandidateHintLease(t, tx, tc.state, now, now.Add(tc.due))
	}

	excluded := []string{}
	if tc.excluded {
		excluded = append(excluded, candidateHintJobKey)
	}

	query := subjectHintQuery
	args := []any{excluded}

	if global {
		query = globalHintQuery

		args = append(args, true)
	}

	var (
		current      bool
		subject      sql.NullString
		minMS, maxMS sql.NullInt64
		hint         sql.NullTime
	)

	require.NoError(t, tx.QueryRow(t.Context(), query, args...).Scan(&current, &subject, &minMS, &maxMS, &hint))
	require.True(t, current)
	require.Equal(t, tc.candidate, subject.Valid)

	if tc.candidate {
		require.Equal(t, int64(120000), minMS.Int64)
		require.Equal(t, minMS, maxMS)
	}

	require.Equal(t, tc.want != 0, hint.Valid)

	if hint.Valid {
		require.True(t, hint.Time.Equal(now.Add(tc.want)), "힌트=%s 기대=%s", hint.Time, now.Add(tc.want))
	}
}

func seedCandidateHintLease(t *testing.T, db collectionBundleDB, state string, scheduled, due time.Time) {
	t.Helper()

	_, err := db.Exec(t.Context(), `INSERT INTO youtube_collection_job_leases
		(job_key,provider,job_class,collection_job_kind,subject_key,projection_generation,poll_interval_ms,
		 slot_state,scheduled_for,next_due_at,retry_not_before,owner_instance,lease_expires_at)
		VALUES ($1,'youtubejs','SUBJECT','youtubejs_content','channel:a',101,120000,$2,$3,$4,
		 CASE WHEN $2='DEFERRED' THEN $4::timestamptz END,
		 CASE WHEN $2='ACTIVE' THEN 'collector-test' END,
		 CASE WHEN $2='ACTIVE' THEN $4::timestamptz END)`, candidateHintJobKey, state, scheduled, due)
	require.NoError(t, err)
}

func TestCollectionCandidateNextDueAcrossBundles(t *testing.T) {
	t.Parallel()

	pool := NewPool(t)
	seedCollectionBundleHeaders(t, pool)
	execCollectionBundleSQL(t, pool, `INSERT INTO youtube_collection_targets
		(projection_generation,subject_key,observation_kind,priority,poll_interval_ms,enabled,not_before,member_since_generation)
		VALUES (101,'channel:a','video_list',40,120000,true,NULL,101),
		       (101,'channel:b','video_list',40,120000,true,'2099-01-02',101),
		       (101,'channel:c','video_list',40,120000,true,'2099-01-01',101),
		       (101,'disabled','video_list',40,120000,false,'2098-01-01',101)`)

	var (
		hint  time.Time
		count int
	)

	require.NoError(t, pool.QueryRow(t.Context(), `SELECT count(subject_key), min(next_due_at) FROM
		public.youtube_collection_job_candidates(101,ARRAY['video_list'],'youtubejs','youtubejs_content',
		ARRAY['collector:youtubejs:youtubejs_content:channel:c'],1)`).Scan(&count, &hint))
	require.Equal(t, 1, count)
	require.Equal(t, time.Date(2099, time.January, 2, 0, 0, 0, 0, time.UTC), hint.UTC())

	var globalHint sql.NullTime

	require.NoError(t, pool.QueryRow(t.Context(), `SELECT next_due_at FROM
		public.youtube_collection_job_global_candidates(101,ARRAY['video_list'],false,'global',
		'collector:holodex:holodex_live:global',ARRAY[]::text[])`).Scan(&globalHint))
	require.False(t, globalHint.Valid, "이미 입장한 전역 bundle은 미래 target이 있어도 지금 후보입니다")
}

func TestCollectionCandidateNextDueMigrationPreservesPreparedReaders(t *testing.T) {
	t.Parallel()

	pool := NewBlankPool(t)
	dir, err := resolveMigrationsDir()
	require.NoError(t, err)
	require.NoError(t, ensureDBTestMigrationLedger(t.Context(), pool))
	require.NoError(t, applyManifestMigration(t.Context(), pool, dir, epoch3BaselineMigration))

	migration, err := fs.ReadFile(os.DirFS(dir), candidateNextDueMigration)
	require.NoError(t, err)

	seedCollectionBundleHeaders(t, pool)
	seedCollectionBundleTargets(t, pool)

	conn, err := pool.Acquire(t.Context())
	require.NoError(t, err)

	defer conn.Release()

	queries := []string{
		`SELECT is_current,subject_key,min_interval_ms,max_interval_ms FROM public.youtube_collection_job_candidates(
		101,ARRAY['video_list','shorts_list'],'youtubejs','youtubejs_content',ARRAY[]::text[],1)`,
		`SELECT is_current,subject_key,min_interval_ms,max_interval_ms FROM public.youtube_collection_job_global_candidates(
		101,ARRAY['video_list','shorts_list'],false,'global','collector:holodex:holodex_live:global',ARRAY[]::text[])`,
	}

	var before [2][][]any

	for i, query := range queries {
		name := []string{"old_subject", "old_global"}[i]

		_, err = conn.Conn().Prepare(t.Context(), name, query)
		require.NoError(t, err)

		rows, queryErr := conn.Query(t.Context(), name)
		require.NoError(t, queryErr)

		before[i], err = pgx.CollectRows(rows, func(row pgx.CollectableRow) ([]any, error) { return row.Values() })
		require.NoError(t, err)
	}

	for range 2 {
		require.NoError(t, applyMigrationContent(t.Context(), pool, candidateNextDueMigration, string(migration)))

		for i, name := range []string{"old_subject", "old_global"} {
			rows, queryErr := conn.Query(t.Context(), name)
			require.NoError(t, queryErr)

			after, collectErr := pgx.CollectRows(rows, func(row pgx.CollectableRow) ([]any, error) { return row.Values() })
			require.NoError(t, collectErr)
			require.Equal(t, before[i], after, "기존 prepared query의 후보·정렬·LIMIT+1 결과 보존")
		}
	}
}
