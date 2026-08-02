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

package polling

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-shared/pkg/domain"
)

const (
	viewerSampleCleanupLockKey int64 = 841977301
	viewerSampleCleanupYield         = 10 * time.Millisecond
)

type ViewerSampleCleanerConfig struct {
	RetentionDays int
	BatchSize     int
}

func DefaultViewerSampleCleanerConfig() ViewerSampleCleanerConfig {
	return ViewerSampleCleanerConfig{
		RetentionDays: 7,
		BatchSize:     1000,
	}
}

type ViewerSampleCleaner struct {
	db       dbx.Querier
	acquirer viewerSampleConnAcquirer
	config   ViewerSampleCleanerConfig
}

type viewerSampleConnAcquirer interface {
	Acquire(ctx context.Context) (*pgxpool.Conn, error)
}

func NewViewerSampleCleaner(db any, config ViewerSampleCleanerConfig) *ViewerSampleCleaner {
	return &ViewerSampleCleaner{
		db:       asViewerSampleQuerier(db),
		acquirer: asViewerSampleConnAcquirer(db),
		config:   config,
	}
}

func asViewerSampleConnAcquirer(db any) viewerSampleConnAcquirer {
	acquirer, ok := db.(viewerSampleConnAcquirer)
	if !ok {
		return nil
	}
	return acquirer
}

func asViewerSampleQuerier(db any) dbx.Querier {
	querier, ok := db.(dbx.Querier)
	if !ok {
		return nil
	}
	return querier
}

func (c *ViewerSampleCleaner) Cleanup(ctx context.Context) (int64, error) {
	if c.db == nil {
		return 0, fmt.Errorf("viewer sample cleaner db is nil")
	}
	if c.acquirer != nil {
		return c.cleanupWithDedicatedConn(ctx)
	}
	return c.cleanupLocked(ctx, c.db)
}

func (c *ViewerSampleCleaner) cleanupWithDedicatedConn(ctx context.Context) (int64, error) {
	conn, err := c.acquirer.Acquire(ctx)
	if err != nil {
		return 0, fmt.Errorf("acquire viewer sample cleanup connection: %w", err)
	}
	defer conn.Release()
	return c.cleanupLocked(ctx, conn)
}

func (c *ViewerSampleCleaner) cleanupLocked(ctx context.Context, db dbx.Querier) (int64, error) {
	var deleted int64
	_, err := dbx.WithSessionAdvisoryLock(ctx, db, viewerSampleCleanupLockKey, func(lockedCtx context.Context) error {
		var batchErr error
		deleted, batchErr = c.cleanupBatches(lockedCtx, db)
		return batchErr
	})
	return deleted, err
}

func (c *ViewerSampleCleaner) cleanupBatches(ctx context.Context, db dbx.Querier) (int64, error) {
	spec := c.batchDeleteSpec(time.Now().AddDate(0, 0, -c.config.RetentionDays))

	totalRowsAffected, err := dbx.DeleteInBatches(ctx, db, spec)
	if err != nil {
		return totalRowsAffected, fmt.Errorf("delete viewer sample batch: %w", err)
	}

	if totalRowsAffected > 0 {
		slog.Info("Cleaned up old viewer samples",
			"deleted", totalRowsAffected,
			"retention_days", c.config.RetentionDays,
			"batch_size", spec.BatchSize)
	}

	return totalRowsAffected, nil
}

func (c *ViewerSampleCleaner) batchDeleteSpec(cutoff time.Time) dbx.BatchDeleteSpec {
	return dbx.BatchDeleteSpec{
		Query:     mustSQL("cleaner_0176_03.sql"),
		Args:      []any{domain.LiveStatusEnded, cutoff},
		BatchSize: c.effectiveBatchSize(),
		Yield:     viewerSampleCleanupYield,
	}
}

func (c *ViewerSampleCleaner) effectiveBatchSize() int {
	if c.config.BatchSize > 0 {
		return c.config.BatchSize
	}
	return DefaultViewerSampleCleanerConfig().BatchSize
}
