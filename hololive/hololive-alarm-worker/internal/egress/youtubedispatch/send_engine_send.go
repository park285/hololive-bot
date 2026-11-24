package youtubedispatch

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/park285/shared-go/v2/pkg/panicguard"
	"golang.org/x/sync/errgroup"

	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/lifecycle"
	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/store"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/panicscope"
	"github.com/kapu/hololive-shared/pkg/privacylog"
	messagedelivery "github.com/kapu/hololive-shared/pkg/service/delivery"
)

func (d *SendEngine) dispatchDeliveryRows(
	ctx context.Context,
	rows []domain.YouTubeNotificationDelivery,
	outboxByID map[int64]domain.YouTubeNotificationOutbox,
) DispatchResult {
	result := newDeliveryDispatchResult(len(rows))

	formattedMessages, formatFailures := d.preFormatMessages(ctx, rowOutboxes(rows, outboxByID))

	frozenGroups, remaining, err := d.frozenDeliveryGroups(ctx, rows, outboxByID)
	if err != nil {
		d.logger.Error("Failed to load persisted delivery requests", slog.Any("error", err))

		return result.DispatchResult
	}

	groups, orphanRows := groupDeliveryRows(remaining, outboxByID)

	groups = append(frozenGroups, groups...)

	for i := range orphanRows {
		d.recordDeliveryFailure(&result, orphanRows[i].OutboxID)
	}

	eg, egCtx := errgroup.WithContext(ctx)
	eg.SetLimit(d.config.DeliveryParallelism)

	for i := range groups {
		group := &groups[i]

		eg.Go(func() error {
			return panicguard.RunE(d.logger, panicscope.BackgroundTask, "youtube-outbox-delivery-group", func() error {
				d.dispatchGroup(egCtx, group, formattedMessages, formatFailures, &result)

				return nil
			})
		})
	}

	if err := eg.Wait(); err != nil {
		d.logger.Warn("Delivery dispatch worker failed", slog.Any("error", err))
	}

	return result.DispatchResult
}

func rowOutboxes(rows []domain.YouTubeNotificationDelivery, outboxByID map[int64]domain.YouTubeNotificationOutbox) map[int64]domain.YouTubeNotificationOutbox {
	out := make(map[int64]domain.YouTubeNotificationOutbox, len(rows))

	for i := range rows {
		if outbox, ok := outboxByID[rows[i].OutboxID]; ok {
			out[rows[i].OutboxID] = outbox
		}
	}

	return out
}

func (d *SendEngine) dispatchGroup(
	ctx context.Context,
	group *deliveryGroup,
	formattedMessages map[int64]messagedelivery.MessageVariants,
	formatFailures map[int64]bool,
	result *deliveryDispatchResult,
) {
	if group.frozen != nil {
		d.dispatchFrozenGroup(ctx, group, formattedMessages, formatFailures, result)

		return
	}

	groupOutboxByID := make(map[int64]domain.YouTubeNotificationOutbox, len(group.outboxes))
	for i := range group.outboxes {
		groupOutboxByID[group.outboxes[i].ID] = group.outboxes[i]
	}

	if len(group.rows) == 1 {
		d.dispatchDeliveryRow(ctx, &group.rows[0], groupOutboxByID, formattedMessages, formatFailures, result)

		return
	}

	validRows, validOutboxes, invalidRows := partitionGroupedDeliveries(group)
	d.dispatchRowsIndividually(ctx, invalidRows, groupOutboxByID, formattedMessages, formatFailures, result)

	if len(validRows) <= 1 {
		d.dispatchRowsIndividually(ctx, validRows, groupOutboxByID, formattedMessages, formatFailures, result)

		return
	}

	members := d.selectDeliverableMembers(ctx, validRows, validOutboxes, result)
	if len(members) == 0 {
		return
	}

	attempt := newDeliveryAttempt(members)
	d.dispatchClaimedGroup(ctx, group, &attempt, formattedMessages, formatFailures, result)
}

