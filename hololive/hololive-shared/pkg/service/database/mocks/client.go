package mocks

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kapu/hololive-shared/pkg/service/database"
)

type Client struct {
	GetPoolFunc func() *pgxpool.Pool
	PingFunc    func(ctx context.Context) error
	CloseFunc   func() error
}

var _ database.Client = (*Client)(nil)

func NewUnconnectedClient(tb testing.TB) *Client {
	tb.Helper()

	pool, err := pgxpool.New(tb.Context(), "postgres://hololive_test@127.0.0.1:1/hololive_test?sslmode=disable")
	if err != nil {
		tb.Fatalf("create unconnected pool: %v", err)
	}

	tb.Cleanup(pool.Close)

	return &Client{GetPoolFunc: func() *pgxpool.Pool { return pool }}
}

func (c *Client) GetPool() *pgxpool.Pool {
	if c.GetPoolFunc != nil {
		return c.GetPoolFunc()
	}

	return nil
}

func (c *Client) Ping(ctx context.Context) error {
	if c.PingFunc != nil {
		if err := c.PingFunc(ctx); err != nil {
			return fmt.Errorf("ping func: %w", err)
		}

		return nil
	}

	return nil
}

func (c *Client) Close() error {
	if c.CloseFunc != nil {
		if err := c.CloseFunc(); err != nil {
			return fmt.Errorf("close func: %w", err)
		}

		return nil
	}

	return errors.New("database mock: CloseFunc not set")
}
