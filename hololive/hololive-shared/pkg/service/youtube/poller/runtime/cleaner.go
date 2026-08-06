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
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kapu/hololive-shared/pkg/dbx"
)

const (
	viewerSampleCleanupLockKey          int64 = 841977301
	viewerSampleCleanupYield                  = 10 * time.Millisecond
	viewerSampleCleanupSessionPageSize        = 64
	viewerSampleCleanupMaxBatchSize           = 1000
	viewerSampleCleanupMaxBatches             = 64
	viewerSampleCleanupMaxDuration            = 30 * time.Second
	viewerSampleCleanupStatementTimeout       = 2 * time.Second
)

type ViewerSampleCleanerConfig struct {
	RetentionDays int
	BatchSize     int
}

func DefaultViewerSampleCleanerConfig() ViewerSampleCleanerConfig {
	return ViewerSampleCleanerConfig{
		RetentionDays: 7,
		BatchSize:     viewerSampleCleanupMaxBatchSize,
	}
}

type ViewerSampleCleaner struct {
	acquirer viewerSampleConnAcquirer
	session  *pgxpool.Conn
	config   ViewerSampleCleanerConfig

	stateMu     sync.Mutex
	state       viewerSampleCleanupState
	maxBatches  int
	maxDuration time.Duration
}

type viewerSampleConnAcquirer interface {
	Acquire(ctx context.Context) (*pgxpool.Conn, error)
}

type viewerSampleCleanupCursor struct {
	endedAt time.Time
	videoID string
}

type viewerSampleCleanupState struct {
	cursor      viewerSampleCleanupCursor
	passDeleted int64
}

type viewerSampleCleanupStep struct {
	deleted        int64
	target         *viewerSampleCleanupCursor
	candidateCount int64
	pageEnd        *viewerSampleCleanupCursor
}

func NewViewerSampleCleaner(db any, config ViewerSampleCleanerConfig) *ViewerSampleCleaner {
	return &ViewerSampleCleaner{
		acquirer:    asViewerSampleConnAcquirer(db),
		session:     asViewerSampleSession(db),
		config:      config,
		state:       viewerSampleCleanupState{cursor: initialViewerSampleCleanupCursor()},
		maxBatches:  viewerSampleCleanupMaxBatches,
		maxDuration: viewerSampleCleanupMaxDuration,
	}
}

func asViewerSampleConnAcquirer(db any) viewerSampleConnAcquirer {
	acquirer, ok := db.(viewerSampleConnAcquirer)
	if !ok {
		return nil
	}
	return acquirer
}

func asViewerSampleSession(db any) *pgxpool.Conn {
	session, _ := db.(*pgxpool.Conn)
	return session
}

func (c *ViewerSampleCleaner) Cleanup(ctx context.Context) (int64, error) {
	runCtx, cancel := context.WithTimeout(ctx, c.effectiveMaxDuration())
	defer cancel()

	deleted, err := c.cleanup(runCtx)
	if err == nil {
		return deleted, nil
	}
	return deleted, viewerSampleCleanupCallError(ctx, runCtx, err)
}

func (c *ViewerSampleCleaner) cleanup(ctx context.Context) (int64, error) {
	if c.acquirer != nil {
		return c.cleanupWithDedicatedConn(ctx)
	}
	if c.session != nil {
		return c.cleanupLocked(ctx, c.session)
	}
	return 0, fmt.Errorf("viewer sample cleaner requires a session-affine pgxpool connection")
}

func (c *ViewerSampleCleaner) cleanupWithDedicatedConn(ctx context.Context) (int64, error) {
	conn, err := c.acquirer.Acquire(ctx)
	if err != nil {
		return 0, fmt.Errorf("acquire viewer sample cleanup connection: %w", err)
	}
	defer conn.Release()
	return c.cleanupLocked(ctx, conn)
}

func (c *ViewerSampleCleaner) cleanupLocked(ctx context.Context, conn *pgxpool.Conn) (int64, error) {
	var deleted int64
	acquired, err := dbx.WithSessionAdvisoryLock(ctx, conn, viewerSampleCleanupLockKey, func(lockedCtx context.Context) error {
		c.stateMu.Lock()
		defer c.stateMu.Unlock()

		var batchErr error
		deleted, batchErr = c.cleanupBatches(lockedCtx, conn)
		return batchErr
	})
	if err != nil {
		return deleted, err
	}
	if !acquired {
		slog.Debug("Skipped viewer sample cleanup because advisory lock is held")
	}
	return deleted, nil
}

