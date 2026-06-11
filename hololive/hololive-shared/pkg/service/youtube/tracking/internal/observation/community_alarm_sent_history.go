package observation

import (
	"context"
	"fmt"
	"time"

	"github.com/kapu/hololive-shared/internal/dbx"
	"github.com/kapu/hololive-shared/pkg/domain"
)

type ObservationAlarmSentHistoryRow struct {
	PostID            string     `db:"post_id"`
	ContentID         string     `db:"content_id"`
	ChannelID         string     `db:"channel_id"`
	ActualPublishedAt *time.Time `db:"actual_published_at"`
	DetectedAt        time.Time  `db:"detected_at"`
	AlarmSentAt       time.Time  `db:"alarm_sent_at"`
}

type CommunityAlarmSentHistoryRow = ObservationAlarmSentHistoryRow

type ShortsAlarmSentHistoryRow = ObservationAlarmSentHistoryRow

func (r *historyRepository) ListCommunityAlarmSentHistoriesByFinalizedObservationWindow(
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

func (r *historyRepository) ListShortsAlarmSentHistoriesByFinalizedObservationWindow(
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

func (r *historyRepository) ListCommunityAlarmSentHistoriesWithinObservationWindow(
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

func (r *historyRepository) ListShortsAlarmSentHistoriesWithinObservationWindow(
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

func (r *historyRepository) listAlarmSentHistoriesWithinObservationWindow(
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
	if err := dbx.SelectSQL(ctx, r.db, &rows, "query rows", `
		SELECT track.canonical_content_id AS post_id,
		       track.content_id AS content_id,
		       track.channel_id AS channel_id,
		       track.actual_published_at AS actual_published_at,
		       track.detected_at AS detected_at,
		       track.alarm_sent_at AS alarm_sent_at
		FROM youtube_content_alarm_tracking AS track
		WHERE track.kind = ?
		  AND track.delivery_status = ?
		  AND track.alarm_sent_at IS NOT NULL
		  AND COALESCE(track.actual_published_at, track.detected_at) >= ?
		  AND COALESCE(track.actual_published_at, track.detected_at) < ?
		  AND track.detected_at < ?
		ORDER BY track.alarm_sent_at ASC, track.canonical_content_id ASC
	`, kind, domain.YouTubeContentAlarmDeliveryStatusSent, startUTC, endUTC, detectedBeforeUTC); err != nil {
		return nil, err
	}

	return rows, nil
}

func (r *historyRepository) listAlarmSentHistoriesByFinalizedObservationWindow(
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
	if err := dbx.SelectSQL(ctx, r.db, &rows, "query rows", `
		SELECT base.post_id AS post_id,
		       track.content_id AS content_id,
		       track.channel_id AS channel_id,
		       track.actual_published_at AS actual_published_at,
		       track.detected_at AS detected_at,
		       track.alarm_sent_at AS alarm_sent_at
		FROM youtube_community_shorts_observation_post_baselines AS base
		INNER JOIN youtube_content_alarm_tracking track ON track.kind = base.kind AND track.canonical_content_id = base.post_id
		WHERE base.runtime_name = ? AND base.bigbang_cutover_at = ?
		  AND base.kind = ?
		  AND track.delivery_status = ?
		  AND track.alarm_sent_at IS NOT NULL
		ORDER BY track.alarm_sent_at ASC, base.post_id ASC
	`, normalizedRuntimeName, normalizedCutoverAt, kind, domain.YouTubeContentAlarmDeliveryStatusSent); err != nil {
		return nil, err
	}

	return rows, nil
}
