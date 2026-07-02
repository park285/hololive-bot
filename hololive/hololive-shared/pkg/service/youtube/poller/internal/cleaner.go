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

	"github.com/kapu/hololive-shared/internal/dbx"
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
	acquirer, _ := db.(viewerSampleConnAcquirer)
	return &ViewerSampleCleaner{
		db:       asViewerSampleQuerier(db),
		acquirer: acquirer,
		config:   config,
	}
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
	locked, err := acquireViewerSampleCleanupLock(ctx, db)
	if err != nil {
		return 0, err
	}
	if !locked {
		return 0, nil
	}
	defer releaseViewerSampleCleanupLock(db)
	return c.cleanupBatches(ctx, db)
}

func acquireViewerSampleCleanupLock(ctx context.Context, db dbx.Querier) (bool, error) {
	var locked bool
	if err := db.QueryRow(ctx, "SELECT pg_try_advisory_lock($1)", viewerSampleCleanupLockKey).Scan(&locked); err != nil {
		return false, fmt.Errorf("acquire viewer sample cleanup lock: %w", err)
	}
	return locked, nil
}

func releaseViewerSampleCleanupLock(db dbx.Querier) {
	// ctx가 취소돼도 세션 락은 반드시 해제돼야 한다. 안 하면 conn이 락을 쥔 채 pool로 반환된다.
	if _, err := db.Exec(context.Background(), "SELECT pg_advisory_unlock($1)", viewerSampleCleanupLockKey); err != nil {
		slog.Warn("release viewer sample cleanup lock failed", "error", err)
	}
}

func (c *ViewerSampleCleaner) cleanupBatches(ctx context.Context, db dbx.Querier) (int64, error) {
	cutoff := time.Now().AddDate(0, 0, -c.config.RetentionDays)
	batchSize := c.effectiveBatchSize()

	var totalRowsAffected int64
	for {
		if err := ctx.Err(); err != nil {
			return totalRowsAffected, err
		}
		rowsAffected, err := deleteViewerSampleCleanupBatch(ctx, db, cutoff, batchSize)
		if err != nil {
			return totalRowsAffected, err
		}
		totalRowsAffected += rowsAffected
		if rowsAffected < int64(batchSize) {
			break
		}
		if err := yieldViewerSampleCleanup(ctx); err != nil {
			return totalRowsAffected, err
		}
	}

	if totalRowsAffected > 0 {
		slog.Info("Cleaned up old viewer samples",
			"deleted", totalRowsAffected,
			"retention_days", c.config.RetentionDays,
			"batch_size", batchSize)
	}

	return totalRowsAffected, nil
}

func deleteViewerSampleCleanupBatch(ctx context.Context, db dbx.Querier, cutoff time.Time, batchSize int) (int64, error) {
	tag, err := db.Exec(ctx, `
		WITH picked AS (
			SELECT s.video_id, s.captured_at
			FROM youtube_live_viewer_samples s
			JOIN youtube_live_sessions l ON l.video_id = s.video_id
			WHERE l.status = $1 AND l.ended_at < $2
			ORDER BY s.video_id ASC, s.captured_at ASC
			LIMIT $3
		)
		DELETE FROM youtube_live_viewer_samples
		USING picked
		WHERE youtube_live_viewer_samples.video_id = picked.video_id
			AND youtube_live_viewer_samples.captured_at = picked.captured_at`,
		domain.LiveStatusEnded,
		cutoff,
		batchSize,
	)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func (c *ViewerSampleCleaner) effectiveBatchSize() int {
	if c.config.BatchSize > 0 {
		return c.config.BatchSize
	}
	return DefaultViewerSampleCleanerConfig().BatchSize
}

func yieldViewerSampleCleanup(ctx context.Context) error {
	timer := time.NewTimer(viewerSampleCleanupYield)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
