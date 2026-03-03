package database

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
	"gorm.io/gorm"
)

// Client defines the behavior that *PostgresService provides.
//
// Goal: allow service consumers to depend on interfaces rather than concrete implementations.
type Client interface {
	GetPool() *pgxpool.Pool
	GetGormDB() *gorm.DB
	Ping(ctx context.Context) error
	Close() error
}

var _ Client = (*PostgresService)(nil)
