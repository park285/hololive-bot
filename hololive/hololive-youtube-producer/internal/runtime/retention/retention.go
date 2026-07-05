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
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller"
	sharedenv "github.com/park285/shared-go/pkg/envutil"
)

const (
	cleanupLockKey   int64 = 841977302
	defaultBatchSize       = 1000
	defaultInterval        = time.Hour
	batchYield             = 10 * time.Millisecond

	channelSnapshotsDaysEnv = "YOUTUBE_PRODUCER_RETENTION_CHANNEL_SNAPSHOTS_DAYS"
	liveSessionsDaysEnv     = "YOUTUBE_PRODUCER_RETENTION_LIVE_SESSIONS_DAYS"
	viewerSamplesDaysEnv    = "YOUTUBE_PRODUCER_RETENTION_VIEWER_SAMPLES_DAYS"
)

// BRIN(time/captured_at)은 정렬을 제공하지 않아 ORDER BY 시 매 배치가 cutoff 미만 잔여 전량을 스캔+정렬한다. cutoff 미만 전량 삭제라 순서 무관 → 생략(live_sessions는 btree idx_yls_ended_cleanup이라 ORDER BY 유지).
const deleteChannelSnapshotsSQL = `
WITH picked AS (
	SELECT channel_id, captured_at
	FROM youtube_channel_stats_snapshots
	WHERE captured_at < $1
	LIMIT $2
)
DELETE FROM youtube_channel_stats_snapshots s
USING picked
WHERE s.channel_id = picked.channel_id AND s.captured_at = picked.captured_at`

// youtube_live_viewer_samples cleaner(shared poller/internal/cleaner.go)가 이 테이블을 JOIN
// 게이트로 써서 삭제 대상 샘플을 고른다. 샘플이 남은 세션을 먼저 지우면 그 게이트가 사라져
// 해당 샘플이 영구 고아가 되므로(두 테이블 사이 FK·cascade 없음), 샘플이 모두 지워진 세션만 삭제한다.
const deleteLiveSessionsSQL = `
WITH picked AS (
	SELECT l.video_id
	FROM youtube_live_sessions l
	WHERE l.status = 'ENDED' AND l.ended_at < $1
	  AND NOT EXISTS (
		SELECT 1 FROM youtube_live_viewer_samples s WHERE s.video_id = l.video_id
	)
	ORDER BY l.ended_at ASC, l.video_id ASC
	LIMIT $2
)
DELETE FROM youtube_live_sessions l
USING picked
WHERE l.video_id = picked.video_id`

type Config struct {
	ChannelSnapshotsDays int
	LiveSessionsDays     int
	ViewerSamplesDays    int
	BatchSize            int
	Interval             time.Duration
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
		return c.BatchSize
	}
	return defaultBatchSize
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
}

func NewCleaner(pool *pgxpool.Pool, config Config, logger *slog.Logger) *Cleaner {
	c := &Cleaner{pool: pool, config: config, logger: logger}
	if pool != nil && config.ViewerSamplesDays > 0 {
		c.viewerCleaner = poller.NewViewerSampleCleaner(pool, poller.ViewerSampleCleanerConfig{
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
		if !sleepContext(ctx, interval) {
			return
		}
	}
}

func (c *Cleaner) Cleanup(ctx context.Context) (int64, error) {
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

func (c *Cleaner) cleanupTargets(ctx context.Context, conn *pgxpool.Conn) (int64, error) {
	batchSize := c.config.effectiveBatchSize()
	var total int64
	for _, t := range c.targets() {
		if t.retentionDays <= 0 {
			continue
		}
		cutoff := cutoffFor(time.Now(), t.retentionDays)
		deleted, err := deleteBatches(ctx, conn, t.deleteSQL, cutoff, batchSize)
		total += deleted
		if err != nil {
			return total, fmt.Errorf("cleanup %s: %w", t.name, err)
		}
		if deleted > 0 {
			c.logInfo(t.name, deleted, t.retentionDays, batchSize)
		}
	}
	return total, nil
}

func deleteBatches(ctx context.Context, conn *pgxpool.Conn, deleteSQL string, cutoff time.Time, batchSize int) (int64, error) {
	var total int64
	for {
		rows, err := deleteOneBatch(ctx, conn, deleteSQL, cutoff, batchSize)
		total += rows
		if err != nil {
			return total, err
		}
		if rows < int64(batchSize) {
			return total, nil
		}
		if err := yield(ctx); err != nil {
			return total, err
		}
	}
}

func deleteOneBatch(ctx context.Context, conn *pgxpool.Conn, deleteSQL string, cutoff time.Time, batchSize int) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	tag, err := conn.Exec(ctx, deleteSQL, cutoff, batchSize)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func acquireLock(ctx context.Context, conn *pgxpool.Conn) (bool, error) {
	var locked bool
	if err := conn.QueryRow(ctx, "SELECT pg_try_advisory_lock($1)", cleanupLockKey).Scan(&locked); err != nil {
		return false, fmt.Errorf("acquire youtube retention cleanup lock: %w", err)
	}
	return locked, nil
}

func releaseLock(ctx context.Context, conn *pgxpool.Conn, logger *slog.Logger) {
	// ctx가 취소돼도 세션 락은 반드시 해제돼야 한다. 안 하면 conn이 락을 쥔 채 pool로 반환된다.
	if _, err := conn.Exec(context.WithoutCancel(ctx), "SELECT pg_advisory_unlock($1)", cleanupLockKey); err != nil && logger != nil {
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

func sleepContext(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
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
