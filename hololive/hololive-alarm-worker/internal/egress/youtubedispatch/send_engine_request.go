package youtubedispatch

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"

	"github.com/park285/iris-client-go/v4/iris"

	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/store"
	"github.com/kapu/hololive-shared/pkg/domain"
)

var (
	errRequestGenerationsExhausted = errors.New("client request id generations exhausted")
	errRequestReissued             = errors.New("client request id reissued after pre-handoff failure")
)

func (d *SendEngine) freezeDeliveryRequest(ctx context.Context, attempt *deliveryAttempt) bool {
	candidate, err := d.prepareFrozenRequest(ctx, attempt.request)
	if err != nil {
		d.logger.Error("Failed to prepare final request", slog.Any("error", err))

		return false
	}

	frozen, err := d.transition.FreezeRequest(ctx, attempt.rows, candidate)
	if err != nil {
		d.logger.Error("Failed to freeze delivery request; preserving preparation", slog.Any("error", err))

		return false
	}

	request, err := deliveryRequestFromFrozen(frozen)
	if err != nil {
		d.logger.Error("Invalid frozen delivery request", slog.Any("error", err))

		return false
	}

	attempt.request = request

	return true
}

func (d *SendEngine) prepareFrozenRequest(ctx context.Context, request deliverySendRequest) (store.FrozenRequest, error) {
	body, route, err := d.sender.PrepareMessageRequest(ctx, request.roomID, request.message)
	if err != nil {
		return store.FrozenRequest{}, fmt.Errorf("prepare frozen request: %w", err)
	}

	return store.FrozenRequest{BaseID: deliveryClientRequestID(request.roomID, request.dedupeKeys), RoomID: request.roomID, Message: body, Route: route, DedupeKeys: request.dedupeKeys}, nil
}

func deliveryRequestFromFrozen(frozen store.FrozenRequest) (deliverySendRequest, error) {
	id, err := frozen.ClientRequestID()
	if err != nil {
		return deliverySendRequest{}, fmt.Errorf("restore frozen delivery request: %w", err)
	}

	return deliverySendRequest{roomID: frozen.RoomID, message: frozen.Message, dedupeKeys: frozen.DedupeKeys, clientRequestID: id, frozen: &frozen}, nil
}

// 확정 pre-handoff 실패도 한 번의 delivery attempt를 소비한다. 새 세대는 다음
// 정규 retry에서 보내므로 기존 backoff·최대 시도·신선도 예산을 우회하지 않는다. 성공하면 202의 Iris requestId를
// attempt에 남겨 CompleteSent가 handoff admission으로 기록하게 한다.
func (d *SendEngine) sendFrozenDelivery(ctx context.Context, attempt *deliveryAttempt) error {
	req, operation := attempt.request, attempt.operation

	irisRequestID, err := d.sendDeliveryMessage(ctx, req)
	if err == nil {
		attempt.irisRequestID = irisRequestID

		return nil
	}

	if errors.Is(err, errDeliverySendOutcomeUnknown) || !iris.IsPreHandoffClientRequestIDConflict(err) {
		return err
	}

	if req.frozen == nil || req.frozen.Generation >= iris.ReplyReissueMaxGenerations || operation.AttemptCount()+1 >= d.config.MaxRetries {
		return fmt.Errorf("send frozen delivery: %w", errors.Join(errRequestGenerationsExhausted, err))
	}

	stateCtx, cancel := deliveryStateContext(ctx)
	defer cancel()

	if _, advanceErr := d.transition.AdvanceRequestGeneration(stateCtx, operation, *req.frozen); advanceErr != nil {
		return fmt.Errorf("send frozen delivery: save generation: %w", errors.Join(errDeliverySendOutcomeUnknown, advanceErr))
	}

	return fmt.Errorf("send frozen delivery: %w", errors.Join(errRequestReissued, err))
}

