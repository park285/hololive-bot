package publishedat

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/database"
)

func validatePublishedAtResolverSchema(ctx context.Context, postgresService database.Client) error {
	if postgresService == nil {
		return fmt.Errorf("postgres service is nil")
	}
	db := postgresService.GetGormDB()
	if db == nil || db.Migrator() == nil {
		return fmt.Errorf("gorm db or migrator is nil")
	}
	migrator := db.WithContext(ctx).Migrator()
	if !migrator.HasColumn(&domain.YouTubeCommunityShortsAlarmState{}, "published_at_retry_after") {
		return fmt.Errorf("missing migration 057: youtube_community_shorts_alarm_states.published_at_retry_after")
	}
	if !migrator.HasIndex(&domain.YouTubeCommunityShortsAlarmState{}, "idx_ycsas_pending_published_at_resolution") {
		return fmt.Errorf("missing migration 056 index: idx_ycsas_pending_published_at_resolution")
	}
	if !migrator.HasIndex(&domain.YouTubeCommunityShortsAlarmState{}, "idx_ycsas_pending_published_at_retry_after") {
		return fmt.Errorf("missing migration 057 index: idx_ycsas_pending_published_at_retry_after")
	}
	return nil
}

func validatePublishedAtResolverSchemaIfEnabled(
	ctx context.Context,
	scraperCfg config.ScraperConfig,
	postgresService database.Client,
	logger *slog.Logger,
) error {
	if !effectivePublishedAtResolverConfig(scraperCfg).Enabled {
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
