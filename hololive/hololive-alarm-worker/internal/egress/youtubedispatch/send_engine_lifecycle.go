package youtubedispatch

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/park285/iris-client-go/v5/iris"
	"github.com/park285/shared-go/postgres/pgxdb"

	"github.com/kapu/hololive-alarm-worker/internal/egress/sendoutcome"
	ytlifecycle "github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/lifecycle"
	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/store"
	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubeoutbox/telemetry"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/privacylog"
	ytcontentid "github.com/kapu/hololive-shared/pkg/service/youtube/contentid"
)

const (
	lifecycleReasonPreSendClaim ytlifecycle.Reason = "pre_send_claim"
	lifecycleReasonFormat       ytlifecycle.Reason = "format_message"
	lifecycleReasonMessage      ytlifecycle.Reason = "message_missing"
	lifecycleReasonRequest      ytlifecycle.Reason = "invalid_send_request"
	lifecycleReasonLeaseBudget  ytlifecycle.Reason = "send_lease_budget"
	lifecycleReasonAuth         ytlifecycle.Reason = "provider_auth"
	lifecycleReasonRateLimited  ytlifecycle.Reason = "provider_rate_limited"
	lifecycleReasonTransport    ytlifecycle.Reason = "provider_transport"
	lifecycleReasonPermanent    ytlifecycle.Reason = "provider_permanent"
	lifecycleReasonUnknownError ytlifecycle.Reason = "provider_outcome_unknown"
)

const deliveryStateTimeout = 5 * time.Second

func deliveryStateContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(pgxdb.WithControlAcquire(context.WithoutCancel(ctx)), deliveryStateTimeout)
}

func (d *SendEngine) beginLifecycleOperation(
	ctx context.Context,
	attempt *deliveryAttempt,
	result *deliveryDispatchResult,
) bool {
	ctx, cancel := context.WithTimeout(pgxdb.WithControlAcquire(ctx), deliveryStateTimeout)
	defer cancel()

	startedAt := time.Now()
	operation, applied, err := d.transition.BeginSending(ctx, attempt.rows, outboxMap(attempt.outboxes))
	observeLifecycleApply("begin_sending", applied, err, len(attempt.rows))

	if err != nil || applied.Outcome != store.ApplyApplied {
		d.logLifecycleApplyFailure("begin_sending", applied, err, len(attempt.rows))

		return false
	}

	appendLifecycleTouched(result, applied.TouchedOutboxIDs)

	attempt.operation = operation
	attempt.sendingStartedAt = startedAt

	return true
}

func (d *SendEngine) applyPreparedLifecycleFailure(
	ctx context.Context,
	rows []domain.YouTubeNotificationDelivery,
	outboxes []domain.YouTubeNotificationOutbox,
	kind ytlifecycle.FailureKind,
	reason ytlifecycle.Reason,
	mode store.DeliveryMode,
	result *deliveryDispatchResult,
) bool {
	ctx, cancel := context.WithTimeout(pgxdb.WithControlAcquire(ctx), deliveryStateTimeout)
	defer cancel()

	applied, err := d.transition.ApplyPreparedFailure(ctx, rows, outboxMap(outboxes), kind, reason, 0, mode)
	observeLifecycleApply("prepared_failure", applied, err, len(rows))

	if err != nil || applied.Outcome != store.ApplyApplied {
		d.logLifecycleApplyFailure("prepared_failure", applied, err, len(rows))

		return false
	}

	appendLifecycleTouched(result, applied.TouchedOutboxIDs)

	return true
}

func (d *SendEngine) applyStartedLifecycleFailure(
	ctx context.Context,
	operation store.StartedOperation,
	kind ytlifecycle.FailureKind,
	reason ytlifecycle.Reason,
	retryAfter time.Duration,
	mode store.DeliveryMode,
	result *deliveryDispatchResult,
) bool {
	ctx, cancel := deliveryStateContext(ctx)
	defer cancel()

	applied, err := d.transition.ApplyStartedFailure(ctx, operation, kind, reason, retryAfter, mode)
	observeLifecycleApply("provider_failure", applied, err, operation.OwnerCount())

	if err != nil || applied.Outcome != store.ApplyApplied {
		d.logLifecycleApplyFailure("provider_failure", applied, err, operation.OwnerCount())

		return false
	}

	appendLifecycleTouched(result, applied.TouchedOutboxIDs)

	return true
}