func (d *SendEngine) dispatchClaimedGroup(
	ctx context.Context,
	group *deliveryGroup,
	attempt *deliveryAttempt,
	formattedMessages map[int64]messagedelivery.MessageVariants,
	formatFailures map[int64]bool,
	result *deliveryDispatchResult,
) {
	if len(attempt.members) == 1 {
		d.dispatchClaimedDeliveryRow(ctx, attempt, formattedMessages, formatFailures, result)

		return
	}

	message, err := d.formatGroupedMessage(ctx, group, attempt.outboxes)
	if err != nil {
		if d.applyPreparedLifecycleFailure(ctx, attempt.rows, attempt.outboxes, lifecycle.FailureRetryable, lifecycleReasonFormat, result) {
			d.recordGroupedFormatFailure(group, attempt, err, result)
		}

		return
	}

	d.dispatchGroupedClaimedRows(ctx, group, attempt, message, formattedMessages, formatFailures, result)
}

func (d *SendEngine) dispatchDeliveryRow(
	ctx context.Context,
	row *domain.YouTubeNotificationDelivery,
	outboxByID map[int64]domain.YouTubeNotificationOutbox,
	formattedMessages map[int64]messagedelivery.MessageVariants,
	formatFailures map[int64]bool,
	result *deliveryDispatchResult,
) {
	outbox, ok := outboxByID[row.OutboxID]
	if !ok {
		d.recordDeliveryFailure(result, row.OutboxID)

		return
	}

	members := d.selectDeliverableMembers(ctx, []domain.YouTubeNotificationDelivery{*row}, []domain.YouTubeNotificationOutbox{outbox}, result)
	if len(members) == 0 {
		return
	}

	attempt := newDeliveryAttempt(members)
	d.dispatchClaimedDeliveryRow(ctx, &attempt, formattedMessages, formatFailures, result)
}

func (d *SendEngine) dispatchClaimedDeliveryRow(
	ctx context.Context,
	attempt *deliveryAttempt,
	formattedMessages map[int64]messagedelivery.MessageVariants,
	formatFailures map[int64]bool,
	result *deliveryDispatchResult,
) {
	request, prepared := d.preparePerRoomSend(ctx, attempt, formattedMessages, formatFailures, result)
	if !prepared {
		return
	}

	attempt.request = request
	if !d.freezeDeliveryRequest(ctx, attempt) {
		return
	}

	if !d.beginLifecycleOperation(ctx, attempt, result) {
		return
	}

	d.logCommunityShortsDeliveryAttemptStarted(attempt.rows, attempt.outboxes, time.Now().UTC(), "per_room")

	if sendErr := d.sendFrozenDelivery(ctx, attempt); sendErr != nil {
		d.handlePerRoomSendFailure(ctx, attempt, sendErr, result)

		return
	}

	if d.completeLifecycleSent(ctx, attempt, result) {
		d.recordPerRoomSuccess(ctx, attempt, result)
	}
}

func (d *SendEngine) preparePerRoomSend(
	ctx context.Context,
	attempt *deliveryAttempt,
	formattedMessages map[int64]messagedelivery.MessageVariants,
	formatFailures map[int64]bool,
	result *deliveryDispatchResult,
) (deliverySendRequest, bool) {
	row := &attempt.rows[0]
	rows, outboxes := attempt.rows, attempt.outboxes

	if formatFailures[row.OutboxID] {
		if d.applyPreparedLifecycleFailure(ctx, rows, outboxes, lifecycle.FailureRetryable, lifecycleReasonFormat, result) {
			d.recordPerRoomFormatFailure(attempt, result)
		}

		return deliverySendRequest{}, false
	}

	message, ok := formattedMessages[row.OutboxID]
	if !ok {
		if d.applyPreparedLifecycleFailure(ctx, rows, outboxes, lifecycle.FailureRetryable, lifecycleReasonMessage, result) {
			d.recordPerRoomMissingMessage(attempt, result)
		}

		return deliverySendRequest{}, false
	}

	sendReq, err := buildDeliverySendRequest(row.RoomID, message, outboxes)
	if err != nil {
		if d.applyPreparedLifecycleFailure(ctx, rows, outboxes, lifecycle.FailurePermanent, lifecycleReasonRequest, result) {
			d.recordPerRoomRequestBuildFailure(attempt, err, result)
		}

		return deliverySendRequest{}, false
	}

	return sendReq, true
}

