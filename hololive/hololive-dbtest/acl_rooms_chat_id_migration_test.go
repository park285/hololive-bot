package dbtest

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestACLRoomsChatIDShape(t *testing.T) {
	t.Parallel()

	pool := NewPool(t)

	for _, roomID := range []string{"-9223372036854775808", "9223372036854775807", "0", "-42"} {
		_, err := pool.Exec(t.Context(), `INSERT INTO acl_rooms(room_id,list_type) VALUES ($1,'whitelist')`, roomID)
		require.NoError(t, err)
	}

	for _, roomID := range []string{"홀로라이브 알림방", "0123", "-0", "9223372036854775808", "room-a"} {
		_, err := pool.Exec(t.Context(), `INSERT INTO acl_rooms(room_id,list_type) VALUES ($1,'whitelist')`, roomID)
		requireSQLState(t, err, sqlStateCheckViolation)
	}
}
