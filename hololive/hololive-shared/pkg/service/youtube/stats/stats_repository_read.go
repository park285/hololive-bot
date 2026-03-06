package stats

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/kapu/hololive-shared/pkg/domain"
)

// GetLatestStats: 각 채널의 최신 통계 데이터를 조회합니다.
func (r *StatsRepository) GetLatestStats(ctx context.Context, channelID string) (*domain.TimestampedStats, error) {
	if r.isLatestTableAvailable() {
		stats, err := r.getLatestStatsFromSnapshot(ctx, channelID)
		if err == nil {
			return stats, nil
		}
		if isUndefinedTableError(err) {
			r.markLatestTableUnavailable()
		} else {
			return nil, fmt.Errorf("failed to get latest stats from snapshot: %w", err)
		}
	}

	return r.getLatestStatsFromHistory(ctx, channelID)
}

// GetLatestStatsForChannels: 여러 채널의 최신 통계를 한 번에 조회한다. (N+1 쿼리 방지)
func (r *StatsRepository) GetLatestStatsForChannels(ctx context.Context, channelIDs []string) (map[string]*domain.TimestampedStats, error) {
	if len(channelIDs) == 0 {
		return make(map[string]*domain.TimestampedStats), nil
	}

	if r.isLatestTableAvailable() {
		result, err := r.getLatestStatsForChannelsFromSnapshot(ctx, channelIDs)
		if err == nil {
			return result, nil
		}
		if isUndefinedTableError(err) {
			r.markLatestTableUnavailable()
		} else {
			return nil, fmt.Errorf("failed to batch query latest stats snapshot: %w", err)
		}
	}

	return r.getLatestStatsForChannelsFromHistory(ctx, channelIDs)
}

func (r *StatsRepository) getLatestStatsFromHistory(ctx context.Context, channelID string) (*domain.TimestampedStats, error) {
	query := `
		SELECT time, channel_id, member_name, subscribers, videos, views
		FROM youtube_stats_history
		WHERE channel_id = $1
		ORDER BY time DESC
		LIMIT 1
	`

	var stats domain.TimestampedStats
	var memberName *string

	err := r.pool.QueryRow(ctx, query, channelID).Scan(
		&stats.Timestamp,
		&stats.ChannelID,
		&memberName,
		&stats.SubscriberCount,
		&stats.VideoCount,
		&stats.ViewCount,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get latest stats: %w", err)
	}

	if memberName != nil {
		stats.MemberName = *memberName
	}

	return &stats, nil
}

func (r *StatsRepository) getLatestStatsForChannelsFromHistory(ctx context.Context, channelIDs []string) (map[string]*domain.TimestampedStats, error) {
	// PostgreSQL DISTINCT ON 기반 fallback 조회
	query := `
		SELECT DISTINCT ON (channel_id)
			time, channel_id, member_name, subscribers, videos, views
		FROM youtube_stats_history
		WHERE channel_id = ANY($1::text[])
		ORDER BY channel_id, time DESC
	`

	rows, err := r.pool.Query(ctx, query, channelIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to batch query stats: %w", err)
	}
	defer rows.Close()

	result := make(map[string]*domain.TimestampedStats, len(channelIDs))
	for rows.Next() {
		var stats domain.TimestampedStats
		var memberName *string

		if err := rows.Scan(
			&stats.Timestamp,
			&stats.ChannelID,
			&memberName,
			&stats.SubscriberCount,
			&stats.VideoCount,
			&stats.ViewCount,
		); err != nil {
			r.logger.Warn("Failed to scan batch stats row", slog.Any("error", err))
			continue
		}

		if memberName != nil {
			stats.MemberName = *memberName
		}
		result[stats.ChannelID] = &stats
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration error: %w", err)
	}

	return result, nil
}

