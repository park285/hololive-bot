package stats

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
)

type NearMilestoneEntry struct {
	ChannelID     string  `json:"channelId"`
	MemberName    string  `json:"memberName"`
	CurrentSubs   uint64  `json:"currentSubs"`
	NextMilestone uint64  `json:"nextMilestone"`
	Remaining     int64   `json:"remaining"`
	ProgressPct   float64 `json:"progressPct"`
}

const latestActiveStatsMilestoneCTE = `
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
		)
`

func (r *StatsRepository) GetNearMilestoneMembers(ctx context.Context, thresholdPct float64, milestones []uint64, limit int) ([]NearMilestoneEntry, error) {
	if len(milestones) == 0 {
		return nil, nil
	}

	query := latestActiveStatsMilestoneCTE + `
		,
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

	return scanNearMilestoneEntries(rows, r.logger, "Failed to scan near milestone entry")
}

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

func scanNearMilestoneEntries(rows pgx.Rows, logger *slog.Logger, warnMessage string) ([]NearMilestoneEntry, error) {
	var entries []NearMilestoneEntry
	for rows.Next() {
		var e NearMilestoneEntry
		if err := rows.Scan(&e.ChannelID, &e.MemberName, &e.CurrentSubs, &e.NextMilestone, &e.Remaining, &e.ProgressPct); err != nil {
			if logger != nil {
				logger.Warn(warnMessage, slog.Any("error", err))
			}
			continue
		}
		entries = append(entries, e)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration error: %w", err)
	}

	return entries, nil
}
