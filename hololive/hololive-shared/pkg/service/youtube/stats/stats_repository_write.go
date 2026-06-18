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

package stats

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"strings"

	"github.com/kapu/hololive-shared/pkg/domain"
)

const (
	// saveBatchMaxSize: 한 번의 INSERT에 포함할 최대 레코드 수 (PostgreSQL 파라미터 한도 방어)
	saveBatchMaxSize = 100
	// columnsPerRow: INSERT VALUES 절 한 행의 파라미터 수
	columnsPerRow = 6
)

func (r *StatsRepository) SaveStats(ctx context.Context, stats *domain.TimestampedStats) error {
	query := `
		INSERT INTO youtube_stats_history (time, channel_id, member_name, subscribers, videos, views)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (time, channel_id) DO UPDATE
		SET subscribers = EXCLUDED.subscribers,
		    videos = EXCLUDED.videos,
		    views = EXCLUDED.views
	`

	_, err := r.pool.Exec(ctx, query,
		stats.Timestamp,
		stats.ChannelID,
		stats.MemberName,
		stats.SubscriberCount,
		stats.VideoCount,
		stats.ViewCount,
	)

	if err != nil {
		return fmt.Errorf("failed to save stats: %w", err)
	}

	// 최신 스냅샷 테이블이 있으면 함께 upsert하여 조회 비용을 줄인다.
	if r.isLatestTableAvailable() {
		if latestErr := r.upsertLatestStats(ctx, stats); latestErr != nil {
			if isUndefinedTableError(latestErr) {
				r.markLatestTableUnavailable()
			} else {
				return fmt.Errorf("failed to save latest stats snapshot: %w", latestErr)
			}
		}
	}

	r.logger.Debug("Stats saved to TimescaleDB",
		slog.String("channel", stats.ChannelID),
		slog.Any("subscribers", stats.SubscriberCount),
	)

	return nil
}

func (r *StatsRepository) upsertLatestStats(ctx context.Context, stats *domain.TimestampedStats) error {
	query := `
		INSERT INTO youtube_channel_latest_stats
			(channel_id, member_name, subscribers, videos, views, time, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, NOW())
		ON CONFLICT (channel_id) DO UPDATE
		SET member_name = EXCLUDED.member_name,
		    subscribers = EXCLUDED.subscribers,
		    videos = EXCLUDED.videos,
		    views = EXCLUDED.views,
		    time = EXCLUDED.time,
		    updated_at = NOW()
		WHERE youtube_channel_latest_stats.time <= EXCLUDED.time
	`

	_, err := r.pool.Exec(ctx, query,
		stats.ChannelID,
		stats.MemberName,
		stats.SubscriberCount,
		stats.VideoCount,
		stats.ViewCount,
		stats.Timestamp,
	)
	if err != nil {
		return fmt.Errorf("upsert latest stats for %s: %w", stats.ChannelID, err)
	}
	return nil
}

// saveBatchMaxSize 단위로 분할하여 PostgreSQL 파라미터 한도를 초과하지 않도록 합니다.
func (r *StatsRepository) SaveStatsBatch(ctx context.Context, stats []*domain.TimestampedStats) error {
	if len(stats) == 0 {
		return nil
	}

	for batchStart := 0; batchStart < len(stats); batchStart += saveBatchMaxSize {
		batchEnd := min(batchStart+saveBatchMaxSize, len(stats))
		chunk := stats[batchStart:batchEnd]
		if err := r.saveStatsBatchChunk(ctx, chunk); err != nil {
			return err
		}
		if err := r.saveLatestStatsBatchIfAvailable(ctx, chunk); err != nil {
			return err
		}
	}

	r.logger.Debug("Stats batch saved to TimescaleDB",
		slog.Int("count", len(stats)),
	)
	return nil
}

func (r *StatsRepository) saveLatestStatsBatchIfAvailable(ctx context.Context, stats []*domain.TimestampedStats) error {
	if !r.isLatestTableAvailable() {
		return nil
	}

	if latestErr := r.upsertLatestStatsBatch(ctx, stats); latestErr != nil {
		if isUndefinedTableError(latestErr) {
			r.markLatestTableUnavailable()
			return nil
		}
		return fmt.Errorf("failed to save latest stats snapshot batch: %w", latestErr)
	}
	return nil
}

// saveStatsBatchChunk: 단일 청크에 대한 multi-value INSERT 실행
func (r *StatsRepository) saveStatsBatchChunk(ctx context.Context, stats []*domain.TimestampedStats) error {
	var sb strings.Builder
	sb.WriteString(`INSERT INTO youtube_stats_history (time, channel_id, member_name, subscribers, videos, views) VALUES `)

	args := make([]any, 0, len(stats)*columnsPerRow)
	for i, s := range stats {
		writeValuePlaceholders(&sb, i, columnsPerRow, "")
		args = append(args, s.Timestamp, s.ChannelID, s.MemberName, s.SubscriberCount, s.VideoCount, s.ViewCount)
	}

	sb.WriteString(` ON CONFLICT (time, channel_id) DO UPDATE SET subscribers = EXCLUDED.subscribers, videos = EXCLUDED.videos, views = EXCLUDED.views`)

	_, err := r.pool.Exec(ctx, sb.String(), args...)
	if err != nil {
		return fmt.Errorf("failed to batch save stats (%d rows): %w", len(stats), err)
	}
	return nil
}

