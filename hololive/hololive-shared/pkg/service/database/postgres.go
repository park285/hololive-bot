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

package database

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"gorm.io/gorm"

	"github.com/kapu/hololive-shared/internal/dbx"
	"github.com/kapu/hololive-shared/pkg/constants"
)

// 내부적으로 hololive-shared/internal/dbx.Client를 사용한다.
type PostgresService struct {
	client *dbx.Client
	logger *slog.Logger
}

type PostgresConfig struct {
	Host             string
	Port             int
	SocketPath       string // UDS 경로 (비어있으면 TCP 사용)
	User             string
	Password         string
	Database         string
	SSLMode          string
	QueryExecMode    string
	PoolMinConns     int
	PoolMaxConns     int
	PoolMaxIdleConns int
}

// hololive-shared/internal/dbx.Client를 사용하여 pgxpool + GORM 듀얼 구조를 제공한다.
func NewPostgresService(ctx context.Context, cfg PostgresConfig, logger *slog.Logger) (*PostgresService, error) {
	dbxCfg := dbx.Config{
		Host:          cfg.Host,
		Port:          cfg.Port,
		SocketPath:    cfg.SocketPath,
		User:          cfg.User,
		Password:      cfg.Password,
		Name:          cfg.Database,
		SSLMode:       cfg.SSLMode,
		QueryExecMode: cfg.QueryExecMode,
	}
	minConns := cfg.PoolMinConns
	if minConns <= 0 {
		minConns = constants.DatabaseConfig.MaxIdleConns
	}
	maxConns := cfg.PoolMaxConns
	if maxConns <= 0 {
		maxConns = constants.DatabaseConfig.MaxOpenConns
	}
	maxIdleConns := cfg.PoolMaxIdleConns
	if maxIdleConns <= 0 {
		maxIdleConns = constants.DatabaseConfig.MaxIdleConns
	}
	poolCfg := dbx.PoolConfig{
		MaxConns:        maxConns,
		MaxIdleConns:    maxIdleConns,
		MinConns:        minConns,
		ConnMaxLifetime: constants.DatabaseConfig.ConnMaxLifetime,
	}

	retryCfg := dbx.RetryConfig{
		PingTimeout: constants.RequestTimeout.DatabasePing,
	}

	client, err := dbx.Open(ctx, dbxCfg, dbx.OpenOptions{
		Logger: logger,
		Pool:   poolCfg,
		Retry:  retryCfg,
	})
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	return &PostgresService{
		client: client,
		logger: logger,
	}, nil
}

func (ps *PostgresService) GetPool() *pgxpool.Pool {
	return ps.client.Pool()
}

func (ps *PostgresService) GetGormDB() *gorm.DB {
	return ps.client.Gorm()
}

func (ps *PostgresService) Close() error {
	if ps.client != nil {
		if err := ps.client.Close(); err != nil {
			return fmt.Errorf("close database: %w", err)
		}
	}
	return nil
}

func (ps *PostgresService) Ping(ctx context.Context) error {
	if err := ps.client.Ping(ctx); err != nil {
		return fmt.Errorf("ping database: %w", err)
	}
	return nil
}
