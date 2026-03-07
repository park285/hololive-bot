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
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

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

// MarkMilestonesNotifiedBatch: 여러 마일스톤의 알림 발송 완료를 한 번에 처리합니다.
func (r *StatsRepository) MarkMilestonesNotifiedBatch(ctx context.Context, milestones []MilestoneNotification) error {
	if len(milestones) == 0 {
		return nil
	}

	// (channel_id, type, value) 튜플 기반 IN 절 구성
	const colsPerRow = 3
	var sb strings.Builder
	sb.WriteString(`UPDATE youtube_milestones SET notified = true WHERE (channel_id, type, value) IN (`)
	args := make([]any, 0, len(milestones)*colsPerRow)
	for i, m := range milestones {
		if i > 0 {
			sb.WriteByte(',')
		}
		base := i * colsPerRow
		sb.WriteString("($")
		sb.WriteString(strconv.Itoa(base + 1))
		sb.WriteString(",$")
		sb.WriteString(strconv.Itoa(base + 2))
		sb.WriteString(",$")
		sb.WriteString(strconv.Itoa(base + 3))
		sb.WriteByte(')')
		args = append(args, m.ChannelID, m.Type, m.Value)
	}
	sb.WriteByte(')')

	_, err := r.pool.Exec(ctx, sb.String(), args...)
	if err != nil {
		return fmt.Errorf("failed to batch mark milestones notified (%d rows): %w", len(milestones), err)
	}
	return nil
}

// MarkApproachingChatNotifiedBatch: 여러 예고 알림의 채팅방 발송 완료를 한 번에 처리합니다.
func (r *StatsRepository) MarkApproachingChatNotifiedBatch(ctx context.Context, notifications []ApproachingNotification) error {
	if len(notifications) == 0 {
		return nil
	}

	// (channel_id, milestone_value) 튜플 기반 IN 절 구성
	const colsPerRow = 2
	var sb strings.Builder
	sb.WriteString(`UPDATE youtube_milestone_approaching SET chat_notified = true WHERE (channel_id, milestone_value) IN (`)
	args := make([]any, 0, len(notifications)*colsPerRow)
	for i, n := range notifications {
		if i > 0 {
			sb.WriteByte(',')
		}
		base := i * colsPerRow
		sb.WriteString("($")
		sb.WriteString(strconv.Itoa(base + 1))
		sb.WriteString(",$")
		sb.WriteString(strconv.Itoa(base + 2))
		sb.WriteByte(')')
		args = append(args, n.ChannelID, n.MilestoneValue)
	}
	sb.WriteByte(')')

	_, err := r.pool.Exec(ctx, sb.String(), args...)
	if err != nil {
		return fmt.Errorf("failed to batch mark approaching notified (%d rows): %w", len(notifications), err)
	}
	return nil
}
