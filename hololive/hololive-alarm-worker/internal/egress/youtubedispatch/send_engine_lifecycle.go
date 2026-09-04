package youtubedispatch

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/park285/iris-client-go/v2/iris"

	"github.com/kapu/hololive-alarm-worker/internal/egress"
	ytlifecycle "github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/lifecycle"
	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/store"
	"github.com/kapu/hololive-alarm-worker/internal/service/youtube/outbox/dispatchstate"
	"github.com/kapu/hololive-shared/pkg/domain"
)

const (
	lifecycleReasonPreSendClaim ytlifecycle.Reason = "pre_send_claim"
	lifecycleReasonFormat       ytlifecycle.Reason = "format_message"
	lifecycleReasonMessage      ytlifecycle.Reason = "message_missing"
	lifecycleReasonRequest      ytlifecycle.Reason = "invalid_send_request"
	lifecycleReasonAuth         ytlifecycle.Reason = "provider_auth"
	lifecycleReasonRateLimited  ytlifecycle.Reason = "provider_rate_limited"
	lifecycleReasonTransport    ytlifecycle.Reason = "provider_transport"
	lifecycleReasonPermanent    ytlifecycle.Reason = "provider_permanent"
	lifecycleReasonHandoff      ytlifecycle.Reason = "handoff_publish"
	lifecycleReasonKaring       ytlifecycle.Reason = "karing_send"
	lifecycleReasonUnknownError ytlifecycle.Reason = "provider_outcome_unknown"
)

func (d *SendEngine) beginLifecycleOperation(
	ctx context.Context,
	rows []domain.YouTubeNotificationDelivery,
	outboxes []domain.YouTubeNotificationOutbox,
	result *dispatchstate.DispatchResult,
	mu *sync.Mutex,
) (store.StartedOperation, bool) {
	if d.transition == nil {
		return store.StartedOperation{}, true
	}

	operation, applied, err := d.transition.BeginSending(ctx, rows, outboxMap(outboxes))
	observeLifecycleApply("begin_sending", applied, err, len(rows))

	if err != nil || applied.Outcome != store.ApplyApplied {
		d.logLifecycleApplyFailure("begin_sending", applied, err, len(rows))

		return store.StartedOperation{}, false
	}

	appendLifecycleTouched(result, mu, applied.TouchedOutboxIDs)

	return operation, true
}

func (d *SendEngine) applyPreparedLifecycleFailure(
	ctx context.Context,
	rows []domain.YouTubeNotificationDelivery,
	outboxes []domain.YouTubeNotificationOutbox,
	kind ytlifecycle.FailureKind,
	reason ytlifecycle.Reason,
	result *dispatchstate.DispatchResult,
	mu *sync.Mutex,
) bool {
	if d.transition == nil {
		return true
	}

	applied, err := d.transition.ApplyPreparedFailure(ctx, rows, outboxMap(outboxes), kind, reason, 0)
	observeLifecycleApply("prepared_failure", applied, err, len(rows))

	if err != nil || applied.Outcome != store.ApplyApplied {
		d.logLifecycleApplyFailure("prepared_failure", applied, err, len(rows))

		return false
	}

	appendLifecycleTouched(result, mu, applied.TouchedOutboxIDs)

	return true
}

func (d *SendEngine) applyStartedLifecycleFailure(
	ctx context.Context,
	operation store.StartedOperation,
	kind ytlifecycle.FailureKind,
	reason ytlifecycle.Reason,
	retryAfter time.Duration,
	result *dispatchstate.DispatchResult,
	mu *sync.Mutex,
) bool {
	if d.transition == nil {
		return true
	}

	applied, err := d.transition.ApplyStartedFailure(ctx, operation, kind, reason, retryAfter)
	observeLifecycleApply("provider_failure", applied, err, operation.OwnerCount())

	if err != nil || applied.Outcome != store.ApplyApplied {
		d.logLifecycleApplyFailure("provider_failure", applied, err, operation.OwnerCount())

		return false
	}

	appendLifecycleTouched(result, mu, applied.TouchedOutboxIDs)

	return true
}

func (d *SendEngine) completeLifecycleSent(
	ctx context.Context,
	operation store.StartedOperation,
	claimTokens []dispatchstate.ClaimToken,
	result *dispatchstate.DispatchResult,
	mu *sync.Mutex,
) bool {
	if d.transition == nil {
		return true
	}

	applied, err := d.transition.CompleteSent(ctx, operation, claimTokens)
	observeLifecycleApply("complete_sent", applied, err, operation.OwnerCount())

	if err != nil || applied.Outcome != store.ApplyApplied {
		d.logLifecycleApplyFailure("complete_sent", applied, err, operation.OwnerCount())

		return false
	}

	appendLifecycleTouched(result, mu, applied.TouchedOutboxIDs)
	observeLedgerOperation("record_sent", applied, operation.OwnerCount())

	return true
}

