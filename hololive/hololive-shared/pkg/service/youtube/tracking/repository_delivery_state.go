package tracking

import (
	"context"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/alarmtiming"
	yttimestamp "github.com/kapu/hololive-shared/pkg/service/youtube/timestamp"
)

func (r *GormRepository) MarkAlarmSentBatch(ctx context.Context, marks []AlarmSentMark) error {
	if len(marks) == 0 {
		return nil
	}
	if r == nil || r.db == nil {
		return fmt.Errorf("mark alarm sent batch: db is nil")
	}

	normalized, err := normalizeAlarmSentMarks(marks)
	if err != nil {
		return fmt.Errorf("mark alarm sent batch: %w", err)
	}

	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		txRepo := NewRepository(tx)
		updatedAt := yttimestamp.Normalize(time.Now())
		for i, mark := range normalized {
			if err := txRepo.applyAlarmSentMark(ctx, mark, updatedAt); err != nil {
				return fmt.Errorf("update mark at index %d: %w", i, err)
			}
		}

		return nil
	})
}

func (r *GormRepository) applyAlarmSentMark(ctx context.Context, mark AlarmSentMark, updatedAt time.Time) error {
	trackingRow, err := r.FindByIdentity(ctx, mark.Kind, mark.ContentID)
	if err != nil {
		return fmt.Errorf("load tracking row: %w", err)
	}
	latencyMillis, latencyExceeded := calculateLatencyResult(nil, &mark.AlarmSentAt)
	targetContentID := mark.ContentID
	if trackingRow != nil {
		targetContentID = trackingRow.ContentID
		latencyMillis, latencyExceeded = calculateLatencyResult(trackingRow.ActualPublishedAt, &mark.AlarmSentAt)
	}

	result := r.db.WithContext(ctx).
		Model(&domain.YouTubeContentAlarmTracking{}).
		Where("kind = ? AND content_id = ?", mark.Kind, targetContentID).
		Where("alarm_sent_at IS NULL OR alarm_sent_at > ?", mark.AlarmSentAt).
		Updates(map[string]any{
			"alarm_sent_at":          mark.AlarmSentAt,
			"alarm_latency_millis":   nullableInt64Value(latencyMillis),
			"alarm_latency_exceeded": nullableBoolValue(latencyExceeded),
			"delivery_status":        domain.YouTubeContentAlarmDeliveryStatusSent,
			"updated_at":             updatedAt,
		})
	if result.Error != nil {
		return result.Error
	}

	if !isCommunityShortsAlarmStateKind(mark.Kind) {
		return nil
	}

	postID := canonicalTrackingIdentity(mark.Kind, targetContentID)
	if trackingRow != nil && strings.TrimSpace(trackingRow.CanonicalContentID) != "" {
		postID = strings.TrimSpace(trackingRow.CanonicalContentID)
	}

	return r.applyAlarmStateSentMark(ctx, mark, postID, targetContentID, trackingRow, updatedAt)
}

func (r *GormRepository) applyAlarmStateSentMark(
	ctx context.Context,
	mark AlarmSentMark,
	postID string,
	targetContentID string,
	trackingRow *domain.YouTubeContentAlarmTracking,
	updatedAt time.Time,
) error {
	if mark.AuthorizedAt != nil {
		result := r.db.WithContext(ctx).
			Model(&domain.YouTubeCommunityShortsAlarmState{}).
			Where("kind = ? AND post_id = ?", mark.Kind, postID).
			Where("authorized_at = ?", *mark.AuthorizedAt).
			Where("alarm_sent_at IS NULL").
			Updates(map[string]any{
				"authorized_at":   nil,
				"alarm_sent_at":   mark.AlarmSentAt,
				"delivery_status": domain.YouTubeCommunityShortsAlarmStateStatusSent,
				"updated_at":      updatedAt,
			})
		if result.Error != nil {
			return fmt.Errorf("finalize claimed alarm state: update row: %w", result.Error)
		}
		if result.RowsAffected > 0 {
			return nil
		}
	}

	stateRow, err := r.FindAlarmStateByPostID(ctx, mark.Kind, postID)
	if err != nil {
		return fmt.Errorf("load alarm state row: %w", err)
	}

	if stateRow != nil {
		if stateRow.AlarmSentAt != nil && !stateRow.AlarmSentAt.IsZero() {
			if err := r.updateAlarmStateSentRow(ctx, mark.Kind, postID, mark.AlarmSentAt, updatedAt); err != nil {
				return fmt.Errorf("refresh existing sent alarm state row: %w", err)
			}
			return nil
		}
		if mark.AuthorizedAt != nil {
			return fmt.Errorf("finalize claimed alarm state: claim authorization mismatch")
		}
		if err := r.updateAlarmStateSentRow(ctx, mark.Kind, postID, mark.AlarmSentAt, updatedAt); err != nil {
			return fmt.Errorf("mark existing alarm state row sent: %w", err)
		}
		return nil
	}

	if trackingRow == nil {
		if mark.AuthorizedAt != nil {
			return fmt.Errorf("finalize claimed alarm state: tracking row missing")
		}
		return nil
	}

	alarmSentAt := mark.AlarmSentAt
	if err := r.UpsertAlarmState(ctx, &domain.YouTubeCommunityShortsAlarmState{
		Kind:              mark.Kind,
		PostID:            postID,
		ContentID:         targetContentID,
		ChannelID:         trackingRow.ChannelID,
		ActualPublishedAt: trackingRow.ActualPublishedAt,
		DetectedAt:        trackingRow.DetectedAt,
		AlarmSentAt:       &alarmSentAt,
	}); err != nil {
		return fmt.Errorf("upsert fallback alarm state row: %w", err)
	}

	return nil
}

