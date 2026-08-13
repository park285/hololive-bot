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

package retention

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	polling "github.com/kapu/hololive-shared/pkg/service/youtube/poller/runtime"
	sharedenv "github.com/park285/shared-go/pkg/envutil"
	"github.com/park285/shared-go/pkg/retry"
)

const (
	cleanupLockKey          int64 = 841977302
	defaultBatchSize              = 1000
	defaultInterval               = time.Hour
	batchYield                    = 10 * time.Millisecond
	maxCleanupBatches             = 64
	maxCleanupDuration            = 30 * time.Second
	cleanupStatementTimeout       = 2 * time.Second

	channelSnapshotsDaysEnv = "YOUTUBE_PRODUCER_RETENTION_CHANNEL_SNAPSHOTS_DAYS"
	liveSessionsDaysEnv     = "YOUTUBE_PRODUCER_RETENTION_LIVE_SESSIONS_DAYS"
	viewerSamplesDaysEnv    = "YOUTUBE_PRODUCER_RETENTION_VIEWER_SAMPLES_DAYS"
)

// BRIN(time/captured_at)은 정렬을 제공하지 않아 ORDER BY 시 매 배치가 cutoff 미만 잔여 전량을 스캔+정렬한다. cutoff 미만 전량 삭제라 순서 무관 → 생략(live_sessions는 btree idx_yls_ended_cleanup이라 ORDER BY 유지).
var deleteChannelSnapshotsSQL = mustSQL("retention_0046_01.sql")

// youtube_live_viewer_samples cleaner(shared poller/internal/cleaner.go)가 이 테이블을 JOIN
// 게이트로 써서 삭제 대상 샘플을 고른다. 샘플이 남은 세션을 먼저 지우면 그 게이트가 사라져
// 해당 샘플이 영구 고아가 되므로(두 테이블 사이 FK·cascade 없음), 샘플이 모두 지워진 세션만 삭제한다.
var deleteLiveSessionsSQL = mustSQL("retention_0060_02.sql")

type Config struct {
	ChannelSnapshotsDays int
	LiveSessionsDays     int
	ViewerSamplesDays    int
	BatchSize            int
	Interval             time.Duration
	MaxBatches           int
	MaxDuration          time.Duration
	StatementTimeout     time.Duration
}

func LoadConfig() Config {
	return Config{
		ChannelSnapshotsDays: retentionDaysEnv(channelSnapshotsDaysEnv),
		LiveSessionsDays:     retentionDaysEnv(liveSessionsDaysEnv),
		ViewerSamplesDays:    retentionDaysEnv(viewerSamplesDaysEnv),
		BatchSize:            defaultBatchSize,
		Interval:             defaultInterval,
	}
}

// retentionDaysEnv는 음수를 0(비활성)으로 강제한다. 음수 보존일은 cutoff를 미래로 밀어
// 전체 이력을 삭제하므로 반드시 차단해야 한다.
func retentionDaysEnv(key string) int {
	days := sharedenv.Int(key, 0)
	if days < 0 {
		return 0
	}
	return days
}

func (c Config) Enabled() bool {
	return c.ChannelSnapshotsDays > 0 || c.LiveSessionsDays > 0 || c.ViewerSamplesDays > 0
}

func (c Config) effectiveBatchSize() int {
	if c.BatchSize > 0 {
		return min(c.BatchSize, defaultBatchSize)
	}
	return defaultBatchSize
}

func (c Config) effectiveMaxBatches() int {
	if c.MaxBatches > 0 {
		return min(c.MaxBatches, maxCleanupBatches)
	}
	return maxCleanupBatches
}

func (c Config) effectiveMaxDuration() time.Duration {
	if c.MaxDuration > 0 {
		return min(c.MaxDuration, maxCleanupDuration)
	}
	return maxCleanupDuration
}

func (c Config) effectiveStatementTimeout() time.Duration {
	if c.StatementTimeout > 0 {
		return min(c.StatementTimeout, cleanupStatementTimeout)
	}
	return cleanupStatementTimeout
}

func (c Config) effectiveInterval() time.Duration {
	if c.Interval > 0 {
		return c.Interval
	}
	return defaultInterval
}

func cutoffFor(now time.Time, retentionDays int) time.Time {
	return now.AddDate(0, 0, -retentionDays)
}

type viewerSampleCleaner interface {
	Cleanup(ctx context.Context) (int64, error)
}

type Cleaner struct {
	pool          *pgxpool.Pool
	config        Config
	logger        *slog.Logger
	viewerCleaner viewerSampleCleaner

	cursorMu     sync.Mutex
	targetCursor int
}

