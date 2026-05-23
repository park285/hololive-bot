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

package notifier

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/checker/internal/checking"
	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/alarm/dedup"
	"github.com/kapu/hololive-shared/pkg/service/alarm/keys"
	"github.com/kapu/hololive-shared/pkg/service/alarm/queue"
	"github.com/kapu/hololive-shared/pkg/service/alarm/tier"
)

// Notifier는 dedup claim + 큐 발행 + 발송 마킹을 담당한다.
type Notifier struct {
	dedupService   *dedup.Service
	queuePublisher *queue.Publisher
	tierScheduler  *tier.TieredScheduler
	logger         *slog.Logger
}

// NewNotifier는 알림 발행기를 생성한다.
func NewNotifier(
	dedupService *dedup.Service,
	queuePublisher *queue.Publisher,
	tierScheduler *tier.TieredScheduler,
	logger *slog.Logger,
) (*Notifier, error) {
	if dedupService == nil {
		return nil, errors.New("new notifier: dedup service is nil")
	}

	if queuePublisher == nil {
		return nil, errors.New("new notifier: queue publisher is nil")
	}

	return &Notifier{
		dedupService:   dedupService,
		queuePublisher: queuePublisher,
		tierScheduler:  tierScheduler,
		logger:         checking.SafeLogger(logger),
	}, nil
}

// Send는 알림 목록을 독립 처리한다. 단일 큐 발행 실패가 전체 배치를 중단하지 않도록
// 실패는 집계하고 나머지 알림은 계속 처리한다.
func (n *Notifier) Send(ctx context.Context, notifications []*domain.AlarmNotification) (checking.SendResult, error) {
	result, prepared, errs := n.prepareSendBatch(ctx, notifications)

	if len(prepared) > 0 {
		errs = n.publishPreparedBatch(ctx, prepared, &result, errs)
	}

	n.logger.Info("notification batch completed",
		slog.Int("total", len(notifications)),
		slog.Int("sent", result.Sent),
		slog.Int("skipped", result.Skipped),
		slog.Int("failed", result.Failed),
	)

	return result, errors.Join(errs...)
}

func (n *Notifier) prepareSendBatch(ctx context.Context, notifications []*domain.AlarmNotification) (checking.SendResult, []claimedSend, []error) {
	result := checking.SendResult{}
	var errs []error
	prepared := make([]claimedSend, 0, len(notifications))
	for _, notification := range notifications {
		if err := ctx.Err(); err != nil {
			errs = append(errs, fmt.Errorf("send notifications: context done: %w", err))
			break
		}

		prepared = n.prepareBatchNotification(ctx, notification, prepared, &result, &errs)
	}

	return result, prepared, errs
}

func (n *Notifier) prepareBatchNotification(
	ctx context.Context,
	notification *domain.AlarmNotification,
	prepared []claimedSend,
	result *checking.SendResult,
	errs *[]error,
) []claimedSend {
	payload, claimKeys, singleResult, err := n.prepareOne(ctx, notification)
	prepared = appendPreparedOutcome(prepared, payload, claimKeys, singleResult, err, result)
	if err != nil {
		n.recordPrepareError(notification, err, errs)
	}

	return prepared
}

func appendPreparedOutcome(
	prepared []claimedSend,
	payload *sendInput,
	claimKeys []string,
	outcome sendOutcome,
	err error,
	result *checking.SendResult,
) []claimedSend {
	if outcome == sendOutcomeSent {
		return append(prepared, claimedSend{payload: payload, claimKeys: claimKeys})
	}

	applyNonSentOutcome(outcome, err, result)
	return prepared
}

func applyNonSentOutcome(outcome sendOutcome, err error, result *checking.SendResult) {
	if outcome == sendOutcomeSkipped {
		result.Skipped++
		return
	}

	if outcome == sendOutcomeFailed || err != nil {
		result.Failed++
	}
}

