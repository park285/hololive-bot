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

func (d *Dispatcher) releaseDeliveryClaims(ctx context.Context, claims []deliveryClaimToken) error {
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

func (d *Dispatcher) deliveryClaimTimeout() time.Duration {
	claimTimeout := maxCommunityShortsClaimHold
	if d != nil && d.cfg.LockTimeout > 0 && d.cfg.LockTimeout < claimTimeout {
		claimTimeout = d.cfg.LockTimeout
	}
	if claimTimeout <= 0 {
		return maxCommunityShortsClaimHold
	}
	return claimTimeout
}

func (d *Dispatcher) logClaimIssue(
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