func NewCleaner(pool *pgxpool.Pool, config Config, logger *slog.Logger) *Cleaner {
	c := &Cleaner{pool: pool, config: config, logger: logger}
	if pool != nil && config.ViewerSamplesDays > 0 {
		c.viewerCleaner = polling.NewViewerSampleCleaner(pool, polling.ViewerSampleCleanerConfig{
			RetentionDays: config.ViewerSamplesDays,
			BatchSize:     config.effectiveBatchSize(),
		})
	}
	return c
}

func (c *Cleaner) Start(ctx context.Context) {
	interval := c.config.effectiveInterval()
	for {
		if _, err := c.Cleanup(ctx); err != nil && ctx.Err() == nil {
			c.logWarn("youtube retention cleanup failed", err)
		}
		if !retry.Sleep(ctx, interval) {
			return
		}
	}
}

func (c *Cleaner) Cleanup(ctx context.Context) (int64, error) {
	runCtx, cancel := context.WithTimeout(ctx, c.config.effectiveMaxDuration())
	defer cancel()

	deleted, err := c.cleanup(runCtx)
	if err == nil {
		return deleted, nil
	}
	if ctx.Err() == nil && errors.Is(runCtx.Err(), context.DeadlineExceeded) {
		return deleted, fmt.Errorf("youtube retention cleanup time budget exceeded: %w", errors.Join(context.DeadlineExceeded, err))
	}
	return deleted, err
}

func (c *Cleaner) cleanup(ctx context.Context) (int64, error) {
	if c.pool == nil {
		return 0, fmt.Errorf("youtube retention cleaner pool is nil")
	}
	conn, err := c.pool.Acquire(ctx)
	if err != nil {
		return 0, fmt.Errorf("acquire youtube retention cleanup connection: %w", err)
	}
	defer conn.Release()

	locked, err := acquireLock(ctx, conn)
	if err != nil {
		return 0, err
	}
	if !locked {
		return 0, nil
	}
	defer releaseLock(ctx, conn, c.logger)

	// viewer_samples를 먼저 정리해야 같은 tick 안에서 live_sessions NOT EXISTS 게이트가 열린다.
	samplesDeleted, err := c.cleanupViewerSamples(ctx)
	if err != nil {
		return samplesDeleted, err
	}
	targetsDeleted, err := c.cleanupTargets(ctx, conn)
	return samplesDeleted + targetsDeleted, err
}

func (c *Cleaner) cleanupViewerSamples(ctx context.Context) (int64, error) {
	if c.viewerCleaner == nil {
		return 0, nil
	}
	deleted, err := c.viewerCleaner.Cleanup(ctx)
	if err != nil {
		return deleted, fmt.Errorf("cleanup youtube_live_viewer_samples: %w", err)
	}
	if deleted > 0 {
		c.logInfo("youtube_live_viewer_samples", deleted, c.config.ViewerSamplesDays, c.config.effectiveBatchSize())
	}
	return deleted, nil
}

type target struct {
	name          string
	retentionDays int
	deleteSQL     string
}

func (c *Cleaner) targets() []target {
	return []target{
		{name: "youtube_channel_stats_snapshots", retentionDays: c.config.ChannelSnapshotsDays, deleteSQL: deleteChannelSnapshotsSQL},
		{name: "youtube_live_sessions", retentionDays: c.config.LiveSessionsDays, deleteSQL: deleteLiveSessionsSQL},
	}
}

func (c *Cleaner) cleanupTargets(ctx context.Context, conn batchExecutor) (int64, error) {
	batchSize := c.config.effectiveBatchSize()
	remainingBatches := c.config.effectiveMaxBatches()
	targets := c.targets()
	start := c.nextTargetStart(len(targets))
	var total int64
	for i := range targets {
		t := targets[(start+i)%len(targets)]
		deleted, exhausted, err := c.cleanupTarget(ctx, conn, t, batchSize, &remainingBatches)
		total += deleted
		if err != nil {
			return total, fmt.Errorf("cleanup %s: %w", t.name, err)
		}
		if exhausted {
			c.logBudgetExhausted(t.name, deleted)
			return total, nil
		}
	}
	return total, nil
}

func (c *Cleaner) nextTargetStart(count int) int {
	if count <= 0 {
		return 0
	}
	c.cursorMu.Lock()
	defer c.cursorMu.Unlock()
	start := c.targetCursor % count
	c.targetCursor = (start + 1) % count
	return start
}

