package youtubedispatch

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/lifecycle"
	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/store"
	"github.com/kapu/hololive-alarm-worker/internal/service/youtube/outbox/dispatchstate"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/alarm/handoff"
)

type YouTubeOutboxHandoff interface {
	PublishPending(ctx context.Context, roomID string, payload *domain.YouTubeOutboxDispatchPayload) error
	PublishShadow(ctx context.Context, roomID string, payload *domain.YouTubeOutboxDispatchPayload) error
}

func (d *SendEngine) dispatchClaimedRowsWithHandoff(
	ctx context.Context,
	roomID string,
	channelID string,
	kind domain.OutboxKind,
	rows []domain.YouTubeNotificationDelivery,
	outboxes []domain.YouTubeNotificationOutbox,
	claimTokens []dispatchstate.ClaimToken,
	result *dispatchstate.DispatchResult,
	mu *sync.Mutex,
) bool {
	if d.handoffMode == handoff.ModeOff || d.handoff == nil || len(rows) == 0 || len(outboxes) == 0 {
		return false
	}

	payload, err := d.buildYouTubeOutboxKaringPayload(ctx, channelID, kind, outboxes)
	if err != nil {
		if d.handoffMode == handoff.ModeShadow {
			d.logger.Warn("YouTube outbox shadow handoff payload build failed",
				slog.String("room_id", roomID),
				slog.String("channel_id", channelID),
				slog.String("kind", string(kind)),
				slog.Any("error", err))
			observeYouTubeOutboxHandoff(handoff.ModeShadow, "failure", len(rows))

			return false
		}

		if d.applyPreparedLifecycleFailure(ctx, rows, outboxes, lifecycle.FailurePermanent, lifecycleReasonRequest, result, mu) {
			d.recordHandoffFailure(ctx, roomID, channelID, kind, rows, outboxes, claimTokens, "payload", err, result, mu)
		}

		return true
	}

	return d.publishClaimedRowsWithHandoff(ctx, roomID, channelID, kind, rows, outboxes, claimTokens, &payload, result, mu)
}

func (d *SendEngine) publishClaimedRowsWithHandoff(
	ctx context.Context,
	roomID string,
	channelID string,
	kind domain.OutboxKind,
	rows []domain.YouTubeNotificationDelivery,
	outboxes []domain.YouTubeNotificationOutbox,
	claimTokens []dispatchstate.ClaimToken,
	payload *domain.YouTubeOutboxDispatchPayload,
	result *dispatchstate.DispatchResult,
	mu *sync.Mutex,
) bool {
	switch d.handoffMode {
	case handoff.ModeOff:
		return false
	case handoff.ModeShadow:
		return d.publishShadowRows(ctx, roomID, channelID, kind, rows, payload)
	case handoff.ModeCutover:
		return d.publishCutoverRows(ctx, roomID, channelID, kind, rows, outboxes, claimTokens, payload, result, mu)
	default:
		err := fmt.Errorf("unsupported youtube outbox handoff mode %q", d.handoffMode)
		if d.applyPreparedLifecycleFailure(ctx, rows, outboxes, lifecycle.FailurePermanent, lifecycleReasonRequest, result, mu) {
			d.recordHandoffFailure(ctx, roomID, channelID, kind, rows, outboxes, claimTokens, "mode", err, result, mu)
		}

		return true
	}
}

func (d *SendEngine) publishShadowRows(
	ctx context.Context,
	roomID string,
	channelID string,
	kind domain.OutboxKind,
	rows []domain.YouTubeNotificationDelivery,
	payload *domain.YouTubeOutboxDispatchPayload,
) bool {
	if err := d.handoff.PublishShadow(ctx, roomID, payload); err != nil {
		d.logger.Warn("YouTube outbox shadow handoff failed",
			slog.String("room_id", roomID),
			slog.String("channel_id", channelID),
			slog.String("kind", string(kind)),
			slog.Any("error", err))
		observeYouTubeOutboxHandoff(handoff.ModeShadow, "failure", len(rows))
	} else {
		observeYouTubeOutboxHandoff(handoff.ModeShadow, "success", len(rows))
	}

	return false
}