func (d *SendEngine) handlePerRoomSendFailure(
	ctx context.Context,
	attempt *deliveryAttempt,
	sendErr error,
	result *deliveryDispatchResult,
) {
	row := &attempt.rows[0]
	if !d.applyPerRoomLifecycleFailure(ctx, attempt.operation, row, attempt.request, sendErr, result) {
		return
	}

	if errors.Is(sendErr, errDeliverySendOutcomeUnknown) {
		d.recordPerRoomSendOutcomeUnknown(row, attempt.request, sendErr)

		return
	}

	d.recordPerRoomSendFailure(attempt, sendErr, result)
}

func (d *SendEngine) applyPerRoomLifecycleFailure(
	ctx context.Context,
	operation store.StartedOperation,
	row *domain.YouTubeNotificationDelivery,
	sendReq deliverySendRequest,
	sendErr error,
	result *deliveryDispatchResult,
) bool {
	if errors.Is(sendErr, errRequestReissued) {
		appendLifecycleTouched(result, operation.TouchedOutboxIDs())

		return true
	}

	kind, reason, retryAfter := lifecycleProviderFailure(sendErr)
	if kind == lifecycle.FailureOutcomeUnknown {
		d.recordPerRoomSendOutcomeUnknown(row, sendReq, sendErr)

		return false
	}

	return d.applyStartedLifecycleFailure(ctx, operation, kind, reason, retryAfter, result)
}

func (d *SendEngine) dispatchGroupedClaimedRows(
	ctx context.Context,
	group *deliveryGroup,
	attempt *deliveryAttempt,
	message messagedelivery.MessageVariants,
	formattedMessages map[int64]messagedelivery.MessageVariants,
	formatFailures map[int64]bool,
	result *deliveryDispatchResult,
) {
	request, prepared := d.prepareGroupedSend(ctx, group, attempt, message, result)
	if !prepared {
		return
	}

	attempt.request = request
	if !d.freezeDeliveryRequest(ctx, attempt) {
		return
	}

	if !d.beginLifecycleOperation(ctx, attempt, result) {
		return
	}

	d.logCommunityShortsDeliveryAttemptStarted(attempt.rows, attempt.outboxes, time.Now().UTC(), "grouped")

	if sendErr := d.sendFrozenDelivery(ctx, attempt); sendErr != nil {
		d.handleGroupedSendFailure(ctx, group, attempt, formattedMessages, formatFailures, sendErr, result)

		return
	}

	if d.completeLifecycleSent(ctx, attempt, result) {
		d.recordGroupedSuccess(group, attempt, result)
	}
}

func (d *SendEngine) prepareGroupedSend(
	ctx context.Context,
	group *deliveryGroup,
	attempt *deliveryAttempt,
	message messagedelivery.MessageVariants,
	result *deliveryDispatchResult,
) (deliverySendRequest, bool) {
	rows, outboxes := attempt.rows, attempt.outboxes

	sendReq, err := buildDeliverySendRequest(group.roomID, message, outboxes)
	if err == nil {
		return sendReq, true
	}

	if d.applyPreparedLifecycleFailure(ctx, rows, outboxes, lifecycle.FailurePermanent, lifecycleReasonRequest, result) {
		d.recordGroupedRequestBuildFailure(group, attempt, err, result)
	}

	return deliverySendRequest{}, false
}