func (n *Notifier) recordPrepareError(notification *domain.AlarmNotification, err error, errs *[]error) {
	*errs = append(*errs, fmt.Errorf("send notification room=%q stream=%q: %w", notificationRoomID(notification), notificationStreamID(notification), err))
	n.logger.Warn("Alarm notification send failed",
		slog.String("room_id", notificationRoomID(notification)),
		slog.String("stream_id", notificationStreamID(notification)),
		slog.Any("error", err),
	)
}

func (n *Notifier) publishPreparedBatch(ctx context.Context, prepared []claimedSend, result *checking.SendResult, errs []error) []error {
	publishedCount, err := n.publishBatchAndMark(ctx, prepared)
	if err != nil {
		result.Sent += publishedCount
		result.Failed += len(prepared) - publishedCount
		errs = append(errs, fmt.Errorf("send notifications: publish batch: %w", err))
	} else {
		result.Sent += publishedCount
	}
	for _, item := range prepared[:publishedCount] {
		if n.tierScheduler != nil {
			n.tierScheduler.MarkChannelRecentlyNotified(item.payload.channelID)
		}
	}
	return errs
}

func notificationRoomID(notification *domain.AlarmNotification) string {
	if notification == nil {
		return ""
	}

	return notification.RoomID
}

func notificationStreamID(notification *domain.AlarmNotification) string {
	if notification == nil || notification.Stream == nil {
		return ""
	}

	return notification.Stream.ID
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

type claimedSend struct {
	payload   *sendInput
	claimKeys []string
}

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

func (n *Notifier) claimDedup(ctx context.Context, payload *sendInput) ([]string, bool, error) {
	category := keys.NotificationCategory(
		n.dedupService.TargetMinutesSnapshot(),
		payload.notification.MinutesUntil,
	)
	notifyKey := keys.BuildNotifyClaimKey(
		payload.notification.RoomID,
		payload.streamID,
		payload.startScheduled,
		category,
	)
	logicalKey := keys.BuildLogicalEventClaimKey(
		payload.notification.RoomID,
		payload.channelID,
		payload.notification.Stream.ID,
		payload.notification.Stream.Title,
		payload.startScheduled,
		category,
	)

	notifyClaimed, logicalClaimed := n.dedupService.TryClaimPair(
		ctx, notifyKey, logicalKey, constants.CacheTTL.NotificationSent,
	)

	if !notifyClaimed {
		if logicalClaimed {
			n.releaseClaimsBestEffort(ctx, []string{logicalKey}, "release logical claim after notification dedup skip")
		}
		return nil, false, nil
	}
	if !logicalClaimed {
		n.releaseClaimsBestEffort(ctx, []string{notifyKey}, "release notification claim after logical dedup skip")
		return nil, false, nil
	}

	claimKeys := compactClaimKeys(notifyKey, logicalKey)
	scheduleClaimKeys, scheduleClaimed, err := n.claimScheduleChangeDedup(ctx, payload)
	if err != nil {
		n.releaseClaimsBestEffort(ctx, append(claimKeys, scheduleClaimKeys...), "failed to release claims after schedule change claim error")
		return nil, false, fmt.Errorf("claim schedule change: %w", err)
	}
	if !scheduleClaimed {
		n.releaseClaimsBestEffort(ctx, append(claimKeys, scheduleClaimKeys...), "failed to release claims after schedule change dedup skip")
		return nil, false, nil
	}

	return append(claimKeys, scheduleClaimKeys...), true, nil
}

func (n *Notifier) claimScheduleChangeDedup(ctx context.Context, payload *sendInput) ([]string, bool, error) {
	if payload == nil || payload.notification == nil {
		return nil, true, nil
	}
	if strings.TrimSpace(payload.notification.ScheduleChangeMessage) == "" {
		return nil, true, nil
	}

	claimKeys, claimed, err := n.dedupService.TryClaimNotificationScheduleChange(
		ctx,
		payload.notification.RoomID,
		payload.channelID,
		payload.notification.Stream,
		payload.notification.ScheduleChangePreviousStart,
	)
	if err != nil {
		return claimKeys, false, fmt.Errorf("claim notification schedule change: %w", err)
	}
	if !claimed {
		return claimKeys, false, nil
	}

	return claimKeys, true, nil
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
