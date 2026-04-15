package ops

import (
	"context"
	"fmt"
	"log/slog"

	"gorm.io/gorm"

	"github.com/kapu/hololive-shared/pkg/config"
	sharedproviders "github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/service/database"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox"
	trackingrepo "github.com/kapu/hololive-shared/pkg/service/youtube/tracking"
)

type communityShortsOpsSession struct {
	postgres           database.Client
	db                 *gorm.DB
	trackingRepository *trackingrepo.GormRepository
	telemetryRepo      *outbox.DeliveryTelemetryRepository
}

func openCommunityShortsOpsSession(
	ctx context.Context,
	cfg *config.Config,
	logger *slog.Logger,
) (*communityShortsOpsSession, func(), error) {
	if cfg == nil {
		return nil, nil, fmt.Errorf("config is nil")
	}

	databaseResources, cleanupDB, err := sharedproviders.ProvideDatabaseResources(ctx, cfg.Postgres, logger)
	if err != nil {
		return nil, nil, fmt.Errorf("provide database resources: %w", err)
	}

	session := newCommunityShortsOpsSession(databaseResources.Service.GetGormDB())
	session.postgres = databaseResources.Service
	return session, cleanupDB, nil
}

func newCommunityShortsOpsSession(db *gorm.DB) *communityShortsOpsSession {
	return &communityShortsOpsSession{
		postgres:           nil,
		db:                 db,
		trackingRepository: trackingrepo.NewRepository(db),
		telemetryRepo:      outbox.NewDeliveryTelemetryRepository(db),
	}
}
