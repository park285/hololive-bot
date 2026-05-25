package delivery

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/logschema"
	trackingrepo "github.com/kapu/hololive-shared/pkg/service/youtube/tracking"
)

func (d *ClaimManager) releaseOutboxLock(ctx context.Context, id int64, lockedAt *time.Time) {
	query := d.db.WithContext(ctx).Model(&domain.YouTubeNotificationOutbox{}).
		Where("id = ? AND status = ?", id, domain.OutboxStatusPending)
	if lockedAt != nil {
		query = query.Where("locked_at = ?", *lockedAt)
	}
	result := query.Update("locked_at", nil)
	if result.Error != nil {
		d.logger.Warn("Failed to release outbox lock",
			slog.Int64("id", id),
			slog.Any("error", result.Error))
	}
}

func (d *ClaimManager) cleanupOutbox(ctx context.Context) {
	if d == nil || d.db == nil {
		return
	}

	outboxCutoff := time.Now().UTC().Add(-d.config.CleanupAfter)
	result := d.db.WithContext(ctx).
		Where("status IN (?, ?) AND COALESCE(sent_at, created_at) < ?", domain.OutboxStatusSent, domain.OutboxStatusFailed, outboxCutoff).
		Delete(&domain.YouTubeNotificationOutbox{})

	if result.Error != nil {
		d.logger.Warn("Failed to cleanup old outbox items", slog.Any("error", result.Error))
		return
	}

	if result.RowsAffected > 0 {
		d.logger.Info("Cleaned up old outbox items", slog.Int64("deleted", result.RowsAffected))
	}
}

func (d *ClaimManager) releaseDeliveryClaims(ctx context.Context, claims []deliveryClaimToken) error {
	if d == nil || d.db == nil || len(claims) == 0 {
		return nil
	}

	repository := trackingrepo.NewRepository(d.db)
	for i := range claims {
		if _, err := repository.ReleaseAlarmStateClaim(ctx, claims[i].kind, claims[i].postID, claims[i].authorizedAt); err != nil {
			return fmt.Errorf("release claim at index %d: %w", i, err)
		}
	}
	return nil
}

func (d *ClaimManager) deliveryClaimTimeout() time.Duration {
	claimTimeout := maxCommunityShortsClaimHold
	if d != nil && d.config.LockTimeout > 0 && d.config.LockTimeout < claimTimeout {
		claimTimeout = d.config.LockTimeout
	}
	if claimTimeout <= 0 {
		return maxCommunityShortsClaimHold
	}
	return claimTimeout
}

func (d *ClaimManager) logClaimIssue(
	message string,
	row domain.YouTubeNotificationDelivery,
	outbox domain.YouTubeNotificationOutbox,
	level slog.Level,
	attrs ...any,
) {
	if d == nil || d.logger == nil {
		return
	}

	baseAttrs := make([]any, 0, 7+len(attrs))
	baseAttrs = append(baseAttrs,
		slog.Int64(logschema.FieldDeliveryID, row.ID),
		slog.Int64(logschema.FieldOutboxID, outbox.ID),
		slog.String(logschema.FieldRoomID, row.RoomID),
		slog.String(logschema.FieldChannelID, outbox.ChannelID),
		slog.String(deliveryAuditPostIDLogField, resolveTelemetryPostID(outbox.Kind, outbox.ContentID, outbox.Payload)),
		slog.String(deliveryAuditContentIDLogField, strings.TrimSpace(outbox.ContentID)),
		slog.String(deliveryAuditAlarmTypeLogField, string(outbox.Kind.ToAlarmType())),
	)
	baseAttrs = append(baseAttrs, attrs...)

	switch level {
	case slog.LevelWarn:
		d.logger.Warn(message, baseAttrs...)
	case slog.LevelError:
		d.logger.Error(message, baseAttrs...)
	default:
		d.logger.Info(message, baseAttrs...)
	}
}