func (d *SendEngine) completeLifecycleSent(
	ctx context.Context,
	attempt *deliveryAttempt,
	mode store.DeliveryMode,
	result *deliveryDispatchResult,
) bool {
	ctx, cancel := deliveryStateContext(ctx)
	defer cancel()

	operation := attempt.operation
	admission := store.HandoffAdmission{IrisRequestID: attempt.irisRequestID, Horizon: d.config.HandoffHorizon}

	if frozen := attempt.request.frozen; frozen != nil {
		admission.RequestBaseID = frozen.BaseID
		admission.RequestGeneration = frozen.Generation
	}

	applied, err := d.transition.CompleteSent(ctx, operation, mode, admission)
	observeLifecycleApply("complete_sent", applied, err, operation.OwnerCount())

	if err != nil || applied.Outcome != store.ApplyApplied {
		d.logLifecycleApplyFailure("complete_sent", applied, err, operation.OwnerCount())

		return false
	}

	appendLifecycleTouched(result, applied.TouchedOutboxIDs)
	observeLedgerOperation("record_sent", applied, operation.OwnerCount())

	return true
}

func (d *SendEngine) selectDeliverableMembers(
	ctx context.Context,
	rows []domain.YouTubeNotificationDelivery,
	outboxes []domain.YouTubeNotificationOutbox,
	mode store.DeliveryMode,
	result *deliveryDispatchResult,
) []claimedDelivery {
	members := make([]claimedDelivery, 0, min(len(rows), len(outboxes)))

	var unresolved []claimedDelivery

	for i := range rows {
		if i >= len(outboxes) {
			break
		}

		member := claimedDelivery{row: rows[i], outbox: outboxes[i]}
		if _, err := ytcontentid.ResolveDeliveryKey(member.outbox.Kind, member.outbox.ContentID, member.outbox.Payload, member.row.RoomID); err != nil {
			d.logger.Warn("Failed to resolve delivery logical identity before send",
				deliveryLogAttrs(&member.row, &member.outbox, slog.Any("error", err))...)

			unresolved = append(unresolved, member)

			continue
		}

		members = append(members, member)
	}

	if len(unresolved) > 0 {
		d.applyPreSendFailure(ctx, unresolved, mode, result)
	}

	return members
}

func (d *SendEngine) applyPreSendFailure(
	ctx context.Context,
	members []claimedDelivery,
	mode store.DeliveryMode,
	result *deliveryDispatchResult,
) {
	attempt := newDeliveryAttempt(members)
	if !d.applyPreparedLifecycleFailure(
		ctx,
		attempt.rows,
		attempt.outboxes,
		ytlifecycle.FailureRetryable,
		lifecycleReasonPreSendClaim,
		mode,
		result,
	) {
		return
	}

	for i := range attempt.members {
		d.recordDeliveryFailure(result, attempt.members[i].row.OutboxID)
	}
}

func deliveryLogAttrs(
	row *domain.YouTubeNotificationDelivery,
	outbox *domain.YouTubeNotificationOutbox,
	attrs ...any,
) []any {
	baseAttrs := make([]any, 0, 7+len(attrs))

	baseAttrs = append(baseAttrs,
		slog.Int64(FieldDeliveryID, row.ID),
		slog.Int64(FieldOutboxID, outbox.ID),
		privacylog.RoomIDAttr(row.RoomID),
		slog.String(FieldChannelID, outbox.ChannelID),
		slog.String(FieldPostID, telemetry.PostIDLogValue(outbox.Kind, outbox.ContentID, outbox.Payload)),
		slog.String(FieldContentID, strings.TrimSpace(outbox.ContentID)),
		slog.String(FieldAlarmType, string(outbox.Kind.ToAlarmType())),
	)

	return append(baseAttrs, attrs...)
}

func lifecycleProviderFailure(err error) (ytlifecycle.FailureKind, ytlifecycle.Reason, time.Duration) {
	const defaultReason = lifecycleReasonUnknownError

	if errors.Is(err, errDeliverySendOutcomeUnknown) || sendoutcome.Classify(err) == sendoutcome.OutcomeUnknown {
		observeDeliveryOutcomeUnknown(string(defaultReason))

		return ytlifecycle.FailureOutcomeUnknown, lifecycleReasonUnknownError, 0
	}

	if errors.Is(err, errRequestReissued) {
		return ytlifecycle.FailureRetryable, lifecycleReasonTransport, 0
	}

	if errors.Is(err, errRequestGenerationsExhausted) {
		return ytlifecycle.FailurePermanent, ytlifecycle.Reason(errRequestGenerationsExhausted.Error()), 0
	}

	if errors.Is(err, errDeliverySendRequestInvalid) {
		return ytlifecycle.FailurePermanent, lifecycleReasonRequest, 0
	}

	if errors.Is(err, errSendLeaseBudgetExhausted) {
		return ytlifecycle.FailureRetryable, lifecycleReasonLeaseBudget, 0
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
	case errors.Is(err, iris.ErrTransport), errors.Is(err, iris.ErrNotSent):
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

func appendLifecycleTouched(result *deliveryDispatchResult, outboxIDs []int64) {
	if result == nil || len(outboxIDs) == 0 {
		return
	}

	result.mu.Lock()

	result.TouchedOutboxIDs = append(result.TouchedOutboxIDs, outboxIDs...)
	result.mu.Unlock()
}
