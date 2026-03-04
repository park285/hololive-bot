package checker

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/alarm/dedup"
	"github.com/kapu/hololive-shared/pkg/service/alarm/queue"
	"github.com/kapu/hololive-shared/pkg/service/alarm/tier"

	"github.com/kapu/hololive-kakao-bot-go/internal/service/notification"
)

// Notifier는 dedup claim + 큐 발행 + 발송 마킹을 담당한다.
type Notifier struct {
	dedupSvc       *dedup.Service
	queuePublisher *queue.Publisher
	alarmSvc       *notification.AlarmService
	tierScheduler  *tier.TieredScheduler
	logger         *slog.Logger
}

// NewNotifier는 알림 발행기를 생성한다.
func NewNotifier(
	dedupSvc *dedup.Service,
	queuePublisher *queue.Publisher,
	alarmSvc *notification.AlarmService,
	tierScheduler *tier.TieredScheduler,
	logger *slog.Logger,
) (*Notifier, error) {
	if dedupSvc == nil {
		return nil, fmt.Errorf("new notifier: dedup service is nil")
	}
	if queuePublisher == nil {
		return nil, fmt.Errorf("new notifier: queue publisher is nil")
	}
	if alarmSvc == nil {
		return nil, fmt.Errorf("new notifier: alarm service is nil")
	}

	return &Notifier{
		dedupSvc:       dedupSvc,
		queuePublisher: queuePublisher,
		alarmSvc:       alarmSvc,
		tierScheduler:  tierScheduler,
		logger:         safeLogger(logger),
	}, nil
}

// Send는 알림 목록을 순차 처리한다.
func (n *Notifier) Send(ctx context.Context, notifications []*domain.AlarmNotification) (SendResult, error) {
	result := SendResult{}
	for _, notification := range notifications {
		singleResult, err := n.sendOne(ctx, notification)
		if err != nil {
			return result, fmt.Errorf("send notifications: send one: %w", err)
		}

		switch singleResult {
		case sendOutcomeSent:
			result.Sent++
		case sendOutcomeSkipped:
			result.Skipped++
		case sendOutcomeFailed:
			result.Failed++
		}
	}
	return result, nil
}

type sendOutcome int

const (
	sendOutcomeSent sendOutcome = iota + 1
	sendOutcomeSkipped
	sendOutcomeFailed
)

type sendInput struct {
	notification   *domain.AlarmNotification
	streamID       string
	channelID      string
	startScheduled time.Time
}

func (n *Notifier) sendOne(ctx context.Context, notif *domain.AlarmNotification) (sendOutcome, error) {
	payload := resolveSendInput(notif, time.Now().UTC())
	if payload == nil {
		return sendOutcomeSkipped, nil
	}

	claimKeys, claimed, err := n.claimDedup(ctx, payload)
	if err != nil {
		return sendOutcomeFailed, fmt.Errorf("send one: claim dedup: %w", err)
	}
	if !claimed {
		return sendOutcomeSkipped, nil
	}

	if err := n.publishAndMark(ctx, payload, claimKeys); err != nil {
		return sendOutcomeFailed, fmt.Errorf("send one: publish and mark: %w", err)
	}

	if n.tierScheduler != nil {
		n.tierScheduler.MarkChannelRecentlyNotified(payload.channelID)
	}

	return sendOutcomeSent, nil
}

func resolveSendInput(notif *domain.AlarmNotification, now time.Time) *sendInput {
	if notif == nil || notif.Stream == nil {
		return nil
	}

	resolvedStream := ensureScheduledTime(notif.Stream, now)
	if resolvedStream == nil || resolvedStream.StartScheduled == nil {
		return nil
	}

	startScheduled := resolvedStream.StartScheduled.UTC()
	if startScheduled.IsZero() {
		return nil
	}

	streamID := strings.TrimSpace(resolvedStream.ID)
	if streamID == "" {
		return nil
	}

	channelID := strings.TrimSpace(resolvedStream.ChannelID)
	if channelID == "" && resolvedStream.Channel != nil {
		channelID = strings.TrimSpace(resolvedStream.Channel.ID)
	}
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

func (n *Notifier) claimDedup(ctx context.Context, payload *sendInput) ([]string, bool, error) {
	notifyClaimKey, notifyClaimed, err := n.dedupSvc.TryClaimNotification(
		ctx,
		payload.notification.RoomID,
		payload.streamID,
		payload.startScheduled,
		payload.notification.MinutesUntil,
	)
	if err != nil {
		return nil, false, fmt.Errorf("claim notification: %w", err)
	}
	if !notifyClaimed {
		return nil, false, nil
	}

	logicalClaimKey, logicalClaimed, err := n.dedupSvc.TryClaimLogicalEvent(
		ctx,
		payload.notification.RoomID,
		payload.channelID,
		payload.notification.Stream,
		payload.notification.MinutesUntil,
	)
	if err != nil {
		n.releaseClaimsBestEffort(ctx, []string{notifyClaimKey}, "failed to release notification claim after logical claim error")
		return nil, false, fmt.Errorf("claim logical event: %w", err)
	}
	if !logicalClaimed {
		n.releaseClaimsBestEffort(ctx, []string{notifyClaimKey}, "failed to release notification claim after logical dedup skip")
		return nil, false, nil
	}

	return compactClaimKeys(notifyClaimKey, logicalClaimKey), true, nil
}

func compactClaimKeys(keys ...string) []string {
	if len(keys) == 0 {
		return nil
	}

	compacted := make([]string, 0, len(keys))
	for _, key := range keys {
		if strings.TrimSpace(key) == "" {
			continue
		}
		compacted = append(compacted, key)
	}
	return compacted
}

func (n *Notifier) publishAndMark(ctx context.Context, payload *sendInput, claimKeys []string) error {
	if err := n.queuePublisher.Publish(ctx, payload.notification, claimKeys); err != nil {
		n.releaseClaimsBestEffort(ctx, claimKeys, "failed to release claims after queue publish error")
		return fmt.Errorf("publish queue: %w", err)
	}

	if err := n.alarmSvc.MarkAsNotified(
		ctx,
		payload.streamID,
		payload.startScheduled,
		payload.notification.MinutesUntil,
	); err != nil {
		return fmt.Errorf("mark as notified: %w", err)
	}

	if err := n.alarmSvc.MarkUpcomingEventNotified(
		ctx,
		payload.notification.RoomID,
		payload.channelID,
		payload.notification.Stream,
	); err != nil {
		return fmt.Errorf("mark upcoming event notified: %w", err)
	}

	return nil
}

func (n *Notifier) releaseClaimsBestEffort(ctx context.Context, claimKeys []string, message string) {
	if len(claimKeys) == 0 {
		return
	}
	if err := n.dedupSvc.ReleaseClaims(ctx, claimKeys); err != nil {
		n.logger.Warn(message, slog.Any("error", err))
	}
}
