package publishedat

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/service/database"
)

func validatePublishedAtResolverSchema(ctx context.Context, postgresService database.Client) error {
	if postgresService == nil {
		return fmt.Errorf("postgres service is nil")
	}
	pool := postgresService.GetPool()
	if pool == nil {
		return fmt.Errorf("postgres pool is nil")
	}
	columnExists, err := publishedAtResolverColumnExists(ctx, pool, "youtube_community_shorts_alarm_states", "published_at_retry_after")
	if err != nil {
		return err
	}
	if !columnExists {
		return fmt.Errorf("missing migration 057: youtube_community_shorts_alarm_states.published_at_retry_after")
	}
	indexExists, err := publishedAtResolverIndexExists(ctx, pool, "idx_ycsas_pending_published_at_resolution")
	if err != nil {
		return err
	}
	if !indexExists {
		return fmt.Errorf("missing migration 056 index: idx_ycsas_pending_published_at_resolution")
	}
	indexExists, err = publishedAtResolverIndexExists(ctx, pool, "idx_ycsas_pending_published_at_retry_after")
	if err != nil {
		return err
	}
	if !indexExists {
		return fmt.Errorf("missing migration 057 index: idx_ycsas_pending_published_at_retry_after")
	}
	return nil
}

type publishedAtResolverSchemaQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func publishedAtResolverColumnExists(ctx context.Context, db publishedAtResolverSchemaQuerier, tableName, columnName string) (bool, error) {
	var exists bool
	if err := db.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM information_schema.columns
			WHERE table_schema = current_schema()
			  AND table_name = $1
			  AND column_name = $2
		)
	`, tableName, columnName).Scan(&exists); err != nil {
		return false, fmt.Errorf("check published_at resolver column: %w", err)
	}
	return exists, nil
}

func publishedAtResolverIndexExists(ctx context.Context, db publishedAtResolverSchemaQuerier, indexName string) (bool, error) {
	var exists bool
	if err := db.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM pg_indexes
			WHERE schemaname = current_schema()
			  AND indexname = $1
		)
	`, indexName).Scan(&exists); err != nil {
		return false, fmt.Errorf("check published_at resolver index: %w", err)
	}
	return exists, nil
}

func validatePublishedAtResolverSchemaIfEnabled(
	ctx context.Context,
	scraperConfig *config.ScraperConfig,
	postgresService database.Client,
	logger *slog.Logger,
) error {
	if !effectivePublishedAtResolverConfig(scraperConfig).Enabled {
		return nil
	}
	if err := validatePublishedAtResolverSchema(ctx, postgresService); err != nil {
		return err
	}
	if logger != nil {
		logger.Info("published_at_resolver_schema_validated")
	}
	return nil
}
