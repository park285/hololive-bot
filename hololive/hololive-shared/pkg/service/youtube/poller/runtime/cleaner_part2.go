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
	"errors"
	"fmt"
	"log/slog"
	"time"
)

func viewerSampleCleanupCallError(parentCtx, runCtx context.Context, err error) error {
	if parentCtx.Err() == nil && errors.Is(runCtx.Err(), context.DeadlineExceeded) {
		return fmt.Errorf("viewer sample cleanup time budget exceeded: %w", errors.Join(context.DeadlineExceeded, err))
	}

	return err
}

func yieldViewerSampleCleanup(ctx context.Context) error {
	timer := time.NewTimer(viewerSampleCleanupYield)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("yield viewer sample cleanup: %w", err)
		}

		return nil
	case <-timer.C:
		return nil
	}
}

func (c *ViewerSampleCleaner) logCleanup(deleted int64, batches int, budgetExhausted bool, duration time.Duration) {
	if deleted == 0 && !budgetExhausted {
		return
	}

	slog.Info("Viewer sample cleanup completed",
		"deleted", deleted,
		"retention_days", c.config.RetentionDays,
		"batch_size", c.effectiveBatchSize(),
		"candidate_page_size", viewerSampleCleanupSessionPageSize,
		"batches", batches,
		"max_batches", c.effectiveMaxBatches(),
		"max_duration", c.effectiveMaxDuration(),
		"statement_timeout", viewerSampleCleanupStatementTimeout,
		"budget_exhausted", budgetExhausted,
		"duration", duration)
}

func (c *ViewerSampleCleaner) effectiveBatchSize() int {
	if c.config.BatchSize <= 0 {
		return DefaultViewerSampleCleanerConfig().BatchSize
	}

	return min(c.config.BatchSize, viewerSampleCleanupMaxBatchSize)
}

func (c *ViewerSampleCleaner) effectiveMaxBatches() int {
	if c.maxBatches <= 0 {
		return viewerSampleCleanupMaxBatches
	}

	return min(c.maxBatches, viewerSampleCleanupMaxBatches)
}

func (c *ViewerSampleCleaner) effectiveMaxDuration() time.Duration {
	if c.maxDuration <= 0 {
		return viewerSampleCleanupMaxDuration
	}

	return min(c.maxDuration, viewerSampleCleanupMaxDuration)
}
