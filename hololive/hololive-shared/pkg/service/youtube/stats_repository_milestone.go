package youtube

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

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

func buildMilestoneWhereClause(filter MilestoneFilter) (string, []any, int) {
	var whereClauses []string
	args := make([]any, 0, 2)
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

	return whereSQL, args, argIdx
}

// GetAllMilestones: 달성된 마일스톤 목록을 조회한다 (페이지네이션/필터링 지원)
func (r *StatsRepository) GetAllMilestones(ctx context.Context, filter MilestoneFilter) (*MilestoneResult, error) {
	whereSQL, args, argIdx := buildMilestoneWhereClause(filter)

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
