package dbtest

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
)

func TestWebhookNoncePrivilegesOverrideBroadDefaults(t *testing.T) {
	pool := NewBlankPool(t)
	roles := createObservationGrantRoles(t, pool)
	grantBroadBaselineDefaults(t, pool, roles)
	applyBaselineForRoles(t, pool, roles)

	const reserve = `SELECT public.reserve_bot_webhook_nonce(sha256('grant-test'::bytea), 600000000)`

	ctx := t.Context()
	execAs := func(role, sql string) error {
		return pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, "SET LOCAL ROLE "+pgx.Identifier{role}.Sanitize()); err != nil {
				t.Fatalf("set fixture role: %v", err)
			}

			_, err := tx.Exec(ctx, sql)

			return err
		})
	}

	require.NoError(t, execAs(roles.runtime, reserve))

	for _, check := range []struct {
		role string
		sql  string
	}{
		{roles.runtime, `SELECT digest FROM public.bot_webhook_nonces`},
		{roles.runtime, `UPDATE public.bot_webhook_nonces SET expires_at=now()`},
		{roles.runtime, `DELETE FROM public.bot_webhook_nonces`},
		{roles.runtime, `TRUNCATE public.bot_webhook_nonces`},
		{roles.scraper, reserve},
	} {
		err := execAs(check.role, check.sql)
		pgErr, ok := errors.AsType[*pgconn.PgError](err)
		require.True(t, ok, "expected a PostgreSQL error, got %v", err)
		require.Equal(t, "42501", pgErr.Code)
	}
}