func (r *GormRepository) updateAlarmStateSentRow(ctx context.Context, kind domain.OutboxKind, postID string, alarmSentAt time.Time, updatedAt time.Time) error {
	normalizedKind, normalizedPostID, err := normalizeSourcePostIdentity(kind, postID)
	if err != nil {
		return err
	}

	result := r.db.WithContext(ctx).
		Model(&domain.YouTubeCommunityShortsAlarmState{}).
		Where("kind = ? AND post_id = ?", normalizedKind, normalizedPostID).
		Where("alarm_sent_at IS NULL OR alarm_sent_at > ? OR authorized_at IS NOT NULL", alarmSentAt).
		Updates(map[string]any{
			"authorized_at": nil,
			"alarm_sent_at": gorm.Expr(
				"CASE WHEN alarm_sent_at IS NULL OR alarm_sent_at > ? THEN ? ELSE alarm_sent_at END",
				alarmSentAt,
				alarmSentAt,
			),
			"delivery_status": domain.YouTubeCommunityShortsAlarmStateStatusSent,
			"updated_at":      updatedAt,
		})
	if result.Error != nil {
		return result.Error
	}

	return nil
}

func normalizeAlarmSentMarks(marks []AlarmSentMark) ([]AlarmSentMark, error) {
	normalized := make([]AlarmSentMark, 0, len(marks))
	indexByIdentity := make(map[string]int, len(marks))

	for i, mark := range marks {
		normalizedKind, normalizedContentID, err := normalizeIdentity(mark.Kind, mark.ContentID)
		if err != nil {
			return nil, fmt.Errorf("normalize mark at index %d: %w", i, err)
		}
		if mark.AlarmSentAt.IsZero() {
			return nil, fmt.Errorf("normalize mark at index %d: alarm sent at is empty", i)
		}

		var normalizedAuthorizedAt *time.Time
		if mark.AuthorizedAt != nil {
			if mark.AuthorizedAt.IsZero() {
				return nil, fmt.Errorf("normalize mark at index %d: authorized at is empty", i)
			}
			authorizedAt := yttimestamp.Normalize(*mark.AuthorizedAt)
			normalizedAuthorizedAt = &authorizedAt
		}

		normalizedAlarmSentAt := yttimestamp.Normalize(mark.AlarmSentAt)
		identity := string(normalizedKind) + "\x00" + canonicalTrackingIdentity(normalizedKind, normalizedContentID)
		if existingIndex, ok := indexByIdentity[identity]; ok {
			if normalizedAuthorizedAt != nil {
				existingAuthorizedAt := normalized[existingIndex].AuthorizedAt
				switch {
				case existingAuthorizedAt == nil:
					normalized[existingIndex].AuthorizedAt = normalizedAuthorizedAt
				case !existingAuthorizedAt.UTC().Equal(normalizedAuthorizedAt.UTC()):
					return nil, fmt.Errorf("normalize mark at index %d: conflicting authorized_at for %s", i, identity)
				}
			}
			if normalizedAlarmSentAt.Before(normalized[existingIndex].AlarmSentAt) {
				normalized[existingIndex].AlarmSentAt = normalizedAlarmSentAt
			}
			continue
		}

		indexByIdentity[identity] = len(normalized)
		normalized = append(normalized, AlarmSentMark{
			Kind:         normalizedKind,
			ContentID:    normalizedContentID,
			AlarmSentAt:  normalizedAlarmSentAt,
			AuthorizedAt: normalizedAuthorizedAt,
		})
	}

	return normalized, nil
}

func isCommunityShortsAlarmStateKind(kind domain.OutboxKind) bool {
	switch kind {
	case domain.OutboxKindCommunityPost, domain.OutboxKindNewShort:
		return true
	default:
		return false
	}
}

func calculateLatencyResult(start *time.Time, end *time.Time) (*int64, *bool) {
	return alarmtiming.CalculateLatency(start, end)
}

func nullableInt64Value(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableBoolValue(value *bool) any {
	if value == nil {
		return nil
	}
	return *value
}