func (c *ViewerSampleCleaner) cleanupBatches(ctx context.Context, db dbx.Querier) (int64, error) {
	startedAt := time.Now()
	cutoff := time.Now().AddDate(0, 0, -c.config.RetentionDays)
	c.ensureCleanupState()
	maxBatches := c.effectiveMaxBatches()
	var total int64

	for batch := 1; batch <= maxBatches; batch++ {
		step, err := c.deleteNextBatch(ctx, db, cutoff, c.state.cursor)
		if err != nil {
			return total, fmt.Errorf("delete viewer sample batch: %w", err)
		}
		total += step.deleted
		c.state.passDeleted += step.deleted

		nextCursor, passDone, err := step.nextCursor()
		if err != nil {
			return total, fmt.Errorf("advance viewer sample cleanup cursor: %w", err)
		}
		if passDone {
			if c.state.passDeleted == 0 {
				c.resetCleanupState()
				c.logCleanup(total, batch, false, time.Since(startedAt))
				return total, nil
			}
			c.resetCleanupState()
		} else {
			c.state.cursor = nextCursor
		}

		if batch == maxBatches {
			c.logCleanup(total, batch, true, time.Since(startedAt))
			return total, nil
		}
		if err := yieldViewerSampleCleanup(ctx); err != nil {
			return total, fmt.Errorf("yield viewer sample cleanup: %w", err)
		}
	}

	return total, nil
}

func (c *ViewerSampleCleaner) deleteNextBatch(
	ctx context.Context,
	db dbx.Querier,
	cutoff time.Time,
	cursor viewerSampleCleanupCursor,
) (viewerSampleCleanupStep, error) {
	statementCtx, cancel := context.WithTimeout(ctx, viewerSampleCleanupStatementTimeout)
	defer cancel()

	var step viewerSampleCleanupStep
	var targetEndedAt, pageEndEndedAt pgtype.Timestamptz
	var targetVideoID, pageEndVideoID pgtype.Text

	err := db.QueryRow(
		statementCtx,
		mustSQL("cleaner_0176_03.sql"),
		cutoff,
		cursor.endedAt,
		cursor.videoID,
		viewerSampleCleanupSessionPageSize,
		c.effectiveBatchSize(),
	).Scan(
		&step.deleted,
		&targetEndedAt,
		&targetVideoID,
		&step.candidateCount,
		&pageEndEndedAt,
		&pageEndVideoID,
	)
	if err != nil {
		if ctx.Err() == nil && errors.Is(statementCtx.Err(), context.DeadlineExceeded) {
			return viewerSampleCleanupStep{}, fmt.Errorf(
				"viewer sample cleanup statement time budget exceeded: %w",
				errors.Join(context.DeadlineExceeded, err),
			)
		}
		return viewerSampleCleanupStep{}, fmt.Errorf("delete viewer sample cleanup step: %w", err)
	}

	step.target, err = viewerSampleCleanupCursorFromPG(targetEndedAt, targetVideoID)
	if err != nil {
		return viewerSampleCleanupStep{}, fmt.Errorf("decode viewer sample cleanup target: %w", err)
	}
	step.pageEnd, err = viewerSampleCleanupCursorFromPG(pageEndEndedAt, pageEndVideoID)
	if err != nil {
		return viewerSampleCleanupStep{}, fmt.Errorf("decode viewer sample cleanup page end: %w", err)
	}
	if err := step.validate(); err != nil {
		return viewerSampleCleanupStep{}, err
	}
	return step, nil
}

func (s viewerSampleCleanupStep) validate() error {
	if s.candidateCount < 0 || s.candidateCount > viewerSampleCleanupSessionPageSize {
		return fmt.Errorf("viewer sample candidate count is out of range: %d", s.candidateCount)
	}
	if (s.candidateCount == 0) != (s.pageEnd == nil) {
		return fmt.Errorf("viewer sample page end does not match candidate count: %d", s.candidateCount)
	}
	if s.target == nil && s.deleted != 0 {
		return fmt.Errorf("viewer sample cleanup deleted %d rows without a target session", s.deleted)
	}
	if s.target != nil && s.deleted <= 0 {
		return fmt.Errorf("viewer sample cleanup target %q produced no deleted rows", s.target.videoID)
	}
	return nil
}

func (s viewerSampleCleanupStep) nextCursor() (viewerSampleCleanupCursor, bool, error) {
	if s.target != nil {
		next := *s.target
		return next, false, nil
	}
	if s.candidateCount < viewerSampleCleanupSessionPageSize {
		return viewerSampleCleanupCursor{}, true, nil
	}
	if s.pageEnd == nil {
		return viewerSampleCleanupCursor{}, false, fmt.Errorf("full candidate page has no page-end cursor")
	}

	return *s.pageEnd, false, nil
}

func viewerSampleCleanupCursorFromPG(
	endedAt pgtype.Timestamptz,
	videoID pgtype.Text,
) (*viewerSampleCleanupCursor, error) {
	if endedAt.Valid != videoID.Valid {
		return nil, fmt.Errorf("cursor fields have mismatched nullability")
	}
	if !endedAt.Valid {
		return nil, nil
	}
	return &viewerSampleCleanupCursor{endedAt: endedAt.Time.UTC(), videoID: videoID.String}, nil
}

func initialViewerSampleCleanupCursor() viewerSampleCleanupCursor {
	return viewerSampleCleanupCursor{
		endedAt: time.Date(1, time.January, 1, 0, 0, 0, 0, time.UTC),
		videoID: "",
	}
}

func (c *ViewerSampleCleaner) ensureCleanupState() {
	if c.state.cursor.endedAt.IsZero() {
		c.resetCleanupState()
	}
}

func (c *ViewerSampleCleaner) resetCleanupState() {
	c.state = viewerSampleCleanupState{cursor: initialViewerSampleCleanupCursor()}
}

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
		return ctx.Err()
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
