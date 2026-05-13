package tracking

import (
	"context"
	"fmt"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	yttimestamp "github.com/kapu/hololive-shared/pkg/service/youtube/timestamp"
)

func (r *GormRepository) UpsertAlarmState(ctx context.Context, record *domain.YouTubeCommunityShortsAlarmState) error {
	if record == nil {
		return fmt.Errorf("upsert alarm state: record is nil")
	}
	return r.UpsertAlarmStateBatch(ctx, []*domain.YouTubeCommunityShortsAlarmState{record})
}

func (r *GormRepository) TryClaimAlarmState(ctx context.Context, record *domain.YouTubeCommunityShortsAlarmState) (bool, error) {
	if r == nil || r.db == nil {
		return false, fmt.Errorf("try claim alarm state: db is nil")
	}

	normalizedRecord, err := normalizeAlarmStateClaim(record)
	if err != nil {
		return false, fmt.Errorf("try claim alarm state: %w", err)
	}

	claimed, err := r.insertAlarmStateClaim(ctx, normalizedRecord)
	if err != nil || !claimed {
		return claimed, err
	}
	return r.confirmAlarmStateClaim(ctx, normalizedRecord)
}

func (r *GormRepository) insertAlarmStateClaim(
	ctx context.Context,
	normalizedRecord *domain.YouTubeCommunityShortsAlarmState,
) (bool, error) {
	now := yttimestamp.Normalize(time.Now())
	result := r.db.WithContext(ctx).Exec(`
        INSERT INTO youtube_community_shorts_alarm_states
            (kind, post_id, content_id, channel_id, actual_published_at, detected_at, authorized_at, alarm_sent_at, delivery_status, created_at, updated_at)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
        ON CONFLICT (kind, post_id) DO UPDATE
        SET content_id = EXCLUDED.content_id,
            channel_id = EXCLUDED.channel_id,
            actual_published_at = CASE
                WHEN EXCLUDED.actual_published_at IS NULL THEN youtube_community_shorts_alarm_states.actual_published_at
                ELSE EXCLUDED.actual_published_at
            END,
            detected_at = CASE
                WHEN EXCLUDED.detected_at < youtube_community_shorts_alarm_states.detected_at THEN EXCLUDED.detected_at
                ELSE youtube_community_shorts_alarm_states.detected_at
            END,
            authorized_at = EXCLUDED.authorized_at,
            delivery_status = EXCLUDED.delivery_status,
            updated_at = EXCLUDED.updated_at
        WHERE youtube_community_shorts_alarm_states.authorized_at IS NULL
          AND youtube_community_shorts_alarm_states.alarm_sent_at IS NULL
    `,
		normalizedRecord.Kind,
		normalizedRecord.PostID,
		normalizedRecord.ContentID,
		normalizedRecord.ChannelID,
		normalizedRecord.ActualPublishedAt,
		normalizedRecord.DetectedAt,
		normalizeDatabaseTimestamp(*normalizedRecord.AuthorizedAt),
		nil,
		normalizedRecord.DeliveryStatus,
		now,
		now,
	)
	if result.Error != nil {
		return false, fmt.Errorf("try claim alarm state: exec query: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return false, nil
	}
	return true, nil
}

func (r *GormRepository) confirmAlarmStateClaim(
	ctx context.Context,
	normalizedRecord *domain.YouTubeCommunityShortsAlarmState,
) (bool, error) {
	current, err := r.FindAlarmStateByPostID(ctx, normalizedRecord.Kind, normalizedRecord.PostID)
	if err != nil {
		return false, fmt.Errorf("try claim alarm state: reload row: %w", err)
	}
	if current == nil || current.AuthorizedAt == nil || current.AuthorizedAt.IsZero() {
		return false, nil
	}
	if current.AlarmSentAt != nil && !current.AlarmSentAt.IsZero() {
		return false, nil
	}

	return current.AuthorizedAt.UTC().Equal(normalizedRecord.AuthorizedAt.UTC()), nil
}

func (r *GormRepository) ReleaseAlarmStateClaim(ctx context.Context, kind domain.OutboxKind, postID string, authorizedAt time.Time) (bool, error) {
	if r == nil || r.db == nil {
		return false, fmt.Errorf("release alarm state claim: db is nil")
	}
	if authorizedAt.IsZero() {
		return false, fmt.Errorf("release alarm state claim: authorized_at is empty")
	}

	normalizedKind, normalizedPostID, err := normalizeSourcePostIdentity(kind, postID)
	if err != nil {
		return false, fmt.Errorf("release alarm state claim: %w", err)
	}

	updatedAt := yttimestamp.Normalize(time.Now())
	result := r.db.WithContext(ctx).
		Model(&domain.YouTubeCommunityShortsAlarmState{}).
		Where("kind = ? AND post_id = ?", normalizedKind, normalizedPostID).
		Where("alarm_sent_at IS NULL").
		Where("authorized_at = ?", normalizeDatabaseTimestamp(authorizedAt)).
		Updates(map[string]any{
			"authorized_at":   nil,
			"delivery_status": domain.YouTubeCommunityShortsAlarmStateStatusDetected,
			"updated_at":      updatedAt,
		})
	if result.Error != nil {
		return false, fmt.Errorf("release alarm state claim: update row: %w", result.Error)
	}

	return result.RowsAffected > 0, nil
}
