package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/park285/shared-go/postgres/pgxdb"
	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
)

func TestFrozenRequestInsertReturnsStoredSnapshotWithoutRewrite(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	pool := dbtest.NewPool(t)
	request := frozenInsertTestRequest()

	first, err := insertFrozenRequestAsWorker(ctx, t, pool, request)
	require.NoError(t, err)

	sum := sha256.Sum256([]byte(request.Message))
	request.MessageHash = hex.EncodeToString(sum[:])
	require.Equal(t, request, first)

	_, err = pool.Exec(ctx, `UPDATE youtube_notification_send_request SET generation = 1 WHERE base_id = $1`, request.BaseID)
	require.NoError(t, err)

	var before, after string

	require.NoError(t, pool.QueryRow(ctx, `SELECT xmin::text || ':' || ctid::text FROM youtube_notification_send_request WHERE base_id = $1`, request.BaseID).Scan(&before))

	repeated, err := insertFrozenRequestAsWorker(ctx, t, pool, request)
	require.NoError(t, err)

	first.Generation = 1
	require.Equal(t, first, repeated)
	require.NoError(t, pool.QueryRow(ctx, `SELECT xmin::text || ':' || ctid::text FROM youtube_notification_send_request WHERE base_id = $1`, request.BaseID).Scan(&after))
	require.Equal(t, before, after)
	requireFrozenInsertRowCount(t, pool)
}

func TestFrozenRequestInsertRejectsConflictingPayload(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	first, err := insertFrozenRequestAsWorker(ctx, t, pool, frozenInsertTestRequest())
	require.NoError(t, err)

	cases := []struct {
		name   string
		change func(*FrozenRequest)
	}{
		{name: "room", change: func(r *FrozenRequest) { r.RoomID = "another-room" }},
		{name: "message", change: func(r *FrozenRequest) { r.Message = "different body" }},
		{name: "route", change: func(r *FrozenRequest) { r.Route = "richtext" }},
		{name: "members", change: func(r *FrozenRequest) { r.MemberIDs = []int64{1, 3} }},
		{name: "member order", change: func(r *FrozenRequest) { r.MemberIDs = []int64{2, 1} }},
		{name: "dedupe keys", change: func(r *FrozenRequest) { r.DedupeKeys = []string{"other-key"} }},
		{name: "dedupe order", change: func(r *FrozenRequest) { r.DedupeKeys = []string{"key-b", "key-a"} }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			candidate := frozenInsertTestRequest()
			tc.change(&candidate)

			_, err := insertFrozenRequestAsWorker(t.Context(), t, pool, candidate)
			require.ErrorContains(t, err, "existing request collision")

			stored, err := loadFrozenRequest(t.Context(), pool, first.BaseID)
			require.NoError(t, err)
			require.Equal(t, first, stored)
			requireFrozenInsertRowCount(t, pool)
		})
	}
}

func TestFrozenRequestInsertRejectsCorruptStoredHash(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	pool := dbtest.NewPool(t)
	request := frozenInsertTestRequest()
	_, err := insertFrozenRequestAsWorker(ctx, t, pool, request)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `UPDATE youtube_notification_send_request SET message_hash = repeat('0', 64) WHERE base_id = $1`, request.BaseID)
	require.NoError(t, err)

	_, err = insertFrozenRequestAsWorker(ctx, t, pool, request)
	require.ErrorContains(t, err, "message hash mismatch")
	requireFrozenInsertRowCount(t, pool)
}

func TestFrozenRequestInsertWaitsForConcurrentInsert(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		commit   bool
		conflict bool
	}{
		{name: "commit identical", commit: true},
		{name: "commit collision", commit: true, conflict: true},
		{name: "rollback identical"},
		{name: "rollback different payload", conflict: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			testFrozenRequestConcurrentInsert(t, tc.commit, tc.conflict)
		})
	}
}

