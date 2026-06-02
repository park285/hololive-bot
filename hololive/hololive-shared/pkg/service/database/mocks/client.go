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
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kapu/hololive-shared/pkg/service/database"
)

// Client is a manual mock for database.Client.
//
// Rationale: keep test dependencies minimal (no mockgen) while allowing interface-based injection.
type Client struct {
	GetPoolFunc func() *pgxpool.Pool
	PingFunc    func(ctx context.Context) error
	CloseFunc   func() error
}

var _ database.Client = (*Client)(nil)

func (c *Client) GetPool() *pgxpool.Pool {
	if c.GetPoolFunc != nil {
		return c.GetPoolFunc()
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
