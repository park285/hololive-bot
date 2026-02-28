package youtube

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/json"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/database"
)

// StatsRepository: YouTube 채널 통계 데이터(구독자 수 등)를 관리하는 저장소 (TimescaleDB)
type StatsRepository struct {
	pool   *pgxpool.Pool
	logger *slog.Logger

	mu                   sync.RWMutex
	latestTableAvailable bool
}

// NewYouTubeStatsRepository: 새로운 StatsRepository 인스턴스를 생성합니다.
func NewYouTubeStatsRepository(postgres *database.PostgresService, logger *slog.Logger) *StatsRepository {
	return &StatsRepository{
		pool:                 postgres.GetPool(),
		logger:               logger,
		latestTableAvailable: true,
	}
}

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

// GetAchievedMilestones: 여러 채널의 달성된 마일스톤을 배치 조회한다. (N+1 쿼리 방지)
// 반환: map[channelID][]uint64 (채널별 달성된 마일스톤 값 목록)
func (r *StatsRepository) GetAchievedMilestones(ctx context.Context, channelIDs []string, milestoneType domain.MilestoneType) (map[string][]uint64, error) {
	if len(channelIDs) == 0 {
		return make(map[string][]uint64), nil
	}

	query := `
		SELECT channel_id, value
		FROM youtube_milestones
		WHERE channel_id = ANY($1::text[]) AND type = $2
		ORDER BY channel_id, value
	`

	rows, err := r.pool.Query(ctx, query, channelIDs, string(milestoneType))
	if err != nil {
		return nil, fmt.Errorf("failed to batch query milestones: %w", err)
	}
	defer rows.Close()

	result := make(map[string][]uint64, len(channelIDs))
	for rows.Next() {
		var channelID string
		var value uint64

		if err := rows.Scan(&channelID, &value); err != nil {
			r.logger.Warn("Failed to scan milestone row", slog.Any("error", err))
			continue
		}

		result[channelID] = append(result[channelID], value)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration error: %w", err)
	}

	return result, nil
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

// RecordMilestone: 구독자 수 달성 등 마일스톤 이벤트를 기록합니다.
func (r *StatsRepository) SaveMilestone(ctx context.Context, milestone *domain.Milestone) error {
	query := `
		INSERT INTO youtube_milestones (channel_id, member_name, type, value, achieved_at, notified)
		VALUES ($1, $2, $3, $4, $5, $6)
	`

	_, err := r.pool.Exec(ctx, query,
		milestone.ChannelID,
		milestone.MemberName,
		string(milestone.Type),
		milestone.Value,
		milestone.AchievedAt,
		milestone.Notified,
	)

	if err != nil {
		return fmt.Errorf("failed to save milestone: %w", err)
	}

	r.logger.Info("Milestone saved",
		slog.String("member", milestone.MemberName),
		slog.String("type", string(milestone.Type)),
		slog.Any("value", milestone.Value),
	)

	return nil
}

// HasAchievedMilestone: 특정 채널이 특정 마일스톤을 이미 달성했는지 확인합니다.
// 구독자가 감소 후 다시 증가해도 중복 달성으로 처리되지 않도록 방지한다.
func (r *StatsRepository) HasAchievedMilestone(ctx context.Context, channelID string, milestoneType domain.MilestoneType, value uint64) (bool, error) {
	query := `
		SELECT EXISTS(
			SELECT 1 FROM youtube_milestones
			WHERE channel_id = $1 AND type = $2 AND value = $3
		)
	`

	var exists bool
	err := r.pool.QueryRow(ctx, query, channelID, string(milestoneType), value).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("failed to check milestone: %w", err)
	}

	return exists, nil
}