func (d *SendEngine) handleGroupedSendFailure(
	ctx context.Context,
	group *deliveryGroup,
	attempt *deliveryAttempt,
	formattedMessages map[int64]messagedelivery.MessageVariants,
	formatFailures map[int64]bool,
	sendErr error,
	result *deliveryDispatchResult,
) {
	operation, rows := attempt.operation, attempt.rows
	sendReq := attempt.request

	if errors.Is(sendErr, errRequestReissued) {
		appendLifecycleTouched(result, operation.TouchedOutboxIDs())
		d.recordGroupedSendFailure(group, attempt, sendErr, result)

		return
	}

	kind, reason, retryAfter := lifecycleProviderFailure(sendErr)
	if kind == lifecycle.FailureOutcomeUnknown {
		d.recordGroupedSendOutcomeUnknown(group, rows, sendReq, sendErr)

		return
	}

	if kind == lifecycle.FailurePermanent && shouldFallbackGroupedSend(sendErr) {
		d.logGroupedSendFallback(group, rows, sendReq, sendErr)
		d.dispatchStartedRowsIndividually(ctx, attempt, formattedMessages, formatFailures, result)

		return
	}

	if !d.applyStartedLifecycleFailure(ctx, operation, kind, reason, retryAfter, result) {
		return
	}

	d.recordGroupedSendFailure(group, attempt, sendErr, result)
}

func (d *SendEngine) logGroupedSendFallback(
	group *deliveryGroup,
	rows []domain.YouTubeNotificationDelivery,
	sendReq deliverySendRequest,
	sendErr error,
) {
	d.logger.Warn("Grouped delivery send failed, falling back to version-fenced individual deliveries",
		privacylog.RoomIDAttr(group.roomID),
		slog.String("channel_id", group.channelID),
		slog.String("kind", string(group.kind)),
		slog.Int("count", len(rows)),
		dedupeKeyLogAttr(sendReq.dedupeKeys),
		slog.Any("error", sendErr))
}

var errSendLeaseBudgetExhausted = errors.New("send lease budget exhausted")

func (d *SendEngine) checkSendLeaseBudget(startedAt time.Time) error {
	remaining := d.config.LockTimeout - time.Since(startedAt)
	if required := d.config.sendLeaseBudget(); remaining < required {
		return fmt.Errorf("check send lease budget: remaining %s is below %s: %w", remaining, required, errSendLeaseBudgetExhausted)
	}

	return nil
}

func (d *SendEngine) dispatchStartedRowsIndividually(
	ctx context.Context,
	attempt *deliveryAttempt,
	formattedMessages map[int64]messagedelivery.MessageVariants,
	formatFailures map[int64]bool,
	result *deliveryDispatchResult,
) {
	requests, err := d.prepareFallbackRequests(ctx, attempt, formattedMessages, formatFailures)
	if err != nil {
		observeGroupedSendFallback(groupedSendFallbackResultPrepareFailed)
		d.logger.Warn("Failed to prepare grouped fallback", slog.Any("error", err))

		return
	}

	frozen, err := d.transition.FreezeFallbackRequests(ctx, attempt.operation, requests)
	if err != nil {
		observeGroupedSendFallback(groupedSendFallbackResultFreezeFailed)
		d.logger.Error("Failed to freeze grouped fallback", slog.Any("error", err))

		return
	}

	observeGroupedSendFallback(groupedSendFallbackResultStarted)

	byID := make(map[int64]store.FrozenRequest, len(frozen))
	for i := range frozen {
		byID[frozen[i].MemberIDs[0]] = frozen[i]
	}

	for i := range attempt.members {
		if ctx.Err() != nil {
			return
		}

		singleton := attempt.singleton(i)

		rowOperation, err := attempt.operation.ForOwner(singleton.rows[0].ID)
		if err != nil {
			return
		}

		req, err := deliveryRequestFromFrozen(byID[singleton.rows[0].ID])
		if err != nil {
			return
		}

		singleton.operation = rowOperation
		singleton.request = req

		if budgetErr := d.checkSendLeaseBudget(attempt.sendingStartedAt); budgetErr != nil {
			d.handlePerRoomSendFailure(ctx, &singleton, budgetErr, result)

			continue
		}

		if sendErr := d.sendFrozenDelivery(ctx, &singleton); sendErr != nil {
			d.handlePerRoomSendFailure(ctx, &singleton, sendErr, result)

			continue
		}

		if d.completeLifecycleSent(ctx, &singleton, result) {
			d.recordPerRoomSuccess(ctx, &singleton, result)
		}
	}
}
