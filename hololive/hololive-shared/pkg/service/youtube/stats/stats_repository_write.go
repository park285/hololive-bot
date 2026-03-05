package stats

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/kapu/hololive-shared/pkg/domain"
)

const (
	saveBatchMaxSize = 100
	columnsPerRow    = 6
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

// SaveStatsBatch: 채널 통계 데이터를 배치로 저장합니다.
func (r *StatsRepository) SaveStatsBatch(ctx context.Context, stats []*domain.TimestampedStats) error {
	if len(stats) == 0 {
		return nil
	}

	for start := 0; start < len(stats); start += saveBatchMaxSize {
		end := start + saveBatchMaxSize
		if end > len(stats) {
			end = len(stats)
		}
		chunk := stats[start:end]

		historyQuery, historyArgs := buildStatsHistoryBatchQuery(chunk)
		if _, err := r.pool.Exec(ctx, historyQuery, historyArgs...); err != nil {
			return fmt.Errorf("failed to batch save stats: %w", err)
		}

		if !r.isLatestTableAvailable() {
			continue
		}

		if err := r.upsertLatestStatsBatch(ctx, chunk); err != nil {
			if isUndefinedTableError(err) {
				r.markLatestTableUnavailable()
				continue
			}
			return fmt.Errorf("failed to save latest stats snapshot batch: %w", err)
		}
	}

	return nil
}

func buildStatsHistoryBatchQuery(stats []*domain.TimestampedStats) (string, []any) {
	placeholders := make([]string, 0, len(stats))
	args := make([]any, 0, len(stats)*columnsPerRow)

	for i, stat := range stats {
		argBase := i*columnsPerRow + 1
		placeholders = append(placeholders,
			fmt.Sprintf("($%d, $%d, $%d, $%d, $%d, $%d)", argBase, argBase+1, argBase+2, argBase+3, argBase+4, argBase+5),
		)
		args = append(args,
			stat.Timestamp,
			stat.ChannelID,
			stat.MemberName,
			stat.SubscriberCount,
			stat.VideoCount,
			stat.ViewCount,
		)
	}

	query := `
		INSERT INTO youtube_stats_history (time, channel_id, member_name, subscribers, videos, views)
		VALUES ` + strings.Join(placeholders, ",") + `
		ON CONFLICT (time, channel_id) DO UPDATE
		SET subscribers = EXCLUDED.subscribers,
		    videos = EXCLUDED.videos,
		    views = EXCLUDED.views
	`

	return query, args
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

func (r *StatsRepository) upsertLatestStatsBatch(ctx context.Context, stats []*domain.TimestampedStats) error {
	placeholders := make([]string, 0, len(stats))
	args := make([]any, 0, len(stats)*columnsPerRow)

	for i, stat := range stats {
		argBase := i*columnsPerRow + 1
		placeholders = append(placeholders,
			fmt.Sprintf("($%d, $%d, $%d, $%d, $%d, $%d, NOW())", argBase, argBase+1, argBase+2, argBase+3, argBase+4, argBase+5),
		)
		args = append(args,
			stat.ChannelID,
			stat.MemberName,
			stat.SubscriberCount,
			stat.VideoCount,
			stat.ViewCount,
			stat.Timestamp,
		)
	}

	query := `
		INSERT INTO youtube_channel_latest_stats
			(channel_id, member_name, subscribers, videos, views, time, updated_at)
		VALUES ` + strings.Join(placeholders, ",") + `
		ON CONFLICT (channel_id) DO UPDATE
		SET member_name = EXCLUDED.member_name,
		    subscribers = EXCLUDED.subscribers,
		    videos = EXCLUDED.videos,
		    views = EXCLUDED.views,
		    time = EXCLUDED.time,
		    updated_at = NOW()
		WHERE youtube_channel_latest_stats.time <= EXCLUDED.time
	`

	if _, err := r.pool.Exec(ctx, query, args...); err != nil {
		return fmt.Errorf("upsert latest stats batch: %w", err)
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