// GetUnnotifiedChanges: 아직 알림이 발송되지 않은 통계 변화 내역을 최신순으로 조회합니다.
// PreviousStats와 CurrentStats를 복원하여 마일스톤 검출이 가능하도록 한다.
func (r *StatsRepository) GetUnnotifiedChanges(ctx context.Context, limit int) ([]*domain.StatsChange, error) {
	query := `
		SELECT channel_id, member_name, subscriber_change, video_change, view_change, 
		       previous_subs, current_subs, detected_at
		FROM youtube_stats_changes
		WHERE notified = false
		ORDER BY detected_at DESC
		LIMIT $1
	`

	rows, err := r.pool.Query(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query unnotified changes: %w", err)
	}
	defer rows.Close()

	var changes []*domain.StatsChange
	for rows.Next() {
		var change domain.StatsChange
		var prevSubs, currSubs *int64

		if err := rows.Scan(
			&change.ChannelID,
			&change.MemberName,
			&change.SubscriberChange,
			&change.VideoChange,
			&change.ViewChange,
			&prevSubs,
			&currSubs,
			&change.DetectedAt,
		); err != nil {
			r.logger.Warn("Failed to scan change row", slog.Any("error", err))
			continue
		}

		// PreviousStats/CurrentStats 복원 (마일스톤 검출에 필요)
		if prevSubs != nil {
			change.PreviousStats = &domain.TimestampedStats{
				ChannelID:       change.ChannelID,
				MemberName:      change.MemberName,
				SubscriberCount: uint64(*prevSubs),
			}
		}
		if currSubs != nil {
			change.CurrentStats = &domain.TimestampedStats{
				ChannelID:       change.ChannelID,
				MemberName:      change.MemberName,
				SubscriberCount: uint64(*currSubs),
			}
		}

		changes = append(changes, &change)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration error: %w", err)
	}

	return changes, nil
}

// MarkChangeNotified: 특정 통계 변화 내역을 알림 발송 완료 상태로 처리합니다.
func (r *StatsRepository) MarkChangeNotified(ctx context.Context, channelID string, detectedAt time.Time) error {
	query := `
		UPDATE youtube_stats_changes
		SET notified = true
		WHERE channel_id = $1 AND detected_at = $2
	`

	_, err := r.pool.Exec(ctx, query, channelID, detectedAt)
	if err != nil {
		return fmt.Errorf("failed to mark change notified: %w", err)
	}

	return nil
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

	var entries []domain.RankEntry
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

// MilestoneEntry: API 응답용 마일스톤 정보
type MilestoneEntry struct {
	ChannelID  string    `json:"channelId"`
	MemberName string    `json:"memberName"`
	Type       string    `json:"type"`
	Value      uint64    `json:"value"`
	AchievedAt time.Time `json:"achievedAt"`
	Notified   bool      `json:"notified"`
}

// MilestoneFilter: 마일스톤 조회 필터
type MilestoneFilter struct {
	Limit      int
	Offset     int
	ChannelID  string
	MemberName string
}

// MilestoneResult: 마일스톤 조회 결과 (페이지네이션 정보 포함)
type MilestoneResult struct {
	Milestones []MilestoneEntry `json:"milestones"`
	Total      int              `json:"total"`
	Limit      int              `json:"limit"`
	Offset     int              `json:"offset"`
}

// GetAllMilestones: 달성된 마일스톤 목록을 조회한다 (페이지네이션/필터링 지원)
func (r *StatsRepository) GetAllMilestones(ctx context.Context, filter MilestoneFilter) (*MilestoneResult, error) {
	var whereClauses []string
	var args []any
	argIdx := 1

	if filter.ChannelID != "" {
		whereClauses = append(whereClauses, fmt.Sprintf("channel_id = $%d", argIdx))
		args = append(args, filter.ChannelID)
		argIdx++
	}
	if filter.MemberName != "" {
		whereClauses = append(whereClauses, fmt.Sprintf("member_name ILIKE $%d", argIdx))
		args = append(args, "%"+filter.MemberName+"%")
		argIdx++
	}

	whereSQL := ""
	if len(whereClauses) > 0 {
		whereSQL = "WHERE " + strings.Join(whereClauses, " AND ")
	}

	// 1. Count Total
	countQuery := "SELECT COUNT(*) FROM youtube_milestones " + whereSQL
	var totalCount int
	if err := r.pool.QueryRow(ctx, countQuery, args...).Scan(&totalCount); err != nil {
		return nil, fmt.Errorf("failed to count milestones: %w", err)
	}

	// 2. Select Data
	// nolint:gosec // G201: 동적 WHERE 절은 파라미터화된 값만 사용하므로 안전
	query := fmt.Sprintf(`
		SELECT channel_id, member_name, type, value, achieved_at, notified
		FROM youtube_milestones
		%s
		ORDER BY achieved_at DESC
		LIMIT $%d OFFSET $%d
	`, whereSQL, argIdx, argIdx+1)

	args = append(args, filter.Limit, filter.Offset)

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query milestones: %w", err)
	}
	defer rows.Close()

	var entries []MilestoneEntry
	for rows.Next() {
		var e MilestoneEntry
		if err := rows.Scan(&e.ChannelID, &e.MemberName, &e.Type, &e.Value, &e.AchievedAt, &e.Notified); err != nil {
			r.logger.Warn("Failed to scan milestone entry", slog.Any("error", err))
			continue
		}
		entries = append(entries, e)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration error: %w", err)
	}

	return &MilestoneResult{
		Milestones: entries,
		Total:      totalCount,
		Limit:      filter.Limit,
		Offset:     filter.Offset,
	}, nil
}

