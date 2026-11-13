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

package mocks

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kapu/hololive-shared/pkg/service/database"
)

// mockgen 없이 test 의존성을 최소화하면서 interface 기반 injection을 허용하려고
// database.Client를 손으로 구현한 mock이다.
type Client struct {
	GetPoolFunc func() *pgxpool.Pool
	PingFunc    func(ctx context.Context) error
	CloseFunc   func() error
}

var _ database.Client = (*Client)(nil)

// NewUnconnectedClient의 pool은 MinConns가 0이라 첫 Acquire 전까지 접속하지 않으며, 접속하면 실패한다.
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
