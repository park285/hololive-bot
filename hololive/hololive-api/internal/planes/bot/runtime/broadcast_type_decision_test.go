package botruntime

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	appbootstrap "github.com/kapu/hololive-api/internal/planes/bot/internal/app/bootstrap"
	databasemocks "github.com/kapu/hololive-shared/pkg/service/database/mocks"
)

func TestNewBroadcastTypeDecisionWorkerRequiresValidKeyAndPool(t *testing.T) {
	t.Parallel()

	pool, err := pgxpool.New(t.Context(), "postgres://broadcast-type-test.invalid/db")
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	infra := &appbootstrap.BotInfrastructure{Postgres: &databasemocks.Client{GetPoolFunc: func() *pgxpool.Pool { return pool }}}

	dir := t.TempDir()
	keyFile := filepath.Join(dir, "key")
	junkFile := filepath.Join(dir, "junk")

	const secret = "test-key-0123456789abcdef"

	require.NoError(t, os.WriteFile(keyFile, []byte(secret+"\n"), 0o600))
	require.NoError(t, os.WriteFile(junkFile, []byte("GEMINI_API_KEY="+secret), 0o600))

	worker, err := newBroadcastTypeDecisionWorker(keyFile, infra, testBootstrapGuardLogger())
	require.NoError(t, err)
	require.NotNil(t, worker)

	for name, path := range map[string]string{"relative": "key", "missing": filepath.Join(dir, "absent"), "junk": junkFile} {
		_, loadErr := newBroadcastTypeDecisionWorker(path, infra, testBootstrapGuardLogger())
		require.Error(t, loadErr, name)
		require.NotContains(t, loadErr.Error(), secret, name)
	}

	_, err = newBroadcastTypeDecisionWorker(keyFile, &appbootstrap.BotInfrastructure{}, testBootstrapGuardLogger())
	require.Error(t, err)
}
