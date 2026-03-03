package stats

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/json"
)

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
