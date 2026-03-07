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

// SaveStats: 채널 통계 데이터를 저장합니다.
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

// SaveStatsBatch: 여러 채널의 통계 데이터를 배치 INSERT 합니다.
// saveBatchMaxSize 단위로 분할하여 PostgreSQL 파라미터 한도를 초과하지 않도록 합니다.
func (r *StatsRepository) SaveStatsBatch(ctx context.Context, stats []*domain.TimestampedStats) error {
	if len(stats) == 0 {
		return nil
	}

	for batchStart := 0; batchStart < len(stats); batchStart += saveBatchMaxSize {
		batchEnd := batchStart + saveBatchMaxSize
		if batchEnd > len(stats) {
			batchEnd = len(stats)
		}
		chunk := stats[batchStart:batchEnd]
		if err := r.saveStatsBatchChunk(ctx, chunk); err != nil {
			return err
		}
		if !r.isLatestTableAvailable() {
			continue
		}

		if latestErr := r.upsertLatestStatsBatch(ctx, chunk); latestErr != nil {
			if isUndefinedTableError(latestErr) {
				r.markLatestTableUnavailable()
				continue
			}
			return fmt.Errorf("failed to save latest stats snapshot batch: %w", latestErr)
		}
	}

	r.logger.Debug("Stats batch saved to TimescaleDB",
		slog.Int("count", len(stats)),
	)
	return nil
}

// saveStatsBatchChunk: 단일 청크에 대한 multi-value INSERT 실행
func (r *StatsRepository) saveStatsBatchChunk(ctx context.Context, stats []*domain.TimestampedStats) error {
	var sb strings.Builder
	sb.WriteString(`INSERT INTO youtube_stats_history (time, channel_id, member_name, subscribers, videos, views) VALUES `)

	args := make([]any, 0, len(stats)*columnsPerRow)
	for i, s := range stats {
		if i > 0 {
			sb.WriteByte(',')
		}
		base := i * columnsPerRow
		sb.WriteByte('(')
		for j := 0; j < columnsPerRow; j++ {
			if j > 0 {
				sb.WriteByte(',')
			}
			sb.WriteByte('$')
			sb.WriteString(strconv.Itoa(base + j + 1))
		}
		sb.WriteByte(')')

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
		if i > 0 {
			sb.WriteByte(',')
		}
		base := i * columnsPerRow
		sb.WriteByte('(')
		for j := 0; j < columnsPerRow; j++ {
			if j > 0 {
				sb.WriteByte(',')
			}
			sb.WriteByte('$')
			sb.WriteString(strconv.Itoa(base + j + 1))
		}
		sb.WriteString(`,NOW())`)

		args = append(args, s.ChannelID, s.MemberName, s.SubscriberCount, s.VideoCount, s.ViewCount, s.Timestamp)
	}

	sb.WriteString(` ON CONFLICT (channel_id) DO UPDATE SET member_name = EXCLUDED.member_name, subscribers = EXCLUDED.subscribers, videos = EXCLUDED.videos, views = EXCLUDED.views, time = EXCLUDED.time, updated_at = NOW() WHERE youtube_channel_latest_stats.time <= EXCLUDED.time`)

	_, err := r.pool.Exec(ctx, sb.String(), args...)
	if err != nil {
		return fmt.Errorf("upsert latest stats batch (%d rows): %w", len(stats), err)
	}
	return nil
}

// RecordChange: 구독자 수 등의 변화를 기록합니다.
func (r *StatsRepository) RecordChange(ctx context.Context, change *domain.StatsChange) error {
	query := `
		INSERT INTO youtube_stats_changes
		(channel_id, member_name, subscriber_change, video_change, view_change,
		 previous_subs, current_subs, previous_videos, current_videos, detected_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`

	var prevSubs, currSubs, prevVideos, currVideos *int64

	if change.PreviousStats != nil {
		v := int64(change.PreviousStats.SubscriberCount)
		prevSubs = &v
		v2 := int64(change.PreviousStats.VideoCount)
		prevVideos = &v2
	}

	if change.CurrentStats != nil {
		v := int64(change.CurrentStats.SubscriberCount)
		currSubs = &v
		v2 := int64(change.CurrentStats.VideoCount)
		currVideos = &v2
	}

	_, err := r.pool.Exec(ctx, query,
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

// RecordChangeBatch: 여러 통계 변화를 한 번에 INSERT 합니다.
func (r *StatsRepository) RecordChangeBatch(ctx context.Context, changes []*domain.StatsChange) error {
	if len(changes) == 0 {
		return nil
	}

	for batchStart := 0; batchStart < len(changes); batchStart += saveBatchMaxSize {
		batchEnd := batchStart + saveBatchMaxSize
		if batchEnd > len(changes) {
			batchEnd = len(changes)
		}
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
		if i > 0 {
			sb.WriteByte(',')
		}
		base := i * recordChangeColumnsPerRow
		sb.WriteByte('(')
		for j := 0; j < recordChangeColumnsPerRow; j++ {
			if j > 0 {
				sb.WriteByte(',')
			}
			sb.WriteByte('$')
			sb.WriteString(strconv.Itoa(base + j + 1))
		}
		sb.WriteByte(')')

		var prevSubs, currSubs, prevVideos, currVideos *int64
		if change.PreviousStats != nil {
			v := int64(change.PreviousStats.SubscriberCount)
			prevSubs = &v
			v2 := int64(change.PreviousStats.VideoCount)
			prevVideos = &v2
		}
		if change.CurrentStats != nil {
			v := int64(change.CurrentStats.SubscriberCount)
			currSubs = &v
			v2 := int64(change.CurrentStats.VideoCount)
			currVideos = &v2
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
