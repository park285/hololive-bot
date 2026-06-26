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

package membernews

import (
	"context"
	"log/slog"

	"github.com/kapu/hololive-api/internal/planes/llm/internal/service/membernews/internal/model"
	"github.com/kapu/hololive-api/internal/planes/llm/internal/service/subscription"

	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/database"
)

var _ subscription.SubscriptionRepository[model.SubscribedRoom] = (*Repository)(nil)

const (
	memberNewsRoomsKey     = "membernews:rooms"
	memberNewsRoomNamesKey = "membernews:room_names"
)

type rowScanner interface {
	Scan(dest ...any) error
}

type rowsScanner interface {
	Close()
	Err() error
	Next() bool
	Scan(dest ...any) error
}

type memberNewsQuerier interface {
	Exec(ctx context.Context, sql string, arguments ...any) error
	Query(ctx context.Context, sql string, args ...any) (rowsScanner, error)
	QueryRow(ctx context.Context, sql string, args ...any) rowScanner
}

type Repository struct {
	pool  memberNewsQuerier
	cache cache.Client
	log   *slog.Logger
}

func NewRepository(postgres database.Client, cacheClient cache.Client, logger *slog.Logger) *Repository {
	if logger == nil {
		logger = slog.Default()
	}
	var pool memberNewsQuerier
	if postgres != nil {
		pool = newPGXMemberNewsQuerier(postgres.GetPool())
	}
	return &Repository{
		pool:  pool,
		cache: cacheClient,
		log:   logger,
	}
}