func testFrozenRequestConcurrentInsert(t *testing.T, commit, conflict bool) {
	t.Helper()

	pool := dbtest.NewPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	firstTx := beginFrozenInsertWorkerTx(ctx, t, pool)
	defer rollbackPlanModeSQLTest(t, firstTx)

	request := frozenInsertTestRequest()
	first, err := insertFrozenRequest(ctx, firstTx, request)
	require.NoError(t, err)

	secondTx := beginFrozenInsertWorkerTx(ctx, t, pool)
	defer rollbackPlanModeSQLTest(t, secondTx)

	if conflict {
		request.Message = "competing body"
	}

	type result struct {
		request FrozenRequest
		err     error
	}

	done := make(chan result, 1)

	var workers sync.WaitGroup

	workers.Go(func() {
		frozen, insertErr := insertFrozenRequest(ctx, secondTx, request)
		done <- result{request: frozen, err: insertErr}
	})

	defer func() {
		cancel()
		workers.Wait()
	}()

	waitFrozenInsertBlocked(ctx, t, pool, firstTx, secondTx)

	if commit {
		require.NoError(t, firstTx.Commit(ctx))
	} else {
		require.NoError(t, pgxdb.Rollback(ctx, firstTx))
	}

	var second result

	select {
	case second = <-done:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}

	expected := first

	if commit && conflict {
		require.ErrorContains(t, second.err, "existing request collision")
		require.NoError(t, pgxdb.Rollback(ctx, secondTx))
	} else {
		require.NoError(t, second.err)
		require.NoError(t, secondTx.Commit(ctx))

		if !commit {
			sum := sha256.Sum256([]byte(request.Message))
			request.MessageHash = hex.EncodeToString(sum[:])
			expected = request
		}

		require.Equal(t, expected, second.request)
	}

	stored, err := loadFrozenRequest(ctx, pool, request.BaseID)
	require.NoError(t, err)
	require.Equal(t, expected, stored)
	requireFrozenInsertRowCount(t, pool)
}

func waitFrozenInsertBlocked(ctx context.Context, t *testing.T, pool *pgxpool.Pool, first, second pgx.Tx) {
	t.Helper()

	firstPID := int64(first.Conn().PgConn().PID())
	secondPID := int64(second.Conn().PgConn().PID())
	require.Eventually(t, func() bool {
		var blocked bool

		err := pool.QueryRow(ctx, `SELECT $1::int = ANY(pg_blocking_pids($2::int))`, firstPID, secondPID).Scan(&blocked)

		return err == nil && blocked
	}, 5*time.Second, 10*time.Millisecond)
}

func frozenInsertTestRequest() FrozenRequest {
	return FrozenRequest{
		BaseID: "youtube:pg19-frozen", RoomID: "pg19-room", Message: "original frozen body",
		Route: testFrozenTextRoute, DedupeKeys: []string{"key-a", "key-b"}, MemberIDs: []int64{1, 2},
	}
}

func beginFrozenInsertWorkerTx(ctx context.Context, t *testing.T, pool *pgxpool.Pool) pgx.Tx {
	t.Helper()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)

	if _, err = tx.Exec(ctx, "SET LOCAL ROLE hololive_alarm_worker"); err != nil {
		rollbackPlanModeSQLTest(t, tx)
		t.Fatalf("set frozen request worker role: %v", err)
	}

	return tx
}

func insertFrozenRequestAsWorker(ctx context.Context, t *testing.T, pool *pgxpool.Pool, request FrozenRequest) (FrozenRequest, error) {
	t.Helper()

	tx := beginFrozenInsertWorkerTx(ctx, t, pool)
	defer rollbackPlanModeSQLTest(t, tx)

	frozen, err := insertFrozenRequest(ctx, tx, request)
	if err != nil {
		return FrozenRequest{}, err
	}

	return frozen, tx.Commit(ctx)
}

func requireFrozenInsertRowCount(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	var count int

	require.NoError(t, pool.QueryRow(t.Context(), `SELECT count(*) FROM youtube_notification_send_request`).Scan(&count))
	require.Equal(t, 1, count)
}