// NearMilestoneEntry: API 응답용 마일스톤 직전 멤버 정보
type NearMilestoneEntry struct {
	ChannelID     string  `json:"channelId"`
	MemberName    string  `json:"memberName"`
	CurrentSubs   uint64  `json:"currentSubs"`
	NextMilestone uint64  `json:"nextMilestone"`
	Remaining     int64   `json:"remaining"`
	ProgressPct   float64 `json:"progressPct"`
}

// GetNearMilestoneMembers: 마일스톤 직전(threshold% 이상) 멤버를 조회한다. 졸업 멤버 제외, Limit 지원.
func (r *StatsRepository) GetNearMilestoneMembers(ctx context.Context, thresholdPct float64, milestones []uint64, limit int) ([]NearMilestoneEntry, error) {
	if len(milestones) == 0 {
		return nil, nil
	}

	// CTE를 사용한 효율적인 쿼리
	// pgx는 슬라이스를 직접 전달하고 SQL에서 캐스팅
	query := `
		WITH latest_stats AS (
			SELECT DISTINCT ON (h.channel_id)
				h.channel_id, h.member_name, h.subscribers
			FROM youtube_stats_history h
			JOIN members m ON h.channel_id = m.channel_id
			WHERE m.is_graduated = false
			ORDER BY h.channel_id, h.time DESC
		),
		milestones AS (
			SELECT unnest($1::bigint[]) as milestone
		),
		achieved AS (
			SELECT channel_id, value
			FROM youtube_milestones
			WHERE type = 'subscribers'
		)
		SELECT 
			ls.channel_id,
			ls.member_name,
			ls.subscribers as current_subs,
			m.milestone as next_milestone,
			m.milestone - ls.subscribers as remaining,
			ROUND(100.0 * ls.subscribers / m.milestone, 2) as progress_pct
		FROM latest_stats ls
		CROSS JOIN milestones m
		LEFT JOIN achieved a ON ls.channel_id = a.channel_id AND m.milestone = a.value
		WHERE ls.subscribers < m.milestone 
			AND ls.subscribers >= m.milestone::float8 * $2::float8
			AND a.channel_id IS NULL
			AND ls.member_name IS NOT NULL
		ORDER BY progress_pct DESC
		LIMIT $3
	`

	rows, err := r.pool.Query(ctx, query, milestones, thresholdPct, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query near milestone members: %w", err)
	}
	defer rows.Close()

	var entries []NearMilestoneEntry
	for rows.Next() {
		var e NearMilestoneEntry
		if err := rows.Scan(&e.ChannelID, &e.MemberName, &e.CurrentSubs, &e.NextMilestone, &e.Remaining, &e.ProgressPct); err != nil {
			r.logger.Warn("Failed to scan near milestone entry", slog.Any("error", err))
			continue
		}
		entries = append(entries, e)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration error: %w", err)
	}

	return entries, nil
}

