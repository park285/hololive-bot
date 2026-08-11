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

package providers

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kapu/hololive-shared/pkg/domain"
)

type poollessDatabaseClient struct{}

func (poollessDatabaseClient) GetPool() *pgxpool.Pool     { return nil }
func (poollessDatabaseClient) Ping(context.Context) error { return nil }
func (poollessDatabaseClient) Close() error               { return nil }

func TestProvideAlarmReaderWithholdsWriteMethods(t *testing.T) {
	reader := ProvideAlarmReader(poollessDatabaseClient{}, nil)

	if _, ok := reader.(interface {
		Add(context.Context, *domain.Alarm) error
	}); ok {
		t.Fatal("ProvideAlarmReader must not expose Add to alarm read consumers")
	}
	if _, ok := reader.(interface {
		Remove(context.Context, string, string) error
	}); ok {
		t.Fatal("ProvideAlarmReader must not expose Remove to alarm read consumers")
	}
	if _, ok := reader.(interface {
		ClearByRoom(context.Context, string) (int64, error)
	}); ok {
		t.Fatal("ProvideAlarmReader must not expose ClearByRoom to alarm read consumers")
	}
}
