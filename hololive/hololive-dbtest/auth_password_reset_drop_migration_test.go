package dbtest

import (
	"testing"

	"github.com/stretchr/testify/require"
)

const authResetTableDropMigration = "281_drop_auth_password_reset_tokens.sql"

// 281 적용 전 운영 형태(baseline DDL)의 재설정 토큰 테이블이다.
const authResetTableSQL = `
CREATE TABLE auth_password_reset_tokens (
	token_hash text NOT NULL PRIMARY KEY,
	user_id text NOT NULL REFERENCES auth_users(id) ON DELETE CASCADE,
	expires_at timestamp with time zone NOT NULL,
	used_at timestamp with time zone,
	created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL
);
CREATE INDEX idx_auth_reset_tokens_user_unused ON auth_password_reset_tokens USING btree (user_id) WHERE (used_at IS NULL);
`

// 토큰 행이 남아 있으면 지우지 않고 실패하며, 비면 테이블과 인덱스를 지우고 재적용은 아무것도 바꾸지 않는다.
func TestAuthPasswordResetDropMigrationKeepsNonEmptyTableAndDropsEmptyOne(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	pool := NewPool(t)
	dir, err := resolveMigrationsDir()
	require.NoError(t, err)

	_, err = pool.Exec(ctx, authResetTableSQL+`
		INSERT INTO auth_users (id, email, password_hash, display_name) VALUES ('reset-user', 'reset@example.com', 'hash', 'Reset');
		INSERT INTO auth_password_reset_tokens (token_hash, user_id, expires_at) VALUES ('token-hash', 'reset-user', now());`)
	require.NoError(t, err)

	err = applyMigrationFile(ctx, pool, dir, authResetTableDropMigration)
	requireSQLState(t, err, sqlStateRaiseException)
	require.ErrorContains(t, err, "retired table public.auth_password_reset_tokens has rows; table kept")

	var kept bool

	require.NoError(t, pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM auth_password_reset_tokens WHERE token_hash = 'token-hash')`).Scan(&kept))
	require.True(t, kept)

	_, err = pool.Exec(ctx, `DELETE FROM auth_password_reset_tokens`)
	require.NoError(t, err)

	for _, attempt := range []string{"first", "reapply"} {
		require.NoError(t, applyMigrationFile(ctx, pool, dir, authResetTableDropMigration), attempt)

		var tableExists, indexExists, userKept bool

		require.NoError(t, pool.QueryRow(ctx, `SELECT
			to_regclass('public.auth_password_reset_tokens') IS NOT NULL,
			to_regclass('public.idx_auth_reset_tokens_user_unused') IS NOT NULL,
			EXISTS (SELECT 1 FROM auth_users WHERE id = 'reset-user')`).Scan(&tableExists, &indexExists, &userKept))
		require.False(t, tableExists, attempt)
		require.False(t, indexExists, attempt)
		require.True(t, userKept, "dropping the referencing table must not touch auth_users")
	}
}