func (d *SendEngine) frozenDeliveryGroups(ctx context.Context, rows []domain.YouTubeNotificationDelivery, outboxByID map[int64]domain.YouTubeNotificationOutbox) ([]deliveryGroup, []domain.YouTubeNotificationDelivery, error) {
	frozen, err := d.transition.LoadFrozenRequests(ctx, collectDeliveryIDs(rows))
	if err != nil {
		return nil, nil, fmt.Errorf("group frozen deliveries: %w", err)
	}

	byID := make(map[int64]domain.YouTubeNotificationDelivery, len(rows))
	for i := range rows {
		row := &rows[i]

		byID[row.ID] = *row
	}

	var groups []deliveryGroup

	for i := range frozen {
		request := &frozen[i]
		group := deliveryGroup{roomID: request.RoomID, frozen: request}

		for _, id := range request.MemberIDs {
			row, present := byID[id]
			if !present {
				continue
			}

			group.rows = append(group.rows, row)
			group.outboxes = append(group.outboxes, outboxByID[row.OutboxID])

			delete(byID, id)
		}

		if len(group.rows) > 0 {
			group.channelID = group.outboxes[0].ChannelID
			group.kind = group.outboxes[0].Kind
			groups = append(groups, group)
		}
	}

	remaining := make([]domain.YouTubeNotificationDelivery, 0, len(byID))

	for i := range rows {
		row := &rows[i]
		if _, ok := byID[row.ID]; ok {
			remaining = append(remaining, *row)
		}
	}

	return groups, remaining, nil
}

func (d *SendEngine) dispatchFrozenGroup(ctx context.Context, group *deliveryGroup, formattedMessages map[int64]string, formatFailures map[int64]bool, result *deliveryDispatchResult) {
	if len(group.rows) != len(group.frozen.MemberIDs) {
		d.deferIncompleteFrozenGroup(ctx, group.rows)

		return
	}

	attempt := newDeliveryAttempt(d.selectDeliverableMembers(ctx, group.rows, group.outboxes, store.DeliveryModeGrouped, result))
	selected := collectDeliveryIDs(attempt.rows)
	slices.Sort(selected)

	expected := slices.Clone(group.frozen.MemberIDs)
	slices.Sort(expected)

	if !slices.Equal(selected, expected) {
		d.deferIncompleteFrozenGroup(ctx, attempt.rows)

		return
	}

	req, err := deliveryRequestFromFrozen(*group.frozen)
	if err != nil {
		d.logger.Error("Invalid persisted provider request", slog.Any("error", err))

		return
	}

	attempt.request = req

	operation, begun := d.beginLifecycleOperation(ctx, attempt.rows, attempt.outboxes, result)

	if !begun {
		return
	}

	attempt.operation = operation

	mode := store.DeliveryModeGrouped

	if len(attempt.members) == 1 {
		mode = store.DeliveryModePerRoom
	}

	if sendErr := d.sendFrozenDelivery(ctx, &attempt); sendErr != nil {
		if mode == store.DeliveryModePerRoom {
			d.handlePerRoomSendFailure(ctx, &attempt, sendErr, result)
		} else {
			d.handleGroupedSendFailure(ctx, group, &attempt, formattedMessages, formatFailures, sendErr, result)
		}

		return
	}

	if !d.completeLifecycleSent(ctx, &attempt, mode, result) {
		return
	}

	if len(attempt.members) == 1 {
		d.recordPerRoomSuccess(&attempt, result)
	} else {
		d.recordGroupedSuccess(group, &attempt, result)
	}
}

func (d *SendEngine) deferIncompleteFrozenGroup(ctx context.Context, rows []domain.YouTubeNotificationDelivery) {
	for i := range rows {
		row := &rows[i]
		result, err := d.transition.DeferFollower(ctx, store.DeferCommand{Delivery: *row, Delay: d.config.RetryBackoff})
		observeLifecycleApply("defer_follower", result, err, 1)

		if err != nil || result.Outcome != store.ApplyApplied {
			d.logger.Warn("Failed to defer incomplete frozen request",
				slog.Int64("delivery_id", row.ID),
				slog.String("outcome", result.Outcome.String()),
				slog.Any("error", err))
		}
	}
}

func (d *SendEngine) prepareFallbackRequests(ctx context.Context, attempt *deliveryAttempt, messages map[int64]string, failures map[int64]bool) ([]store.FrozenRequest, error) {
	requests := make([]store.FrozenRequest, 0, len(attempt.members))
	for i := range attempt.members {
		member := &attempt.members[i]
		if failures[member.row.OutboxID] {
			return nil, errors.New("prepare fallback requests: message unavailable")
		}

		req, err := buildDeliverySendRequest(member.row.RoomID, messages[member.row.OutboxID], attempt.outboxes[i:i+1])
		if err != nil {
			return nil, fmt.Errorf("prepare fallback requests: build request: %w", err)
		}

		frozen, err := d.prepareFrozenRequest(ctx, req)
		if err != nil {
			return nil, fmt.Errorf("prepare fallback requests: final request: %w", err)
		}

		frozen.MemberIDs = []int64{member.row.ID}
		requests = append(requests, frozen)
	}

	return requests, nil
}
