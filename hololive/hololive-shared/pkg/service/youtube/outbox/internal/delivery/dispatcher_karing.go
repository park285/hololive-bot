package delivery

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

type YouTubeOutboxKaringSender interface {
	SendYouTubeOutboxKaring(ctx context.Context, roomID string, payload domain.YouTubeOutboxDispatchPayload) error
}

func (d *Dispatcher) dispatchClaimedRowsWithKaringIfSupported(
	ctx context.Context,
	roomID string,
	channelID string,
	kind domain.OutboxKind,
	rows []domain.YouTubeNotificationDelivery,
	outboxes []domain.YouTubeNotificationOutbox,
	claimTokens []deliveryClaimToken,
	mode string,
	result *deliveryDispatchResult,
	mu *sync.Mutex,
) bool {
	sender, ok := d.sender.(YouTubeOutboxKaringSender)
	if !ok {
		return false
	}
	if !isYouTubeOutboxKaringKind(kind) {
		return false
	}
	if len(rows) == 0 || len(outboxes) == 0 {
		return true
	}
	payload, err := d.buildYouTubeOutboxKaringPayload(ctx, channelID, kind, outboxes)
	if err != nil {
		d.recordKaringRequestBuildFailure(ctx, roomID, channelID, kind, rows, outboxes, claimTokens, mode, err, result, mu)
		return true
	}
	sendReq, err := buildDeliveryKaringSendRequest(roomID, outboxes)
	if err != nil {
		d.recordKaringRequestBuildFailure(ctx, roomID, channelID, kind, rows, outboxes, claimTokens, mode, err, result, mu)
		return true
	}

	attemptStartedAt := time.Now().UTC()
	d.logCommunityShortsDeliveryAttemptStarted(rows, outboxes, attemptStartedAt, mode)
	if err := d.sendYouTubeOutboxKaring(ctx, sender, roomID, payload); err != nil {
		d.recordKaringSendFailure(ctx, roomID, channelID, kind, rows, outboxes, sendReq, claimTokens, mode, err, result, mu)
		return true
	}
	d.recordKaringSuccess(ctx, roomID, channelID, kind, rows, outboxes, sendReq, claimTokens, mode, result, mu)
	return true
}

func isYouTubeOutboxKaringKind(kind domain.OutboxKind) bool {
	switch kind {
	case domain.OutboxKindNewVideo, domain.OutboxKindNewShort, domain.OutboxKindLiveStream, domain.OutboxKindCommunityPost:
		return true
	default:
		return false
	}
}

func (d *Dispatcher) sendYouTubeOutboxKaring(
	ctx context.Context,
	sender YouTubeOutboxKaringSender,
	roomID string,
	payload domain.YouTubeOutboxDispatchPayload,
) error {
	d.karingMu.Lock()
	defer d.karingMu.Unlock()

	sendCtx := ctx
	cancel := func() {}
	if d.config.DeliverySendTimeout > 0 {
		sendCtx, cancel = context.WithTimeoutCause(ctx, d.config.DeliverySendTimeout, errDeliverySendTimeout)
	}
	defer cancel()

	if err := sender.SendYouTubeOutboxKaring(sendCtx, roomID, payload); err != nil {
		if errors.Is(context.Cause(sendCtx), errDeliverySendTimeout) {
			return fmt.Errorf("send youtube outbox karing timed out after %s: %w", d.config.DeliverySendTimeout, errors.Join(errDeliverySendTimeout, err))
		}
		return fmt.Errorf("send youtube outbox karing: %w", err)
	}
	return nil
}

func (d *Dispatcher) buildYouTubeOutboxKaringPayload(
	ctx context.Context,
	channelID string,
	kind domain.OutboxKind,
	outboxes []domain.YouTubeNotificationOutbox,
) (domain.YouTubeOutboxDispatchPayload, error) {
	memberName, err := d.formatter.getMemberName(ctx, channelID)
	if err != nil || strings.TrimSpace(memberName) == "" {
		memberName = "VTuber"
	}
	payload := domain.YouTubeOutboxDispatchPayload{
		OutboxIDs:  make([]int64, 0, len(outboxes)),
		Kind:       kind,
		AlarmType:  kind.ToAlarmType(),
		ChannelID:  channelID,
		MemberName: strings.TrimSpace(memberName),
		Items:      make([]domain.YouTubeOutboxItem, 0, len(outboxes)),
	}
	for i := range outboxes {
		payload.OutboxIDs = append(payload.OutboxIDs, outboxes[i].ID)
		payload.Items = append(payload.Items, domain.YouTubeOutboxItem{
			OutboxID:  outboxes[i].ID,
			ContentID: outboxes[i].ContentID,
			Payload:   outboxes[i].Payload,
		})
	}
	if err := payload.Validate(); err != nil {
		return domain.YouTubeOutboxDispatchPayload{}, fmt.Errorf("build youtube outbox karing payload: %w", err)
	}
	return payload, nil
}

