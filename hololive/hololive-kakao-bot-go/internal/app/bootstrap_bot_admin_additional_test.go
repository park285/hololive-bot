// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package app

import (
	"context"
	"log/slog"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kapu/hololive-shared/pkg/config"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
	"github.com/kapu/hololive-shared/pkg/service/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type nilGormPostgres struct{}

type gormOnlyPostgres struct {
	db *gorm.DB
}

func (p *nilGormPostgres) GetPool() *pgxpool.Pool { return nil }
func (p *nilGormPostgres) GetGormDB() *gorm.DB    { return nil }
func (p *nilGormPostgres) Ping(context.Context) error {
	return nil
}
func (p *nilGormPostgres) Close() error { return nil }

func (p *gormOnlyPostgres) GetPool() *pgxpool.Pool { return nil }
func (p *gormOnlyPostgres) GetGormDB() *gorm.DB    { return p.db }
func (p *gormOnlyPostgres) Ping(context.Context) error {
	return nil
}
func (p *gormOnlyPostgres) Close() error { return nil }

func TestBuildBotAdminServerDependencies_FailFastBranches(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.DiscardHandler)

	t.Run("nil config", func(t *testing.T) {
		deps := botAdminRuntimeDependencies{}
		got, err := buildBotAdminServerDependencies(t.Context(), nil, deps, nil, logger)
		require.Error(t, err)
		assert.Nil(t, got)
		assert.Contains(t, err.Error(), "config is nil")
	})

	t.Run("incomplete admin dependency view", func(t *testing.T) {
		cfg := &config.Config{}
		deps := botAdminRuntimeDependencies{
			cache: nil,
		}
		got, err := buildBotAdminServerDependencies(t.Context(), cfg, deps, nil, logger)
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

		got, err := buildBotAdminServerDependencies(t.Context(), cfg, deps, nil, logger)
		require.Error(t, err)
		assert.Nil(t, got)
		assert.Contains(t, err.Error(), "create auth service")
		assert.Contains(t, err.Error(), "db must not be nil")
	})
}

func TestBuildBotAdminAPIHandlers_WiresCommunityShortsOpsRepository(t *testing.T) {
	t.Parallel()

	handlers := buildBotAdminAPIHandlers(
		botAdminRuntimeDependencies{postgres: &gormOnlyPostgres{db: &gorm.DB{}}},
		nil,
		nil,
		nil,
		nil,
		slog.New(slog.DiscardHandler),
	)
	require.NotNil(t, handlers)
	require.NotNil(t, handlers.Stats)
	assert.True(t, handlers.Stats.HasCommunityShortsOpsRepository())
}

func TestBuildBotAdminAPIHandlers_LeavesCommunityShortsOpsRepositoryNilWithoutGorm(t *testing.T) {
	t.Parallel()

	handlers := buildBotAdminAPIHandlers(
		botAdminRuntimeDependencies{postgres: &nilGormPostgres{}},
		nil,
		nil,
		nil,
		nil,
		slog.New(slog.DiscardHandler),
	)
	require.NotNil(t, handlers)
	require.NotNil(t, handlers.Stats)
	assert.False(t, handlers.Stats.HasCommunityShortsOpsRepository())
}

var _ database.Client = (*nilGormPostgres)(nil)
var _ database.Client = (*gormOnlyPostgres)(nil)