// CountNearMilestoneMembers: 마일스톤 직전(threshold% 이상) 멤버 수를 조회한다. 졸업 멤버 제외.
func (r *StatsRepository) CountNearMilestoneMembers(ctx context.Context, thresholdPct float64, milestones []uint64) (int, error) {
	if len(milestones) == 0 {
		return 0, nil
	}

	query := `
		WITH latest_stats AS (
			SELECT DISTINCT ON (h.channel_id)
				h.channel_id, h.member_name, h.subscribers
			FROM youtube_stats_history h
			JOIN members m ON h.channel_id = m.channel_id
			WHERE m.is_graduated = false
			ORDER BY h.channel_id, h.time DESC
		),
		milestones AS (
			SELECT unnest($1::bigint[]) as milestone
		),
		achieved AS (
			SELECT channel_id, value
			FROM youtube_milestones
			WHERE type = 'subscribers'
		)
		SELECT COUNT(*)
		FROM latest_stats ls
		CROSS JOIN milestones m
		LEFT JOIN achieved a ON ls.channel_id = a.channel_id AND m.milestone = a.value
		WHERE ls.subscribers < m.milestone
			AND ls.subscribers >= m.milestone::float8 * $2::float8
			AND a.channel_id IS NULL
			AND ls.member_name IS NOT NULL
	`

	var count int
	if err := r.pool.QueryRow(ctx, query, milestones, thresholdPct).Scan(&count); err != nil {
		return 0, fmt.Errorf("count near milestone members: %w", err)
	}

	return count, nil
}

// GetClosestMilestoneMembers: 마일스톤 달성률이 높은 순서대로 상위 멤버를 조회한다 (threshold 없음, 졸업 멤버 자동 제외)
// allowedChannelIDs는 더 이상 사용하지 않고 DB JOIN으로 처리함
func (r *StatsRepository) GetClosestMilestoneMembers(ctx context.Context, limit int, milestones []uint64) ([]NearMilestoneEntry, error) {
	if len(milestones) == 0 {
		return nil, nil
	}

	query := `
		WITH latest_stats AS (
			SELECT DISTINCT ON (h.channel_id)
				h.channel_id, h.member_name, h.subscribers
			FROM youtube_stats_history h
			JOIN members m ON h.channel_id = m.channel_id
			WHERE m.is_graduated = false
			ORDER BY h.channel_id, h.time DESC
		),
		milestones AS (
			SELECT unnest($1::bigint[]) as milestone
		),
		next_milestones AS (
            SELECT 
                ls.channel_id,
                ls.member_name,
                ls.subscribers,
                MIN(m.milestone) as next_milestone
            FROM latest_stats ls
            CROSS JOIN milestones m
            WHERE ls.subscribers < m.milestone
            GROUP BY ls.channel_id, ls.member_name, ls.subscribers
        )
		SELECT 
			nm.channel_id,
			nm.member_name,
			nm.subscribers as current_subs,
			nm.next_milestone,
			nm.next_milestone - nm.subscribers as remaining,
			ROUND(100.0 * nm.subscribers / nm.next_milestone, 2) as progress_pct
		FROM next_milestones nm
        LEFT JOIN youtube_milestones ym ON nm.channel_id = ym.channel_id AND nm.next_milestone = ym.value
        WHERE ym.channel_id IS NULL
		ORDER BY progress_pct DESC
		LIMIT $2
	`

	rows, err := r.pool.Query(ctx, query, milestones, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query closest milestone members: %w", err)
	}
	defer rows.Close()

	var entries []NearMilestoneEntry
	for rows.Next() {
		var e NearMilestoneEntry
		if err := rows.Scan(&e.ChannelID, &e.MemberName, &e.CurrentSubs, &e.NextMilestone, &e.Remaining, &e.ProgressPct); err != nil {
			r.logger.Warn("Failed to scan closest milestone entry", slog.Any("error", err))
			continue
		}
		entries = append(entries, e)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration error: %w", err)
	}

	return entries, nil
}

