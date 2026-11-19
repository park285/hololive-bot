package database

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Client interface {
	GetPool() *pgxpool.Pool
	Ping(ctx context.Context) error
	Close() error
}

var _ Client = (*PostgresService)(nil)
