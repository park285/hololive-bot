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
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/park285/shared-go/v2/pkg/db/pgxdb"

	"github.com/kapu/hololive-shared/pkg/constants"
)

type PostgresService struct {
	pool   *pgxpool.Pool
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

func NewPostgresService(ctx context.Context, config *PostgresConfig, logger *slog.Logger) (*PostgresService, error) {
	if config == nil {
		return nil, errors.New("postgres config is nil")
	}

	// pgxdb는 빈 sslmode를 거부(fail-closed)하므로, 예전 dbx가 DSN 생성 시 적용하던
	// verify-full 기본값을 호출측인 여기서 명시적으로 해소한다. TLS posture는 절대 낮추지 않는다.
	sslMode := strings.TrimSpace(config.SSLMode)
	if sslMode == "" {
		sslMode = "verify-full"
	}

	minConns, maxConns, err := resolvePoolConns(config)
	if err != nil {
		return nil, fmt.Errorf("resolve pool conns: %w", err)
	}

	pool, err := pgxdb.OpenPool(ctx, pgxdb.Config{
		Host:          config.Host,
		Port:          config.Port,
		SocketPath:    config.SocketPath,
		User:          config.User,
		Password:      config.Password,
		Name:          config.Database,
		SSLMode:       sslMode,
		QueryExecMode: config.QueryExecMode,
	}, pgxdb.Options{
		Logger: logger,
		Pool: pgxdb.PoolConfig{
			MinConns:        minConns,
			MaxConns:        maxConns,
			ConnMaxLifetime: constants.DatabaseConfig.ConnMaxLifetime,
		},
		Ping: pgxdb.PingConfig{
			PingTimeout: constants.RequestTimeout.DatabasePing,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	return &PostgresService{
		pool:   pool,
		logger: logger,
	}, nil
}

// resolvePoolConns는 pgxdb에 넘길 풀 크기를 정한다.
// MinConns=0은 plane 검증(validatePlanePool·validatePool)이 허용하는 "idle 연결 없음" 선택이고
// pgxdb도 0을 그대로 pgx에 넘기는 계약이므로 기본값으로 바꾸지 않는다. 미설정 기본값은
// env loader(LoadPostgresConfig)가 이미 채운다. 음수는 pgxdb가 0으로 조용히 흡수하므로
// 여기서 거부해 설정 오류를 기동 시점에 드러낸다.
func resolvePoolConns(config *PostgresConfig) (minConns, maxConns int, err error) {
	if config.PoolMinConns < 0 {
		return 0, 0, fmt.Errorf("postgres pool min conns must not be negative: %d", config.PoolMinConns)
	}

	maxConns = config.PoolMaxConns
	if maxConns <= 0 {
		maxConns = constants.DatabaseConfig.MaxOpenConns
	}

	return config.PoolMinConns, maxConns, nil
}

func (ps *PostgresService) GetPool() *pgxpool.Pool {
	return ps.pool
}

func (ps *PostgresService) Close() error {
	if ps.pool != nil {
		ps.pool.Close()

		ps.pool = nil
	}

	return nil
}

func (ps *PostgresService) Ping(ctx context.Context) error {
	if ps.pool == nil {
		return errors.New("ping database: pool is nil")
	}

	if err := ps.pool.Ping(ctx); err != nil {
		return fmt.Errorf("ping database: %w", err)
	}

	return nil
}
