package publishedat

import (
	"bytes"
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/dbtest"
	databasemocks "github.com/kapu/hololive-shared/pkg/service/database/mocks"
)

func TestValidatePublishedAtResolverSchema_PassesWhenColumnExists(t *testing.T) {
	pool := dbtest.NewPool(t)

	err := validatePublishedAtResolverSchema(context.Background(), &databasemocks.Client{
		GetPoolFunc: func() *pgxpool.Pool { return pool },
	})
	require.NoError(t, err)
}

func TestValidatePublishedAtResolverSchema_FailsWhenColumnMissing(t *testing.T) {
	pool := dbtest.NewPool(t)
	dropPublishedAtResolverIndex(t, pool, "idx_ycsas_pending_published_at_retry_after")
	_, err := pool.Exec(t.Context(), `ALTER TABLE youtube_community_shorts_alarm_states DROP COLUMN published_at_retry_after`)
	require.NoError(t, err)

	err = validatePublishedAtResolverSchema(context.Background(), &databasemocks.Client{
		GetPoolFunc: func() *pgxpool.Pool { return pool },
	})
	require.ErrorContains(t, err, "missing migration 057")
	require.ErrorContains(t, err, "published_at_retry_after")
}

func TestValidatePublishedAtResolverSchema_FailsWhenPendingResolutionIndexMissing(t *testing.T) {
	pool := dbtest.NewPool(t)
	dropPublishedAtResolverIndex(t, pool, "idx_ycsas_pending_published_at_resolution")

	err := validatePublishedAtResolverSchema(context.Background(), &databasemocks.Client{
		GetPoolFunc: func() *pgxpool.Pool { return pool },
	})
	require.ErrorContains(t, err, "missing migration 056 index")
}

func TestValidatePublishedAtResolverSchema_FailsWhenRetryAfterIndexMissing(t *testing.T) {
	pool := dbtest.NewPool(t)
	dropPublishedAtResolverIndex(t, pool, "idx_ycsas_pending_published_at_retry_after")

	err := validatePublishedAtResolverSchema(context.Background(), &databasemocks.Client{
		GetPoolFunc: func() *pgxpool.Pool { return pool },
	})
	require.ErrorContains(t, err, "missing migration 057 index")
}

func TestValidatePublishedAtResolverSchemaIfEnabled_SkipsWhenResolverDisabled(t *testing.T) {
	var logBuf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logBuf, nil))

	err := validatePublishedAtResolverSchemaIfEnabled(
		context.Background(),
		config.ScraperConfig{
			PublishedAtResolver: config.ScraperPublishedAtResolverConfig{
				Enabled:  false,
				Interval: 15 * time.Second,
			},
		},
		&databasemocks.Client{},
		logger,
	)
	require.NoError(t, err)
	assert.NotContains(t, logBuf.String(), `"msg":"published_at_resolver_schema_validated"`)
}

func TestValidatePublishedAtResolverSchemaIfEnabled_LogsWhenResolverEnabled(t *testing.T) {
	pool := dbtest.NewPool(t)

	var logBuf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logBuf, nil))

	err := validatePublishedAtResolverSchemaIfEnabled(
		context.Background(),
		config.ScraperConfig{
			PublishedAtResolver: config.ScraperPublishedAtResolverConfig{
				Enabled:  true,
				Interval: 15 * time.Second,
			},
		},
		&databasemocks.Client{
			GetPoolFunc: func() *pgxpool.Pool { return pool },
		},
		logger,
	)
	require.NoError(t, err)
	assert.Contains(t, logBuf.String(), `"msg":"published_at_resolver_schema_validated"`)
}

func dropPublishedAtResolverIndex(t *testing.T, pool *pgxpool.Pool, name string) {
	t.Helper()

	_, err := pool.Exec(t.Context(), `DROP INDEX IF EXISTS `+name)
	require.NoError(t, err)
}
