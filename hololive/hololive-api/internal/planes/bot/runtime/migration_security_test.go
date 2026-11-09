package botruntime

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
)

func TestBaselinePreservesUnconfiguredACLAndRoomUniqueness(t *testing.T) {
	t.Parallel()

	pool := dbtest.NewPool(t)
	ctx := t.Context()

	var settings, rooms int

	require.NoError(t, pool.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM acl_settings), (SELECT count(*) FROM acl_rooms)`).Scan(&settings, &rooms))
	require.Zero(t, settings, "migration은 운영 ACL 정책을 임의로 켜지 않는다")
	require.Zero(t, rooms, "migration은 허용·차단 방을 임의로 등록하지 않는다")

	var listType string

	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO acl_rooms(room_id) VALUES ('42') RETURNING list_type`).Scan(&listType))
	require.Equal(t, "whitelist", listType)

	_, err := pool.Exec(ctx, `INSERT INTO acl_rooms(room_id) VALUES ('42')`)
	pgErr, ok := errors.AsType[*pgconn.PgError](err)

	if !ok || pgErr == nil {
		t.Fatalf("중복 ACL 방은 PostgreSQL unique 위반이어야 함: %v", err)
	}

	require.Equal(t, "23505", pgErr.Code)

	_, err = pool.Exec(ctx, `INSERT INTO acl_rooms(room_id,list_type) VALUES ('42','blacklist')`)
	require.NoError(t, err, "같은 방의 서로 다른 목록은 독립 identity다")
}