// MilestoneStats: 마일스톤 관련 통계 요약
type MilestoneStats struct {
	TotalAchieved      int `json:"totalAchieved"`
	TotalNearMilestone int `json:"totalNearMilestone"`
	RecentAchievements int `json:"recentAchievements"` // 최근 30일
	NotNotifiedCount   int `json:"notNotifiedCount"`
}

// GetMilestoneStats: 마일스톤 통계 요약을 조회한다
func (r *StatsRepository) GetMilestoneStats(ctx context.Context) (*MilestoneStats, error) {
	query := `
		SELECT
			(SELECT COUNT(*) FROM youtube_milestones) as total_achieved,
			(SELECT COUNT(*) FROM youtube_milestones WHERE achieved_at > NOW() - INTERVAL '30 days') as recent,
			(SELECT COUNT(*) FROM youtube_milestones WHERE notified = false) as not_notified
	`

	var stats MilestoneStats
	err := r.pool.QueryRow(ctx, query).Scan(
		&stats.TotalAchieved,
		&stats.RecentAchievements,
		&stats.NotNotifiedCount,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get milestone stats: %w", err)
	}

	return &stats, nil
}

// ApproachingNotification: 마일스톤 접근 예고 알림 정보
type ApproachingNotification struct {
	ChannelID      string    `json:"channelId"`
	MemberName     string    `json:"memberName"`
	MilestoneValue uint64    `json:"milestoneValue"`
	CurrentSubs    uint64    `json:"currentSubs"`
	Remaining      uint64    `json:"remaining"`
	NotifiedAt     time.Time `json:"notifiedAt"`
}

// HasApproachingNotified: 특정 마일스톤에 대해 예고 알림이 이미 발송되었는지 확인합니다.
func (r *StatsRepository) HasApproachingNotified(ctx context.Context, channelID string, milestoneValue uint64) (bool, error) {
	query := `
		SELECT EXISTS(
			SELECT 1 FROM youtube_milestone_approaching
			WHERE channel_id = $1 AND milestone_value = $2
		)
	`

	var exists bool
	err := r.pool.QueryRow(ctx, query, channelID, milestoneValue).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("failed to check approaching notification: %w", err)
	}

	return exists, nil
}

// SaveApproachingNotification: 마일스톤 접근 예고 알림 기록을 저장합니다.
func (r *StatsRepository) SaveApproachingNotification(ctx context.Context, channelID string, milestoneValue, currentSubs uint64, notifiedAt time.Time) error {
	query := `
		INSERT INTO youtube_milestone_approaching (channel_id, milestone_value, current_subs, notified_at)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (channel_id, milestone_value) DO NOTHING
	`

	_, err := r.pool.Exec(ctx, query, channelID, milestoneValue, currentSubs, notifiedAt)
	if err != nil {
		return fmt.Errorf("failed to save approaching notification: %w", err)
	}

	r.logger.Info("Approaching notification saved",
		slog.String("channel", channelID),
		slog.Any("milestone", milestoneValue),
		slog.Any("current_subs", currentSubs))

	return nil
}

