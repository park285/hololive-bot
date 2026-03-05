package app

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/kapu/hololive-shared/pkg/config"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
	"github.com/kapu/hololive-shared/pkg/service/database"
)

type nilGormPostgres struct{}

func (p *nilGormPostgres) GetPool() *pgxpool.Pool { return nil }
func (p *nilGormPostgres) GetGormDB() *gorm.DB    { return nil }
func (p *nilGormPostgres) Ping(context.Context) error {
	return nil
}
func (p *nilGormPostgres) Close() error { return nil }

func TestBuildBotAdminServerDependencies_FailFastBranches(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	t.Run("nil config", func(t *testing.T) {
		deps := botAdminRuntimeDependencies{}
		got, err := buildBotAdminServerDependencies(context.Background(), nil, deps, nil, logger)
		require.Error(t, err)
		assert.Nil(t, got)
		assert.Contains(t, err.Error(), "config is nil")
	})

	t.Run("incomplete admin dependency view", func(t *testing.T) {
		cfg := &config.Config{}
		deps := botAdminRuntimeDependencies{
			cache: nil,
		}
		got, err := buildBotAdminServerDependencies(context.Background(), cfg, deps, nil, logger)
		require.Error(t, err)
		assert.Nil(t, got)
		assert.Contains(t, err.Error(), "admin dependency view is incomplete")
	})

	t.Run("auth service creation error wraps", func(t *testing.T) {
		cfg := &config.Config{
			Postgres: config.PostgresConfig{
				AutoPrepareSchema: false,
			},
		}
		deps := botAdminRuntimeDependencies{
			cache:    &cachemocks.Client{},
			postgres: &nilGormPostgres{},
		}

		got, err := buildBotAdminServerDependencies(context.Background(), cfg, deps, nil, logger)
		require.Error(t, err)
		assert.Nil(t, got)
		assert.Contains(t, err.Error(), "create auth service")
		assert.Contains(t, err.Error(), "db must not be nil")
	})
}

var _ database.Client = (*nilGormPostgres)(nil)