func (r *StatsRepository) upsertLatestStatsBatch(ctx context.Context, stats []*domain.TimestampedStats) error {
	var sb strings.Builder
	sb.WriteString(`INSERT INTO youtube_channel_latest_stats (channel_id, member_name, subscribers, videos, views, time, updated_at) VALUES `)

	args := make([]any, 0, len(stats)*columnsPerRow)
	for i, s := range stats {
		writeValuePlaceholders(&sb, i, columnsPerRow, ",NOW()")
		args = append(args, s.ChannelID, s.MemberName, s.SubscriberCount, s.VideoCount, s.ViewCount, s.Timestamp)
	}

	sb.WriteString(` ON CONFLICT (channel_id) DO UPDATE SET member_name = EXCLUDED.member_name, subscribers = EXCLUDED.subscribers, videos = EXCLUDED.videos, views = EXCLUDED.views, time = EXCLUDED.time, updated_at = NOW() WHERE youtube_channel_latest_stats.time <= EXCLUDED.time`)

	_, err := r.pool.Exec(ctx, sb.String(), args...)
	if err != nil {
		return fmt.Errorf("upsert latest stats batch (%d rows): %w", len(stats), err)
	}
	return nil
}

func writeValuePlaceholders(sb *strings.Builder, rowIndex, columns int, suffix string) {
	if rowIndex > 0 {
		sb.WriteByte(',')
	}
	base := rowIndex * columns
	sb.WriteByte('(')
	for j := range columns {
		if j > 0 {
			sb.WriteByte(',')
		}
		sb.WriteByte('$')
		sb.WriteString(strconv.Itoa(base + j + 1))
	}
	sb.WriteString(suffix)
	sb.WriteByte(')')
}

func int64PtrFromUint64(value uint64) (*int64, error) {
	if value > math.MaxInt64 {
		return nil, fmt.Errorf("value %d exceeds int64 range", value)
	}
	converted := int64(value)
	return &converted, nil
}

func nonNegativeInt64ToUint64(value int64) (uint64, bool) {
	if value < 0 {
		return 0, false
	}
	return uint64(value), true
}

func (r *StatsRepository) RecordChange(ctx context.Context, change *domain.StatsChange) error {
	query := `
		INSERT INTO youtube_stats_changes
		(channel_id, member_name, subscriber_change, video_change, view_change,
		 previous_subs, current_subs, previous_videos, current_videos, detected_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`

	prevSubs, currSubs, prevVideos, currVideos, err := statsChangePreviousCurrentValues(change)
	if err != nil {
		return err
	}

	_, err = r.pool.Exec(ctx, query,
		change.ChannelID,
		change.MemberName,
		change.SubscriberChange,
		change.VideoChange,
		change.ViewChange,
		prevSubs,
		currSubs,
		prevVideos,
		currVideos,
		change.DetectedAt,
	)

	if err != nil {
		return fmt.Errorf("failed to record change: %w", err)
	}

	return nil
}

// recordChangeColumnsPerRow: RecordChangeBatch INSERT VALUES 절 한 행의 파라미터 수
const recordChangeColumnsPerRow = 10

func (r *StatsRepository) RecordChangeBatch(ctx context.Context, changes []*domain.StatsChange) error {
	if len(changes) == 0 {
		return nil
	}

	for batchStart := 0; batchStart < len(changes); batchStart += saveBatchMaxSize {
		batchEnd := min(batchStart+saveBatchMaxSize, len(changes))
		if err := r.recordChangeBatchChunk(ctx, changes[batchStart:batchEnd]); err != nil {
			return err
		}
	}
	return nil
}

func (r *StatsRepository) recordChangeBatchChunk(ctx context.Context, changes []*domain.StatsChange) error {
	var sb strings.Builder
	sb.WriteString(`INSERT INTO youtube_stats_changes (channel_id, member_name, subscriber_change, video_change, view_change, previous_subs, current_subs, previous_videos, current_videos, detected_at) VALUES `)

	args := make([]any, 0, len(changes)*recordChangeColumnsPerRow)
	for i, change := range changes {
		writeValuePlaceholders(&sb, i, recordChangeColumnsPerRow, "")
		prevSubs, currSubs, prevVideos, currVideos, err := changeBatchPreviousCurrentValues(change)
		if err != nil {
			return err
		}

		args = append(args,
			change.ChannelID, change.MemberName,
			change.SubscriberChange, change.VideoChange, change.ViewChange,
			prevSubs, currSubs, prevVideos, currVideos,
			change.DetectedAt,
		)
	}

	_, err := r.pool.Exec(ctx, sb.String(), args...)
	if err != nil {
		return fmt.Errorf("failed to batch record changes (%d rows): %w", len(changes), err)
	}
	return nil
}

func changeBatchPreviousCurrentValues(change *domain.StatsChange) (prevSubs, currSubs, prevVideos, currVideos *int64, err error) {
	return statsChangePreviousCurrentValues(change)
}

func statsChangePreviousCurrentValues(change *domain.StatsChange) (prevSubs, currSubs, prevVideos, currVideos *int64, err error) {
	prevSubs, prevVideos, err = statsSnapshotSubscriberVideoCounts("previous", change.PreviousStats)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	currSubs, currVideos, err = statsSnapshotSubscriberVideoCounts("current", change.CurrentStats)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	return prevSubs, currSubs, prevVideos, currVideos, nil
}

func statsSnapshotSubscriberVideoCounts(label string, stats *domain.TimestampedStats) (subscriberCount, videoCount *int64, err error) {
	if stats == nil {
		return nil, nil, nil
	}
	subscriberCount, err = int64PtrFromUint64(stats.SubscriberCount)
	if err != nil {
		return nil, nil, fmt.Errorf("%s subscriber count: %w", label, err)
	}
	videoCount, err = int64PtrFromUint64(stats.VideoCount)
	if err != nil {
		return nil, nil, fmt.Errorf("%s video count: %w", label, err)
	}
	return subscriberCount, videoCount, nil
}
