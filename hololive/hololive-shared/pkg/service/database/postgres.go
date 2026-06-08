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

	"github.com/kapu/hololive-shared/internal/dbx"
	"github.com/kapu/hololive-shared/pkg/constants"
)

// 내부적으로 hololive-shared/internal/dbx.Client를 사용한다.
type PostgresService struct {
	client *dbx.Client
	logger *slog.Logger
}

type PostgresConfig struct {
	Host          string
	Port          int
	SocketPath    string // UDS 경로 (비어있으면 TCP 사용)
	User          string
	Password      string
	Database      string
	SSLMode       string
	QueryExecMode string
	PoolMinConns  int
	PoolMaxConns  int
}

// hololive-shared/internal/dbx.Client를 사용하여 pgxpool 기반 PostgreSQL 연결을 제공한다.
func NewPostgresService(ctx context.Context, config PostgresConfig, logger *slog.Logger) (*PostgresService, error) {
	dbxConfig := dbx.Config{
		Host:          config.Host,
		Port:          config.Port,
		SocketPath:    config.SocketPath,
		User:          config.User,
		Password:      config.Password,
		Name:          config.Database,
		SSLMode:       config.SSLMode,
		QueryExecMode: config.QueryExecMode,
	}
	minConns := config.PoolMinConns
	if minConns <= 0 {
		minConns = constants.DatabaseConfig.MaxIdleConns
	}
	maxConns := config.PoolMaxConns
	if maxConns <= 0 {
		maxConns = constants.DatabaseConfig.MaxOpenConns
	}
	poolConfig := dbx.PoolConfig{
		MaxConns:        maxConns,
		MinConns:        minConns,
		ConnMaxLifetime: constants.DatabaseConfig.ConnMaxLifetime,
	}

	retryConfig := dbx.RetryConfig{
		PingTimeout: constants.RequestTimeout.DatabasePing,
	}

	client, err := dbx.Open(ctx, dbxConfig, dbx.OpenOptions{
		Logger: logger,
		Pool:   poolConfig,
		Retry:  retryConfig,
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
