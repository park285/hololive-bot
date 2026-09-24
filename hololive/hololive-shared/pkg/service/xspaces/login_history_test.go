package xspaces

import (
	"bytes"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
)

func TestLoginStatusPreservesHistoricalReadOnlyResult(t *testing.T) {
	t.Parallel()

	store, err := NewStore(dbtest.NewPool(t), bytes.Repeat([]byte{3}, 32))
	require.NoError(t, err)

	status, err := store.LoginStatus(t.Context())
	require.NoError(t, err)
	require.Equal(t, "disabled", status.State)

	started := time.Now().UTC().Add(-time.Minute).Truncate(time.Microsecond)

	_, err = store.pool.Exec(t.Context(), `INSERT INTO x_space_login_attempts
		(configuration_revision, session_revision, status, error_code, started_at)
		VALUES (1, 0, 'login_required', 'additional_authentication', $1)`, started)
	require.NoError(t, err)

	status, err = store.LoginStatus(t.Context())
	require.NoError(t, err)
	require.Equal(t, "login_required", status.State)
	require.Equal(t, "additional_authentication", status.LastError)
	require.NotNil(t, status.LastAttemptAt)
	require.WithinDuration(t, started, *status.LastAttemptAt, time.Microsecond)

	_, err = store.pool.Exec(t.Context(), `INSERT INTO x_space_login_attempts
		(configuration_revision, session_revision, status, started_at)
		VALUES (1, 1, 'running', now() - interval '6 minutes')`)
	require.NoError(t, err)

	status, err = store.LoginStatus(t.Context())
	require.NoError(t, err)
	require.Equal(t, "outcome_unknown", status.State)
	require.Equal(t, "interrupted", status.LastError)
}
