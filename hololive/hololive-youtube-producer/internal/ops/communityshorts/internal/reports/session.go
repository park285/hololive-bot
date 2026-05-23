package reports

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
	postgres            database.Client
	db                  *gorm.DB
	trackingRepository  *trackingrepo.GormRepository
	telemetryRepository *outbox.DeliveryTelemetryRepository
}

func openCommunityShortsOpsSession(
	ctx context.Context,
	appConfig *config.Config,
	logger *slog.Logger,
) (*communityShortsOpsSession, func(), error) {
	if appConfig == nil {
		return nil, nil, fmt.Errorf("config is nil")
	}

	databaseResources, cleanupDB, err := sharedproviders.ProvideDatabaseResources(ctx, appConfig.Postgres, logger)
	if err != nil {
		return nil, nil, fmt.Errorf("provide database resources: %w", err)
	}

	session := newCommunityShortsOpsSession(databaseResources.Service.GetGormDB())
	session.postgres = databaseResources.Service
	return session, cleanupDB, nil
}

func newCommunityShortsOpsSession(db *gorm.DB) *communityShortsOpsSession {
	return &communityShortsOpsSession{
		postgres:            nil,
		db:                  db,
		trackingRepository:  trackingrepo.NewRepository(db),
		telemetryRepository: outbox.NewDeliveryTelemetryRepository(db),
	}
}
