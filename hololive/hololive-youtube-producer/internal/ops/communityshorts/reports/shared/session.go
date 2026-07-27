package shared

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/config/settings"

	"github.com/jackc/pgx/v5/pgxpool"

	sharedproviders "github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/service/database"

	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/telemetry"
	"github.com/kapu/hololive-shared/pkg/service/youtube/tracking/observation"
)

type OpsSession struct {
	Postgres            database.Client
	TrackingRepository  *observation.PgxRepository
	TelemetryRepository *telemetry.Repository
}

func OpenOpsSession(
	ctx context.Context,
	appConfig *settings.Config,
	logger *slog.Logger,
) (*OpsSession, func(), error) {
	if appConfig == nil {
		return nil, nil, fmt.Errorf("config is nil")
	}

	databaseResources, cleanupDB, err := sharedproviders.ProvideDatabaseResources(ctx, &appConfig.Postgres, logger)
	if err != nil {
		return nil, nil, fmt.Errorf("provide database resources: %w", err)
	}

	session := NewOpsSession(ctx, databaseResources.Service.GetPool())
	session.Postgres = databaseResources.Service
	return session, cleanupDB, nil
}

func NewOpsSession(ctx context.Context, pool *pgxpool.Pool) *OpsSession {
	return &OpsSession{
		Postgres:            nil,
		TrackingRepository:  observation.NewRepositoryContext(ctx, pool),
		TelemetryRepository: telemetry.NewRepository(pool),
	}
}