func buildDeliveryKaringSendRequest(roomID string, outboxes []domain.YouTubeNotificationOutbox) (deliverySendRequest, error) {
	req, err := buildDeliverySendRequest(roomID, "karing", outboxes)
	if err != nil {
		return deliverySendRequest{}, err
	}
	return req, nil
}

func (d *Dispatcher) recordKaringRequestBuildFailure(
	ctx context.Context,
	roomID string,
	channelID string,
	kind domain.OutboxKind,
	rows []domain.YouTubeNotificationDelivery,
	outboxes []domain.YouTubeNotificationOutbox,
	claimTokens []deliveryClaimToken,
	mode string,
	err error,
	result *deliveryDispatchResult,
	mu *sync.Mutex,
) {
	d.releaseDeliveryClaimsWithWarning(ctx, claimTokens, "Failed to release Karing delivery claims after request build error",
		slog.String("room_id", roomID),
		slog.String("channel_id", channelID),
	)
	failedAt := time.Now()
	d.logger.Warn("Failed to build Karing delivery request",
		slog.String("room_id", roomID),
		slog.String("channel_id", channelID),
		slog.String("kind", string(kind)),
		slog.Int("count", len(outboxes)),
		dedupeKeyLogAttrForOutboxes(outboxes),
		slog.Any("error", err))
	d.logCommunityShortsDeliveryAudit(ctx, rows, outboxes, failedAt, mode, "failure", "karing request", err)
	d.logCommunityShortsDeliveryResult(rows, outboxes, failedAt, mode, "failure", "karing request")
	for i := range rows {
		d.recordDeliveryFailure(result, mu, "karing request", rows[i].ID, rows[i].OutboxID)
	}
}

func (d *Dispatcher) recordKaringSendFailure(
	ctx context.Context,
	roomID string,
	channelID string,
	kind domain.OutboxKind,
	rows []domain.YouTubeNotificationDelivery,
	outboxes []domain.YouTubeNotificationOutbox,
	sendReq deliverySendRequest,
	claimTokens []deliveryClaimToken,
	mode string,
	err error,
	result *deliveryDispatchResult,
	mu *sync.Mutex,
) {
	d.releaseDeliveryClaimsWithWarning(ctx, claimTokens, "Failed to release Karing delivery claims after send failure",
		slog.String("room_id", roomID),
		slog.String("channel_id", channelID),
	)
	failedAt := time.Now()
	d.logger.Warn("Failed to send Karing delivery",
		slog.String("room_id", roomID),
		slog.String("channel_id", channelID),
		slog.String("kind", string(kind)),
		slog.Int("count", len(rows)),
		dedupeKeyLogAttr(sendReq.dedupeKeys),
		slog.Any("error", err))
	d.logCommunityShortsDeliveryAudit(ctx, rows, outboxes, failedAt, mode, "failure", "karing send", err)
	d.logCommunityShortsDeliveryResult(rows, outboxes, failedAt, mode, "failure", "karing send")
	for i := range rows {
		d.recordDeliveryFailure(result, mu, "karing send", rows[i].ID, rows[i].OutboxID)
	}
}

func (d *Dispatcher) recordKaringSuccess(
	ctx context.Context,
	roomID string,
	channelID string,
	kind domain.OutboxKind,
	rows []domain.YouTubeNotificationDelivery,
	outboxes []domain.YouTubeNotificationOutbox,
	sendReq deliverySendRequest,
	claimTokens []deliveryClaimToken,
	mode string,
	result *deliveryDispatchResult,
	mu *sync.Mutex,
) {
	sentAt := time.Now()
	d.logger.Info("Sent Karing delivery",
		slog.String("room_id", roomID),
		slog.String("channel_id", channelID),
		slog.String("kind", string(kind)),
		slog.String("delivery_mode", mode),
		slog.Int("count", len(rows)),
		dedupeKeyLogAttr(sendReq.dedupeKeys))
	d.logCommunityShortsDeliveryAudit(ctx, rows, outboxes, sentAt, mode, "success", "", nil)
	d.logCommunityShortsDeliveryResult(rows, outboxes, sentAt, mode, "success", "")

	mu.Lock()
	for i := range rows {
		result.successDeliveryIDs = append(result.successDeliveryIDs, rows[i].ID)
		result.touchedOutboxIDs = append(result.touchedOutboxIDs, rows[i].OutboxID)
	}
	result.successClaimTokens = append(result.successClaimTokens, claimTokens...)
	mu.Unlock()
}
