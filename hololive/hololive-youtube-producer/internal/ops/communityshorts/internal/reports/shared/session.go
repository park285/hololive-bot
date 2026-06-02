package shared

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kapu/hololive-shared/pkg/config"
	sharedproviders "github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/service/database"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox"
	trackingrepo "github.com/kapu/hololive-shared/pkg/service/youtube/tracking"
)

type OpsSession struct {
	Postgres            database.Client
	TrackingRepository  *trackingrepo.PgxRepository
	TelemetryRepository *outbox.DeliveryTelemetryRepository
}

func OpenOpsSession(
	ctx context.Context,
	appConfig *config.Config,
	logger *slog.Logger,
) (*OpsSession, func(), error) {
	if appConfig == nil {
		return nil, nil, fmt.Errorf("config is nil")
	}

	databaseResources, cleanupDB, err := sharedproviders.ProvideDatabaseResources(ctx, appConfig.Postgres, logger)
	if err != nil {
		return nil, nil, fmt.Errorf("provide database resources: %w", err)
	}

	session := NewOpsSession(databaseResources.Service.GetPool())
	session.Postgres = databaseResources.Service
	return session, cleanupDB, nil
}

func NewOpsSession(pool *pgxpool.Pool) *OpsSession {
	return &OpsSession{
		Postgres:            nil,
		TrackingRepository:  trackingrepo.NewRepository(pool),
		TelemetryRepository: outbox.NewDeliveryTelemetryRepository(pool),
	}
}