func (d *SendEngine) publishCutoverRows(
	ctx context.Context,
	roomID string,
	channelID string,
	kind domain.OutboxKind,
	rows []domain.YouTubeNotificationDelivery,
	outboxes []domain.YouTubeNotificationOutbox,
	claimTokens []dispatchstate.ClaimToken,
	payload *domain.YouTubeOutboxDispatchPayload,
	result *dispatchstate.DispatchResult,
	mu *sync.Mutex,
) bool {
	operation, begun := d.beginLifecycleOperation(ctx, rows, outboxes, result, mu)
	if !begun {
		return true
	}

	if err := d.handoff.PublishPending(ctx, roomID, payload); err != nil {
		return d.handleCutoverPublishFailure(
			ctx, roomID, channelID, kind, rows, outboxes, claimTokens, operation, err, result, mu,
		)
	}

	if !d.completeLifecycleSent(ctx, operation, claimTokens, result, mu) {
		return true
	}

	d.recordHandoffSuccess(ctx, roomID, channelID, kind, rows, outboxes, claimTokens, result, mu)
	observeYouTubeOutboxHandoff(handoff.ModeCutover, "success", len(rows))

	return true
}

func (d *SendEngine) handleCutoverPublishFailure(
	ctx context.Context,
	roomID string,
	channelID string,
	kind domain.OutboxKind,
	rows []domain.YouTubeNotificationDelivery,
	outboxes []domain.YouTubeNotificationOutbox,
	claimTokens []dispatchstate.ClaimToken,
	operation store.StartedOperation,
	publishErr error,
	result *dispatchstate.DispatchResult,
	mu *sync.Mutex,
) bool {
	if d.transition != nil {
		failureKind, reason, retryAfter := lifecycleProviderFailure(publishErr, lifecycleReasonHandoff)
		if failureKind == lifecycle.FailureOutcomeUnknown {
			d.logger.Warn("YouTube outbox cutover handoff outcome unknown, preserving SENDING logical groups",
				slog.String("room_id", roomID),
				slog.String("channel_id", channelID),
				slog.String("kind", string(kind)),
				slog.Int("count", len(rows)),
				slog.Any("error", publishErr))

			return true
		}

		if !d.applyStartedLifecycleFailure(ctx, operation, failureKind, reason, retryAfter, result, mu) {
			return true
		}
	}

	d.recordHandoffFailure(ctx, roomID, channelID, kind, rows, outboxes, claimTokens, "publish", publishErr, result, mu)
	observeYouTubeOutboxHandoff(handoff.ModeCutover, "failure", len(rows))

	return true
}

func (d *SendEngine) recordHandoffFailure(
	ctx context.Context,
	roomID string,
	channelID string,
	kind domain.OutboxKind,
	rows []domain.YouTubeNotificationDelivery,
	outboxes []domain.YouTubeNotificationOutbox,
	claimTokens []dispatchstate.ClaimToken,
	stage string,
	err error,
	result *dispatchstate.DispatchResult,
	mu *sync.Mutex,
) {
	d.metricsRecorder.recordHandoffFailure(ctx, roomID, channelID, kind, rows, outboxes, claimTokens, stage, err, result, mu)
}

func (d *SendEngine) recordHandoffSuccess(
	ctx context.Context,
	roomID string,
	channelID string,
	kind domain.OutboxKind,
	rows []domain.YouTubeNotificationDelivery,
	outboxes []domain.YouTubeNotificationOutbox,
	claimTokens []dispatchstate.ClaimToken,
	result *dispatchstate.DispatchResult,
	mu *sync.Mutex,
) {
	d.metricsRecorder.recordHandoffSuccess(ctx, roomID, channelID, kind, rows, outboxes, claimTokens, result, mu)
}