func (d *SendEngine) applyLifecycleClaimSelection(
	ctx context.Context,
	selection *deliveryClaimSelection,
	result *dispatchstate.DispatchResult,
	mu *sync.Mutex,
) {
	if selection == nil {
		return
	}

	if d.transition == nil {
		for i := range selection.retryRows {
			d.recordDeliveryFailure(
				result,
				mu,
				deliveryFailureReasonPreSendClaim,
				selection.retryRows[i].ID,
				selection.retryRows[i].OutboxID,
			)
		}

		mu.Lock()

		result.SuccessDeliveryIDs = append(result.SuccessDeliveryIDs, selection.alreadySentDeliveryIDs...)
		result.TouchedOutboxIDs = append(result.TouchedOutboxIDs, selection.alreadySentOutboxIDs...)
		mu.Unlock()

		return
	}

	if len(selection.retryRows) > 0 && d.applyPreparedLifecycleFailure(
		ctx,
		selection.retryRows,
		selection.retryOutboxes,
		ytlifecycle.FailureRetryable,
		lifecycleReasonPreSendClaim,
		result,
		mu,
	) {
		for i := range selection.retryRows {
			d.recordDeliveryFailure(
				result,
				mu,
				deliveryFailureReasonPreSendClaim,
				selection.retryRows[i].ID,
				selection.retryRows[i].OutboxID,
			)
		}
	}

	if len(selection.alreadySentRows) == 0 {
		return
	}

	prepared, err := d.transition.PrepareClaimed(
		ctx,
		selection.alreadySentRows,
		outboxMap(selection.alreadySentOutboxes),
	)
	observeLogicalResolutions(prepared.Resolutions)

	if err != nil || len(prepared.ActiveRows) > 0 || len(prepared.Blocked) > 0 {
		d.logger.Error("Failed to reconcile already-fulfilled logical delivery",
			slog.Int("delivery_count", len(selection.alreadySentRows)),
			slog.Int("active_count", len(prepared.ActiveRows)),
			slog.Int("blocked_count", len(prepared.Blocked)),
			slog.Any("error", err))

		return
	}

	appendLifecycleTouched(result, mu, prepared.TouchedOutboxIDs)
	mu.Lock()

	result.SuccessDeliveryIDs = append(result.SuccessDeliveryIDs, selection.alreadySentDeliveryIDs...)
	mu.Unlock()
}

func lifecycleProviderFailure(err error, defaultReason ytlifecycle.Reason) (ytlifecycle.FailureKind, ytlifecycle.Reason, time.Duration) {
	if errors.Is(err, egress.ErrKaringStatusFailed) {
		return ytlifecycle.FailurePermanent, defaultReason, 0
	}

	if errors.Is(err, errDeliverySendOutcomeUnknown) {
		observeDeliveryOutcomeUnknown(string(defaultReason))

		return ytlifecycle.FailureOutcomeUnknown, lifecycleReasonUnknownError, 0
	}

	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return ytlifecycle.FailureRetryable, lifecycleReasonTransport, 0
	}

	switch {
	case errors.Is(err, iris.ErrAuthFailed):
		return ytlifecycle.FailurePermanent, lifecycleReasonAuth, 0
	case errors.Is(err, iris.ErrPermanent):
		return ytlifecycle.FailurePermanent, lifecycleReasonPermanent, 0
	case errors.Is(err, iris.ErrRateLimited):
		return ytlifecycle.FailureRetryable, lifecycleReasonRateLimited, deliveryRetryAfter(err)
	case errors.Is(err, iris.ErrTransport):
		return ytlifecycle.FailureRetryable, lifecycleReasonTransport, deliveryRetryAfter(err)
	default:
		observeDeliveryOutcomeUnknown(string(defaultReason))

		return ytlifecycle.FailureOutcomeUnknown, defaultReason, 0
	}
}

func (d *SendEngine) logLifecycleApplyFailure(
	operation string,
	result store.ApplyResult,
	err error,
	deliveryCount int,
) {
	d.logger.Error("YouTube delivery lifecycle transition was not confirmed",
		slog.String("operation", operation),
		slog.String("outcome", result.Outcome.String()),
		slog.Int("delivery_count", deliveryCount),
		slog.Any("error", err))
}

func outboxMap(outboxes []domain.YouTubeNotificationOutbox) map[int64]domain.YouTubeNotificationOutbox {
	result := make(map[int64]domain.YouTubeNotificationOutbox, len(outboxes))
	for i := range outboxes {
		result[outboxes[i].ID] = outboxes[i]
	}

	return result
}

func appendLifecycleTouched(result *dispatchstate.DispatchResult, mu *sync.Mutex, outboxIDs []int64) {
	if result == nil || mu == nil || len(outboxIDs) == 0 {
		return
	}

	mu.Lock()

	result.TouchedOutboxIDs = append(result.TouchedOutboxIDs, outboxIDs...)
	mu.Unlock()
}
