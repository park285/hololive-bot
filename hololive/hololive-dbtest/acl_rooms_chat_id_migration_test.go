package dbtest

import (
	"testing"

	"github.com/stretchr/testify/require"
)

const aclRoomsChatIDMigration = "349_acl_rooms_room_id_chat_id.sql"

// 343은 비정규 room_id 행만 지우고 정규 chatID 행은 남기며, 이후 비정규 값 INSERT를 CHECK로 막는다. 재적용은 바꾸지 않는다.
func TestACLRoomsChatIDMigrationDropsNonCanonicalRowsAndRejectsNewOnes(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	pool := NewPool(t)
	dir, err := resolveMigrationsDir()
	require.NoError(t, err)

	// 349 적용 전 형태로 되돌려 옛 방 이름·앞자리 0·i64 범위 밖 값을 넣는다.
	_, err = pool.Exec(ctx, `
		ALTER TABLE acl_rooms DROP CONSTRAINT chk_acl_rooms_room_id_chat_id;
		INSERT INTO acl_rooms (room_id, list_type) VALUES
			('홀로라이브 알림방', 'whitelist'), ('0123', 'whitelist'), ('-0', 'blacklist'),
			('9223372036854775808', 'whitelist'), ('-9223372036854775808', 'whitelist'),
			('9223372036854775807', 'blacklist'), ('0', 'whitelist'), ('-42', 'blacklist');`)
	require.NoError(t, err)

	for _, attempt := range []string{"first", "reapply"} {
		require.NoError(t, applyMigrationFile(ctx, pool, dir, aclRoomsChatIDMigration), attempt)

		rows, queryErr := pool.Query(ctx, `SELECT room_id FROM acl_rooms ORDER BY room_id`)
		require.NoError(t, queryErr)

		var kept []string

		for rows.Next() {
			var roomID string

			require.NoError(t, rows.Scan(&roomID))

			kept = append(kept, roomID)
		}

		require.NoError(t, rows.Err())
		require.Equal(t, []string{"-42", "-9223372036854775808", "0", "9223372036854775807"}, kept, attempt)
	}

	var validated bool

	require.NoError(t, pool.QueryRow(ctx, `SELECT convalidated FROM pg_constraint
		WHERE conrelid = 'acl_rooms'::regclass AND conname = 'chk_acl_rooms_room_id_chat_id'`).Scan(&validated))
	require.True(t, validated)

	_, err = pool.Exec(ctx, `INSERT INTO acl_rooms (room_id, list_type) VALUES ('room-a', 'whitelist')`)
	requireSQLState(t, err, sqlStateCheckViolation)
}