func (c *Cleaner) cleanupTarget(ctx context.Context, conn batchExecutor, t target, batchSize int, remainingBatches *int) (deleted int64, exhausted bool, err error) {
	if t.retentionDays <= 0 {
		return 0, false, nil
	}
	cutoff := cutoffFor(time.Now(), t.retentionDays)
	deleted, exhausted, err = deleteBatches(ctx, conn, t.deleteSQL, cutoff, batchSize, remainingBatches, c.config.effectiveStatementTimeout())
	if err != nil {
		return deleted, false, err
	}
	if deleted > 0 {
		c.logInfo(t.name, deleted, t.retentionDays, batchSize)
	}
	return deleted, exhausted, nil
}

func deleteBatches(ctx context.Context, conn batchExecutor, deleteSQL string, cutoff time.Time, batchSize int, remainingBatches *int, statementTimeout time.Duration) (deleted int64, exhausted bool, err error) {
	var total int64
	for {
		if *remainingBatches <= 0 {
			return total, true, nil
		}
		rows, exhausted, err := deleteBatch(ctx, conn, deleteSQL, cutoff, batchSize, remainingBatches, statementTimeout)
		total += rows
		if err != nil {
			return total, false, err
		}
		if deleteBatchesDone(rows, batchSize, exhausted) {
			return total, exhausted, nil
		}
	}
}

func deleteBatchesDone(rows int64, batchSize int, exhausted bool) bool {
	return exhausted || rows < int64(batchSize)
}

func deleteBatch(ctx context.Context, conn batchExecutor, deleteSQL string, cutoff time.Time, batchSize int, remainingBatches *int, statementTimeout time.Duration) (deleted int64, exhausted bool, err error) {
	rows, err := deleteOneBatch(ctx, conn, deleteSQL, cutoff, batchSize, statementTimeout)
	(*remainingBatches)--
	if err != nil {
		return rows, false, err
	}
	if rows < int64(batchSize) {
		return rows, false, nil
	}
	if *remainingBatches <= 0 {
		return rows, true, nil
	}
	if err := yield(ctx); err != nil {
		return rows, false, err
	}
	return rows, false, nil
}

func deleteOneBatch(ctx context.Context, conn batchExecutor, deleteSQL string, cutoff time.Time, batchSize int, statementTimeout time.Duration) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	statementCtx, cancel := context.WithTimeout(ctx, statementTimeout)
	defer cancel()
	tag, err := conn.Exec(statementCtx, deleteSQL, cutoff, batchSize)
	if err != nil {
		if ctx.Err() == nil && errors.Is(statementCtx.Err(), context.DeadlineExceeded) {
			return 0, fmt.Errorf("youtube retention statement time budget exceeded: %w", errors.Join(context.DeadlineExceeded, err))
		}
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func (c *Cleaner) logBudgetExhausted(table string, deleted int64) {
	if c.logger == nil {
		return
	}
	c.logger.Info("Youtube retention cleanup budget exhausted",
		slog.String("table", table),
		slog.Int64("deleted", deleted),
		slog.Int("max_batches", c.config.effectiveMaxBatches()),
		slog.Duration("max_duration", c.config.effectiveMaxDuration()),
		slog.Duration("statement_timeout", c.config.effectiveStatementTimeout()),
	)
}

func acquireLock(ctx context.Context, conn *pgxpool.Conn) (bool, error) {
	var locked bool
	if err := conn.QueryRow(ctx, mustSQL("retention_0264_03.sql"), cleanupLockKey).Scan(&locked); err != nil {
		return false, fmt.Errorf("acquire youtube retention cleanup lock: %w", err)
	}
	return locked, nil
}

func releaseLock(ctx context.Context, conn *pgxpool.Conn, logger *slog.Logger) {
	// ctx가 취소돼도 세션 락은 반드시 해제돼야 한다. 안 하면 conn이 락을 쥔 채 pool로 반환된다.
	if _, err := conn.Exec(context.WithoutCancel(ctx), mustSQL("retention_0272_04.sql"), cleanupLockKey); err != nil && logger != nil {
		logger.Warn("release youtube retention cleanup lock failed", slog.Any("error", err))
	}
}

func yield(ctx context.Context) error {
	timer := time.NewTimer(batchYield)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (c *Cleaner) logInfo(table string, deleted int64, retentionDays, batchSize int) {
	if c.logger == nil {
		return
	}
	c.logger.Info("Cleaned up youtube retention rows",
		slog.String("table", table),
		slog.Int64("deleted", deleted),
		slog.Int("retention_days", retentionDays),
		slog.Int("batch_size", batchSize),
	)
}

func (c *Cleaner) logWarn(msg string, err error) {
	if c.logger == nil {
		return
	}
	c.logger.Warn(msg, slog.Any("error", err))
}
