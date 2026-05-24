package stats

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

type MilestoneEntry struct {
	ChannelID  string    `json:"channelId"`
	MemberName string    `json:"memberName"`
	Type       string    `json:"type"`
	Value      uint64    `json:"value"`
	AchievedAt time.Time `json:"achievedAt"`
	Notified   bool      `json:"notified"`
}

type MilestoneFilter struct {
	Limit      int
	Offset     int
	ChannelID  string
	MemberName string
}

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
