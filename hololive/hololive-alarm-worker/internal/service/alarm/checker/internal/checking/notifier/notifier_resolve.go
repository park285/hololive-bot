package notifier

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/checker/internal/checking"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func (n *Notifier) prepareOne(ctx context.Context, notif *domain.AlarmNotification) (*sendInput, []string, sendOutcome, error) {
	payload := resolveSendInput(notif, time.Now().UTC())
	if payload == nil {
		return nil, nil, sendOutcomeSkipped, nil
	}
	if err := payload.notification.ValidateLiveDispatchRoute(); err != nil {
		return nil, nil, sendOutcomeFailed, fmt.Errorf("send one: validate live dispatch route: %w", err)
	}
	if err := payload.notification.ValidateLiveDispatchPersistenceIdentity(); err != nil {
		return nil, nil, sendOutcomeFailed, fmt.Errorf("send one: validate live dispatch persistence identity: %w", err)
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
