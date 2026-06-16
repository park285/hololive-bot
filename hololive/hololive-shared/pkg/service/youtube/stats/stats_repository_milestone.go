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

	"github.com/kapu/hololive-shared/pkg/domain"
)

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

type MilestoneStats struct {
	TotalAchieved      int `json:"totalAchieved"`
	TotalNearMilestone int `json:"totalNearMilestone"`
	RecentAchievements int `json:"recentAchievements"` // 최근 30일
	NotNotifiedCount   int `json:"notNotifiedCount"`
}

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
