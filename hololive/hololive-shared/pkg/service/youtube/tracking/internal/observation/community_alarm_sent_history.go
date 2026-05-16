package observation

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

type ObservationAlarmSentHistoryRow struct {
	PostID            string     `gorm:"column:post_id"`
	ContentID         string     `gorm:"column:content_id"`
	ChannelID         string     `gorm:"column:channel_id"`
	ActualPublishedAt *time.Time `gorm:"column:actual_published_at"`
	DetectedAt        time.Time  `gorm:"column:detected_at"`
	AlarmSentAt       time.Time  `gorm:"column:alarm_sent_at"`
}

type CommunityAlarmSentHistoryRow = ObservationAlarmSentHistoryRow

type ShortsAlarmSentHistoryRow = ObservationAlarmSentHistoryRow

func (r *GormRepository) ListCommunityAlarmSentHistoriesByFinalizedObservationWindow(
	ctx context.Context,
	runtimeName string,
	bigBangCutoverAt time.Time,
) ([]CommunityAlarmSentHistoryRow, error) {
	rows, err := r.listAlarmSentHistoriesByFinalizedObservationWindow(
		ctx,
		runtimeName,
		bigBangCutoverAt,
		domain.OutboxKindCommunityPost,
	)
	if err != nil {
		return nil, fmt.Errorf("list community alarm sent histories by finalized observation window: %w", err)
	}

	return rows, nil
}

func (r *GormRepository) ListShortsAlarmSentHistoriesByFinalizedObservationWindow(
	ctx context.Context,
	runtimeName string,
	bigBangCutoverAt time.Time,
) ([]ShortsAlarmSentHistoryRow, error) {
	rows, err := r.listAlarmSentHistoriesByFinalizedObservationWindow(
		ctx,
		runtimeName,
		bigBangCutoverAt,
		domain.OutboxKindNewShort,
	)
	if err != nil {
		return nil, fmt.Errorf("list shorts alarm sent histories by finalized observation window: %w", err)
	}

	return rows, nil
}

func (r *GormRepository) ListCommunityAlarmSentHistoriesWithinObservationWindow(
	ctx context.Context,
	windowStart time.Time,
	windowEnd time.Time,
	detectedBefore time.Time,
) ([]CommunityAlarmSentHistoryRow, error) {
	rows, err := r.listAlarmSentHistoriesWithinObservationWindow(
		ctx,
		windowStart,
		windowEnd,
		detectedBefore,
		domain.OutboxKindCommunityPost,
	)
	if err != nil {
		return nil, fmt.Errorf("list community alarm sent histories within observation window: %w", err)
	}

	return rows, nil
}

func (r *GormRepository) ListShortsAlarmSentHistoriesWithinObservationWindow(
	ctx context.Context,
	windowStart time.Time,
	windowEnd time.Time,
	detectedBefore time.Time,
) ([]ShortsAlarmSentHistoryRow, error) {
	rows, err := r.listAlarmSentHistoriesWithinObservationWindow(
		ctx,
		windowStart,
		windowEnd,
		detectedBefore,
		domain.OutboxKindNewShort,
	)
	if err != nil {
		return nil, fmt.Errorf("list shorts alarm sent histories within observation window: %w", err)
	}

	return rows, nil
}

func (r *GormRepository) listAlarmSentHistoriesWithinObservationWindow(
	ctx context.Context,
	windowStart time.Time,
	windowEnd time.Time,
	detectedBefore time.Time,
	kind domain.OutboxKind,
) ([]ObservationAlarmSentHistoryRow, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("db is nil")
	}
	if windowStart.IsZero() {
		return nil, fmt.Errorf("window start is empty")
	}
	if windowEnd.IsZero() {
		return nil, fmt.Errorf("window end is empty")
	}
	if detectedBefore.IsZero() {
		return nil, fmt.Errorf("detected before is empty")
	}

	startUTC := windowStart.UTC()
	endUTC := windowEnd.UTC()
	detectedBeforeUTC := detectedBefore.UTC()
	if !startUTC.Before(endUTC) {
		return nil, fmt.Errorf("window start must be before window end")
	}
	if detectedBeforeUTC.Before(endUTC) {
		return nil, fmt.Errorf("detected before must be on or after window end")
	}

	var rows []ObservationAlarmSentHistoryRow
	if err := r.db.WithContext(ctx).
		Table("youtube_content_alarm_tracking AS track").
		Select(strings.Join([]string{
			"track.canonical_content_id AS post_id",
			"track.content_id AS content_id",
			"track.channel_id AS channel_id",
			"track.actual_published_at AS actual_published_at",
			"track.detected_at AS detected_at",
			"track.alarm_sent_at AS alarm_sent_at",
		}, ", ")).
		Where("track.kind = ?", kind).
		Where("track.delivery_status = ?", domain.YouTubeContentAlarmDeliveryStatusSent).
		Where("track.alarm_sent_at IS NOT NULL").
		Where("COALESCE(track.actual_published_at, track.detected_at) >= ?", startUTC).
		Where("COALESCE(track.actual_published_at, track.detected_at) < ?", endUTC).
		Where("track.detected_at < ?", detectedBeforeUTC).
		Order("track.alarm_sent_at ASC").
		Order("track.canonical_content_id ASC").
		Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("query rows: %w", err)
	}

	return rows, nil
}

func (r *GormRepository) listAlarmSentHistoriesByFinalizedObservationWindow(
	ctx context.Context,
	runtimeName string,
	bigBangCutoverAt time.Time,
	kind domain.OutboxKind,
) ([]ObservationAlarmSentHistoryRow, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("db is nil")
	}

	normalizedRuntimeName, normalizedCutoverAt, err := normalizeCommunityShortsObservationWindowKey(runtimeName, bigBangCutoverAt)
	if err != nil {
		return nil, err
	}

	var rows []ObservationAlarmSentHistoryRow
	if err := r.db.WithContext(ctx).
		Table("youtube_community_shorts_observation_post_baselines AS base").
		Select(strings.Join([]string{
			"base.post_id AS post_id",
			"track.content_id AS content_id",
			"track.channel_id AS channel_id",
			"track.actual_published_at AS actual_published_at",
			"track.detected_at AS detected_at",
			"track.alarm_sent_at AS alarm_sent_at",
		}, ", ")).
		Joins("INNER JOIN youtube_content_alarm_tracking track ON track.kind = base.kind AND track.canonical_content_id = base.post_id").
		Where("base.runtime_name = ? AND base.bigbang_cutover_at = ?", normalizedRuntimeName, normalizedCutoverAt).
		Where("base.kind = ?", kind).
		Where("track.delivery_status = ?", domain.YouTubeContentAlarmDeliveryStatusSent).
		Where("track.alarm_sent_at IS NOT NULL").
		Order("track.alarm_sent_at ASC").
		Order("base.post_id ASC").
		Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("query rows: %w", err)
	}

	return rows, nil
}
