package youtubedispatch

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/lifecycle"
	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/store"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/dispatchstate"
)

type YouTubeOutboxKaringSender interface {
	SendYouTubeOutboxKaring(ctx context.Context, roomID string, payload *domain.YouTubeOutboxDispatchPayload) error
}

func (d *SendEngine) dispatchClaimedRowsWithKaringIfSupported(
	ctx context.Context,
	roomID string,
	channelID string,
	kind domain.OutboxKind,
	rows []domain.YouTubeNotificationDelivery,
	outboxes []domain.YouTubeNotificationOutbox,
	claimTokens []dispatchstate.ClaimToken,
	mode string,
	result *dispatchstate.DispatchResult,
	mu *sync.Mutex,
) bool {
	sender, supported := d.karingSender(kind)
	if !supported {
		return false
	}

	if len(rows) == 0 || len(outboxes) == 0 {
		return true
	}

	d.dispatchClaimedKaring(
		ctx, sender, roomID, channelID, kind, rows, outboxes, claimTokens, mode, result, mu,
	)

	return true
}

func (d *SendEngine) karingSender(kind domain.OutboxKind) (YouTubeOutboxKaringSender, bool) {
	sender, ok := d.sender.(YouTubeOutboxKaringSender)

	return sender, ok && isYouTubeOutboxKaringKind(kind)
}

func (d *SendEngine) dispatchClaimedKaring(
	ctx context.Context,
	sender YouTubeOutboxKaringSender,
	roomID string,
	channelID string,
	kind domain.OutboxKind,
	rows []domain.YouTubeNotificationDelivery,
	outboxes []domain.YouTubeNotificationOutbox,
	claimTokens []dispatchstate.ClaimToken,
	mode string,
	result *dispatchstate.DispatchResult,
	mu *sync.Mutex,
) {
	payload, sendReq, prepared := d.prepareKaringDispatch(
		ctx, roomID, channelID, kind, rows, outboxes, claimTokens, mode, result, mu,
	)
	if !prepared {
		return
	}

	operation, begun := d.beginLifecycleOperation(ctx, rows, outboxes, result, mu)
	if !begun {
		return
	}

	attemptStartedAt := time.Now().UTC()
	d.logCommunityShortsDeliveryAttemptStarted(rows, outboxes, attemptStartedAt, mode)

	if err := d.sendYouTubeOutboxKaring(ctx, sender, roomID, &payload); err != nil {
		d.handleKaringSendFailure(
			ctx, operation, roomID, channelID, kind, rows, outboxes, sendReq, claimTokens, mode, err, result, mu,
		)

		return
	}

	if !d.completeLifecycleSent(ctx, operation, claimTokens, result, mu) {
		return
	}

	d.recordKaringSuccess(ctx, roomID, channelID, kind, rows, outboxes, sendReq, claimTokens, mode, result, mu)
}

func (d *SendEngine) prepareKaringDispatch(
	ctx context.Context,
	roomID string,
	channelID string,
	kind domain.OutboxKind,
	rows []domain.YouTubeNotificationDelivery,
	outboxes []domain.YouTubeNotificationOutbox,
	claimTokens []dispatchstate.ClaimToken,
	mode string,
	result *dispatchstate.DispatchResult,
	mu *sync.Mutex,
) (domain.YouTubeOutboxDispatchPayload, deliverySendRequest, bool) {
	payload, err := d.buildYouTubeOutboxKaringPayload(ctx, channelID, kind, outboxes)
	if err != nil {
		d.handleKaringRequestBuildFailure(
			ctx, roomID, channelID, kind, rows, outboxes, claimTokens, mode, err, result, mu,
		)

		return domain.YouTubeOutboxDispatchPayload{}, deliverySendRequest{}, false
	}

	sendReq, err := buildDeliveryKaringSendRequest(roomID, outboxes)
	if err != nil {
		d.handleKaringRequestBuildFailure(
			ctx, roomID, channelID, kind, rows, outboxes, claimTokens, mode, err, result, mu,
		)

		return domain.YouTubeOutboxDispatchPayload{}, deliverySendRequest{}, false
	}

	return payload, sendReq, true
}

func (d *SendEngine) handleKaringRequestBuildFailure(
	ctx context.Context,
	roomID string,
	channelID string,
	kind domain.OutboxKind,
	rows []domain.YouTubeNotificationDelivery,
	outboxes []domain.YouTubeNotificationOutbox,
	claimTokens []dispatchstate.ClaimToken,
	mode string,
	err error,
	result *dispatchstate.DispatchResult,
	mu *sync.Mutex,
) {
	if d.applyPreparedLifecycleFailure(ctx, rows, outboxes, lifecycle.FailurePermanent, lifecycleReasonRequest, result, mu) {
		d.recordKaringRequestBuildFailure(
			ctx, roomID, channelID, kind, rows, outboxes, claimTokens, mode, err, result, mu,
		)
	}
}

func (d *SendEngine) handleKaringSendFailure(
	ctx context.Context,
	operation store.StartedOperation,
	roomID string,
	channelID string,
	kind domain.OutboxKind,
	rows []domain.YouTubeNotificationDelivery,
	outboxes []domain.YouTubeNotificationOutbox,
	sendReq deliverySendRequest,
	claimTokens []dispatchstate.ClaimToken,
	mode string,
	err error,
	result *dispatchstate.DispatchResult,
	mu *sync.Mutex,
) {
	if d.transition != nil && !d.applyKaringLifecycleFailure(ctx, operation, roomID, channelID, kind, rows, err, result, mu) {
		return
	}

	d.recordKaringSendFailure(
		ctx, roomID, channelID, kind, rows, outboxes, sendReq, claimTokens, mode, err, result, mu,
	)
}

