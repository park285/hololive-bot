package notifier

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/checker/internal/checking"
	"github.com/kapu/hololive-shared/pkg/domain"
)

const (
	legacyCommunityShortsRouteAuditLogMessage = "YouTube community/shorts legacy route audit"
	legacyCommunityShortsDeliveryPath         = "legacy_alarm_queue"
)

func (n *Notifier) prepareOne(ctx context.Context, notif *domain.AlarmNotification) (*sendInput, []string, sendOutcome, error) {
	payload := resolveSendInput(notif, time.Now().UTC())
	if payload == nil {
		return nil, nil, sendOutcomeSkipped, nil
	}
	if err := payload.notification.ValidateLegacyRoute(); err != nil {
		n.logLegacyCommunityShortsRoute(payload, err)
		return nil, nil, sendOutcomeFailed, fmt.Errorf("send one: validate legacy route: %w", err)
	}

	claimKeys, claimed, err := n.claimDedup(ctx, payload)
	if err != nil {
		return nil, nil, sendOutcomeFailed, fmt.Errorf("send one: claim dedup: %w", err)
	}

	if !claimed {
		return nil, nil, sendOutcomeSkipped, nil
	}

	return payload, claimKeys, sendOutcomeSent, nil
}

func (n *Notifier) logLegacyCommunityShortsRoute(payload *sendInput, routeErr error) {
	if n == nil || payload == nil || payload.notification == nil {
		return
	}

	attrs := []any{
		slog.String("delivery_path", legacyCommunityShortsDeliveryPath),
		slog.String("send_result", "blocked"),
		slog.String("failure_reason", "legacy route blocked"),
		slog.String("alarm_type", string(payload.notification.AlarmType)),
	}
	if payload.notification.RoomID != "" {
		attrs = append(attrs, slog.String("room_id", payload.notification.RoomID))
	}
	if payload.channelID != "" {
		attrs = append(attrs, slog.String("channel_id", payload.channelID))
	}
	if payload.streamID != "" {
		attrs = append(attrs, slog.String("stream_id", payload.streamID))
	}
	if routeErr != nil {
		attrs = append(attrs, slog.String("error", routeErr.Error()))
	}

	n.logger.Warn(legacyCommunityShortsRouteAuditLogMessage, attrs...)
}

func resolveSendInput(notif *domain.AlarmNotification, now time.Time) *sendInput {
	resolvedStream, startScheduled, ok := resolveScheduledStream(notif, now)
	if !ok {
		return nil
	}

	streamID := resolveStreamID(resolvedStream)
	if streamID == "" {
		return nil
	}

	channelID := resolveChannelID(resolvedStream)
	if channelID == "" {
		return nil
	}

	resolvedNotification := *notif
	resolvedNotification.Stream = resolvedStream

	return &sendInput{
		notification:   &resolvedNotification,
		streamID:       streamID,
		channelID:      channelID,
		startScheduled: startScheduled,
	}
}

func resolveScheduledStream(notif *domain.AlarmNotification, now time.Time) (*domain.Stream, time.Time, bool) {
	if notif == nil || notif.Stream == nil {
		return nil, time.Time{}, false
	}

	resolvedStream := checking.EnsureScheduledTime(notif.Stream, now)
	if resolvedStream == nil || resolvedStream.StartScheduled == nil {
		return nil, time.Time{}, false
	}

	startScheduled := resolvedStream.StartScheduled.UTC()
	if startScheduled.IsZero() {
		return nil, time.Time{}, false
	}

	return resolvedStream, startScheduled, true
}

func resolveStreamID(stream *domain.Stream) string {
	if stream == nil {
		return ""
	}

	return strings.TrimSpace(stream.ID)
}

func resolveChannelID(stream *domain.Stream) string {
	if stream == nil {
		return ""
	}

	channelID := strings.TrimSpace(stream.ChannelID)
	if channelID != "" || stream.Channel == nil {
		return channelID
	}

	return strings.TrimSpace(stream.Channel.ID)
}
