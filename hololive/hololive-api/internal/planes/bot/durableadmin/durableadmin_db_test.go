package durableadmin

import (
	"bytes"
	"io"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
)

func TestRunImportsPostgresOnceThroughTheConfiguredEnvironment(t *testing.T) {
	pool := dbtest.NewPool(t)

	_, err := pool.Exec(t.Context(), `INSERT INTO public.bot_webhook_nonces (digest, expires_at)
		VALUES (sha256('admin-import'::bytea), now() + interval '10 minutes')`)
	require.NoError(t, err)

	source := pool.Config().ConnConfig
	t.Setenv("POSTGRES_HOST", source.Host)
	t.Setenv("POSTGRES_PORT", strconv.Itoa(int(source.Port)))
	t.Setenv("POSTGRES_USER", source.User)
	t.Setenv("POSTGRES_PASSWORD", source.Password)
	t.Setenv("POSTGRES_DB", source.Database)
	t.Setenv("POSTGRES_SSLMODE", "disable")
	t.Setenv("BOT_DURABLE_STORE_PATH", filepath.Join(t.TempDir(), "bot-durable", "bot-durable.db"))

	var stdout bytes.Buffer

	require.Equal(t, exitOK, Run(t.Context(), []string{"import-postgres"}, &stdout, io.Discard))
	require.Contains(t, stdout.String(), "bot_webhook_nonces/=1\n")

	stdout.Reset()

	require.Equal(t, exitFailed, Run(t.Context(), []string{"import-postgres"}, &stdout, io.Discard),
		"a second import must refuse the populated store")
	require.Empty(t, stdout.String())
}
