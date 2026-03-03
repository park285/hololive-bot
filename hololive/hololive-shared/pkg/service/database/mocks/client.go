package mocks

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"gorm.io/gorm"

	"github.com/kapu/hololive-shared/pkg/service/database"
)

// Client is a manual mock for database.Client.
//
// Rationale: keep test dependencies minimal (no mockgen) while allowing interface-based injection.
type Client struct {
	GetPoolFunc   func() *pgxpool.Pool
	GetGormDBFunc func() *gorm.DB
	PingFunc      func(ctx context.Context) error
	CloseFunc     func() error
}

var _ database.Client = (*Client)(nil)

func (c *Client) GetPool() *pgxpool.Pool {
	if c.GetPoolFunc != nil {
		return c.GetPoolFunc()
	}
	return nil
}

func (c *Client) GetGormDB() *gorm.DB {
	if c.GetGormDBFunc != nil {
		return c.GetGormDBFunc()
	}
	return nil
}

func (c *Client) Ping(ctx context.Context) error {
	if c.PingFunc != nil {
		return c.PingFunc(ctx)
	}
	return nil
}

func (c *Client) Close() error {
	if c.CloseFunc != nil {
		return c.CloseFunc()
	}
	return fmt.Errorf("database mock: CloseFunc not set")
}