func (d *SendEngine) applyKaringLifecycleFailure(
	ctx context.Context,
	operation store.StartedOperation,
	roomID string,
	channelID string,
	kind domain.OutboxKind,
	rows []domain.YouTubeNotificationDelivery,
	err error,
	result *dispatchstate.DispatchResult,
	mu *sync.Mutex,
) bool {
	failureKind, reason, retryAfter := lifecycleProviderFailure(err, lifecycleReasonKaring)
	if failureKind == lifecycle.FailureOutcomeUnknown {
		d.logger.Warn("Karing delivery outcome unknown, preserving SENDING logical groups",
			slog.String("room_id", roomID),
			slog.String("channel_id", channelID),
			slog.String("kind", string(kind)),
			slog.Int("count", len(rows)),
			slog.Any("error", err))

		return false
	}

	return d.applyStartedLifecycleFailure(ctx, operation, failureKind, reason, retryAfter, result, mu)
}

func isYouTubeOutboxKaringKind(kind domain.OutboxKind) bool {
	switch kind {
	case domain.OutboxKindNewVideo, domain.OutboxKindNewShort, domain.OutboxKindLiveStream, domain.OutboxKindCommunityPost:
		return true
	case domain.OutboxKindMilestone:
		return false
	default:
		return false
	}
}

func (d *SendEngine) sendYouTubeOutboxKaring(
	ctx context.Context,
	sender YouTubeOutboxKaringSender,
	roomID string,
	payload *domain.YouTubeOutboxDispatchPayload,
) error {
	sendCtx, cancel := d.karingSendContext(ctx)
	defer cancel()

	if err := d.karingMu.LockContext(sendCtx); err != nil {
		//nolint:wrapcheck // 오류 생성자가 만든 값이라 감쌀 하위 오류가 없다.
		return d.wrapKaringTimeoutError(sendCtx, "wait for youtube outbox karing send slot", err)
	}

	defer d.karingMu.Unlock()

	if err := sender.SendYouTubeOutboxKaring(sendCtx, roomID, payload); err != nil {
		//nolint:wrapcheck // 오류 생성자가 만든 값이라 감쌀 하위 오류가 없다.
		return d.wrapKaringTimeoutError(sendCtx, "send youtube outbox karing", err)
	}

	return nil
}

func (d *SendEngine) karingSendContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if d.config.DeliverySendTimeout > 0 {
		return context.WithTimeoutCause(ctx, d.config.DeliverySendTimeout, errDeliverySendTimeout)
	}

	return ctx, func() {}
}

func (d *SendEngine) wrapKaringTimeoutError(ctx context.Context, action string, err error) error {
	if errors.Is(context.Cause(ctx), errDeliverySendTimeout) {
		return fmt.Errorf("%s timed out after %s: %w", action, d.config.DeliverySendTimeout, errors.Join(errDeliverySendOutcomeUnknown, errDeliverySendTimeout, err))
	}

	if deliverySendOutcomeUnknown(err) {
		return fmt.Errorf("%s: %w", action, errors.Join(errDeliverySendOutcomeUnknown, err))
	}

	return fmt.Errorf("%s: %w", action, err)
}

func (d *SendEngine) buildYouTubeOutboxKaringPayload(
	ctx context.Context,
	channelID string,
	kind domain.OutboxKind,
	outboxes []domain.YouTubeNotificationOutbox,
) (domain.YouTubeOutboxDispatchPayload, error) {
	memberName, err := d.formatter.getMemberName(ctx, channelID)
	if err != nil || strings.TrimSpace(memberName) == "" {
		memberName = d.formatter.vtuberFallback(ctx)
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
		return deliverySendRequest{}, fmt.Errorf("build delivery send request: %w", err)
	}

	return req, nil
}

func (d *SendEngine) recordKaringRequestBuildFailure(
	ctx context.Context,
	roomID string,
	channelID string,
	kind domain.OutboxKind,
	rows []domain.YouTubeNotificationDelivery,
	outboxes []domain.YouTubeNotificationOutbox,
	claimTokens []dispatchstate.ClaimToken,
	mode string,
	err error,
	result *dispatchstate.DispatchResult,
	mu *sync.Mutex,
) {
	d.metricsRecorder.recordKaringRequestBuildFailure(ctx, roomID, channelID, kind, rows, outboxes, claimTokens, mode, err, result, mu)
}

func (d *SendEngine) recordKaringSendFailure(
	ctx context.Context,
	roomID string,
	channelID string,
	kind domain.OutboxKind,
	rows []domain.YouTubeNotificationDelivery,
	outboxes []domain.YouTubeNotificationOutbox,
	sendReq deliverySendRequest,
	claimTokens []dispatchstate.ClaimToken,
	mode string,
	err error,
	result *dispatchstate.DispatchResult,
	mu *sync.Mutex,
) {
	d.metricsRecorder.recordKaringSendFailure(ctx, roomID, channelID, kind, rows, outboxes, sendReq, claimTokens, mode, err, result, mu)
}

func (d *SendEngine) recordKaringSuccess(
	ctx context.Context,
	roomID string,
	channelID string,
	kind domain.OutboxKind,
	rows []domain.YouTubeNotificationDelivery,
	outboxes []domain.YouTubeNotificationOutbox,
	sendReq deliverySendRequest,
	claimTokens []dispatchstate.ClaimToken,
	mode string,
	result *dispatchstate.DispatchResult,
	mu *sync.Mutex,
) {
	d.metricsRecorder.recordKaringSuccess(ctx, roomID, channelID, kind, rows, outboxes, sendReq, claimTokens, mode, result, mu)
}