// GetUnnotifiedApproaching: 아직 채팅방에 발송되지 않은 예고 알림 목록을 조회합니다.
// 이 함수는 SendMilestoneAlerts와 유사한 패턴으로 예고 알람을 발송할 때 사용된다.
func (r *StatsRepository) GetUnnotifiedApproaching(ctx context.Context, limit int) ([]ApproachingNotification, error) {
	query := `
		SELECT a.channel_id, m.english_name, a.milestone_value, a.current_subs, a.notified_at
		FROM youtube_milestone_approaching a
		JOIN members m ON a.channel_id = m.channel_id
		WHERE a.chat_notified = false
		ORDER BY a.notified_at DESC
		LIMIT $1
	`

	rows, err := r.pool.Query(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query unnotified approaching: %w", err)
	}
	defer rows.Close()

	var notifications []ApproachingNotification
	for rows.Next() {
		var n ApproachingNotification
		if err := rows.Scan(&n.ChannelID, &n.MemberName, &n.MilestoneValue, &n.CurrentSubs, &n.NotifiedAt); err != nil {
			r.logger.Warn("Failed to scan approaching notification", slog.Any("error", err))
			continue
		}
		n.Remaining = n.MilestoneValue - n.CurrentSubs
		notifications = append(notifications, n)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration error: %w", err)
	}

	return notifications, nil
}

// MarkApproachingChatNotified: 예고 알림의 채팅방 발송 완료 상태를 업데이트합니다.
func (r *StatsRepository) MarkApproachingChatNotified(ctx context.Context, channelID string, milestoneValue uint64) error {
	query := `
		UPDATE youtube_milestone_approaching
		SET chat_notified = true
		WHERE channel_id = $1 AND milestone_value = $2
	`

	_, err := r.pool.Exec(ctx, query, channelID, milestoneValue)
	if err != nil {
		return fmt.Errorf("failed to mark approaching chat notified: %w", err)
	}

	return nil
}

// MilestoneNotification: 마일스톤 달성 알림 정보 (youtube_milestones 테이블 기반)
type MilestoneNotification struct {
	ChannelID  string    `json:"channelId"`
	MemberName string    `json:"memberName"`
	Type       string    `json:"type"`
	Value      uint64    `json:"value"`
	AchievedAt time.Time `json:"achievedAt"`
}

// GetUnnotifiedMilestones: 아직 알림이 발송되지 않은 마일스톤 목록을 조회합니다.
func (r *StatsRepository) GetUnnotifiedMilestones(ctx context.Context, limit int) ([]MilestoneNotification, error) {
	query := `
		SELECT channel_id, member_name, type, value, achieved_at
		FROM youtube_milestones
		WHERE notified = false
		ORDER BY achieved_at DESC
		LIMIT $1
	`

	rows, err := r.pool.Query(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query unnotified milestones: %w", err)
	}
	defer rows.Close()

	var notifications []MilestoneNotification
	for rows.Next() {
		var n MilestoneNotification
		if err := rows.Scan(&n.ChannelID, &n.MemberName, &n.Type, &n.Value, &n.AchievedAt); err != nil {
			r.logger.Warn("Failed to scan milestone notification", slog.Any("error", err))
			continue
		}
		notifications = append(notifications, n)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration error: %w", err)
	}

	return notifications, nil
}

// MarkMilestoneNotified: 마일스톤 알림 발송 완료 표시
func (r *StatsRepository) MarkMilestoneNotified(ctx context.Context, channelID string, milestoneType string, value uint64) error {
	query := `
		UPDATE youtube_milestones
		SET notified = true
		WHERE channel_id = $1 AND type = $2 AND value = $3
	`

	_, err := r.pool.Exec(ctx, query, channelID, milestoneType, value)
	if err != nil {
		return fmt.Errorf("failed to mark milestone notified: %w", err)
	}

	return nil
}