func (r *StatsRepository) getLatestStatsFromSnapshot(ctx context.Context, channelID string) (*domain.TimestampedStats, error) {
	query := `
		SELECT time, channel_id, member_name, subscribers, videos, views
		FROM youtube_channel_latest_stats
		WHERE channel_id = $1
		LIMIT 1
	`

	var stats domain.TimestampedStats
	var memberName *string

	err := r.pool.QueryRow(ctx, query, channelID).Scan(
		&stats.Timestamp,
		&stats.ChannelID,
		&memberName,
		&stats.SubscriberCount,
		&stats.VideoCount,
		&stats.ViewCount,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan latest stats for %s: %w", channelID, err)
	}

	if memberName != nil {
		stats.MemberName = *memberName
	}
	return &stats, nil
}

func (r *StatsRepository) getLatestStatsForChannelsFromSnapshot(ctx context.Context, channelIDs []string) (map[string]*domain.TimestampedStats, error) {
	query := `
		SELECT time, channel_id, member_name, subscribers, videos, views
		FROM youtube_channel_latest_stats
		WHERE channel_id = ANY($1::text[])
	`

	rows, err := r.pool.Query(ctx, query, channelIDs)
	if err != nil {
		return nil, fmt.Errorf("query batch latest stats: %w", err)
	}
	defer rows.Close()

	result := make(map[string]*domain.TimestampedStats, len(channelIDs))
	for rows.Next() {
		var stats domain.TimestampedStats
		var memberName *string

		if err := rows.Scan(
			&stats.Timestamp,
			&stats.ChannelID,
			&memberName,
			&stats.SubscriberCount,
			&stats.VideoCount,
			&stats.ViewCount,
		); err != nil {
			r.logger.Warn("Failed to scan batch stats row", slog.Any("error", err))
			continue
		}

		if memberName != nil {
			stats.MemberName = *memberName
		}
		result[stats.ChannelID] = &stats
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration error: %w", err)
	}

	return result, nil
}

func (r *StatsRepository) isLatestTableAvailable() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.latestTableAvailable
}

func (r *StatsRepository) markLatestTableUnavailable() {
	r.mu.Lock()
	alreadyUnavailable := !r.latestTableAvailable
	r.latestTableAvailable = false
	r.mu.Unlock()

	if alreadyUnavailable {
		return
	}

	if r.logger != nil {
		r.logger.Warn("latest_stats_snapshot_disabled",
			slog.String("table", "youtube_channel_latest_stats"),
			slog.String("reason", "table_not_found"),
		)
	}
}

func isUndefinedTableError(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "42P01"
	}
	return false
}

// GetTopGainers: 특정 시점 이후 구독자 증가량이 가장 높은 채널 상위 목록을 조회합니다.
func (r *StatsRepository) GetTopGainers(ctx context.Context, since time.Time, limit int) ([]domain.RankEntry, error) {
	query := `
		WITH latest AS (
			SELECT DISTINCT ON (channel_id)
				channel_id, member_name, subscribers
			FROM youtube_stats_history
			WHERE time >= $1
			ORDER BY channel_id, time DESC
		),
		earliest AS (
			SELECT DISTINCT ON (channel_id)
				channel_id, subscribers
			FROM youtube_stats_history
			WHERE time >= $1
			ORDER BY channel_id, time ASC
		)
		SELECT
			latest.channel_id,
			latest.member_name,
			(latest.subscribers - earliest.subscribers) AS gain,
			latest.subscribers AS current_subscribers
		FROM latest
		JOIN earliest ON latest.channel_id = earliest.channel_id
		WHERE (latest.subscribers - earliest.subscribers) > 0
		ORDER BY gain DESC
		LIMIT $2
	`

	rows, err := r.pool.Query(ctx, query, since, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query top gainers: %w", err)
	}
	defer rows.Close()

	entries := make([]domain.RankEntry, 0, limit)
	rank := 1
	for rows.Next() {
		var entry domain.RankEntry
		var currentSubs int64
		if err := rows.Scan(&entry.ChannelID, &entry.MemberName, &entry.Value, &currentSubs); err != nil {
			r.logger.Warn("Failed to scan rank entry", slog.Any("error", err))
			continue
		}
		if currentSubs > 0 {
			entry.CurrentSubscribers = uint64(currentSubs)
		}
		entry.Rank = rank
		entries = append(entries, entry)
		rank++
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration error: %w", err)
	}

	return entries, nil
}