// SubscriberGraphPoint: 구독자 그래프 데이터 포인트 (일별 다운샘플링)
type SubscriberGraphPoint struct {
	Date        time.Time `json:"date"`
	Subscribers int64     `json:"subscribers"`
}

// SubscriberGraphData: 구독자 그래프 전체 데이터
type SubscriberGraphData struct {
	ChannelID   string                 `json:"channelId"`
	MemberName  string                 `json:"memberName"`
	Current     int64                  `json:"current"`
	Change7d    int64                  `json:"change7d"`
	Change30d   int64                  `json:"change30d"`
	Points      []SubscriberGraphPoint `json:"points"`
	UpdatedAt   time.Time              `json:"updatedAt"`
	SampleCount int                    `json:"sampleCount"`
}

// GetSubscriberGraph: 채널의 구독자 추이를 일별 다운샘플링하여 조회 (7/30/90일)
func (r *StatsRepository) GetSubscriberGraph(ctx context.Context, channelID string, days int) (*SubscriberGraphData, error) {
	if days <= 0 {
		days = 30
	}
	if days > 90 {
		days = 90
	}

	query := `
		WITH daily_stats AS (
			SELECT 
				DATE(time) as date,
				MAX(subscribers) as subscribers,
				MAX(member_name) as member_name
			FROM youtube_stats_history
			WHERE channel_id = $1 AND time >= NOW() - ($2 || ' days')::interval
			GROUP BY DATE(time)
			ORDER BY date ASC
		),
		current_stats AS (
			SELECT subscribers, member_name
			FROM youtube_stats_history
			WHERE channel_id = $1
			ORDER BY time DESC
			LIMIT 1
		),
		stats_7d AS (
			SELECT subscribers
			FROM youtube_stats_history
			WHERE channel_id = $1 AND time >= NOW() - INTERVAL '7 days'
			ORDER BY time ASC
			LIMIT 1
		),
		stats_30d AS (
			SELECT subscribers
			FROM youtube_stats_history
			WHERE channel_id = $1 AND time >= NOW() - INTERVAL '30 days'
			ORDER BY time ASC
			LIMIT 1
		)
		SELECT 
			COALESCE(c.member_name, '') as member_name,
			COALESCE(c.subscribers, 0) as current_subs,
			COALESCE(c.subscribers - s7.subscribers, 0) as change_7d,
			COALESCE(c.subscribers - s30.subscribers, 0) as change_30d,
			COALESCE(
				(SELECT json_agg(json_build_object('date', date, 'subscribers', subscribers) ORDER BY date)
				 FROM daily_stats),
				'[]'::json
			) as points,
			(SELECT COUNT(*) FROM daily_stats) as sample_count
		FROM current_stats c
		LEFT JOIN stats_7d s7 ON true
		LEFT JOIN stats_30d s30 ON true
	`

	var memberName string
	var currentSubs, change7d, change30d int64
	var pointsJSON []byte
	var sampleCount int

	err := r.pool.QueryRow(ctx, query, channelID, days).Scan(
		&memberName,
		&currentSubs,
		&change7d,
		&change30d,
		&pointsJSON,
		&sampleCount,
	)

	if errors.Is(err, pgx.ErrNoRows) || currentSubs == 0 {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get subscriber graph: %w", err)
	}

	var points []SubscriberGraphPoint
	if err := parseJSONArray(pointsJSON, &points); err != nil {
		r.logger.Warn("Failed to parse graph points", slog.Any("error", err))
		points = nil
	}

	return &SubscriberGraphData{
		ChannelID:   channelID,
		MemberName:  memberName,
		Current:     currentSubs,
		Change7d:    change7d,
		Change30d:   change30d,
		Points:      points,
		UpdatedAt:   time.Now(),
		SampleCount: sampleCount,
	}, nil
}

func parseJSONArray(data []byte, dest any) error {
	if len(data) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, dest); err != nil {
		return fmt.Errorf("unmarshal json array: %w", err)
	}
	return nil
}
