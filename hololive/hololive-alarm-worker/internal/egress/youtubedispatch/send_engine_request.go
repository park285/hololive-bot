package youtubedispatch

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/park285/iris-client-go/v3/iris"

	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/store"
	"github.com/kapu/hololive-alarm-worker/internal/service/youtube/outbox/dispatchstate"
	"github.com/kapu/hololive-shared/pkg/domain"
)

var (
	errRequestGenerationsExhausted = errors.New("client request id generations exhausted")
	errRequestReissued             = errors.New("client request id reissued after pre-handoff failure")
)

func (d *SendEngine) freezeDeliveryRequest(ctx context.Context, rows []domain.YouTubeNotificationDelivery, request deliverySendRequest) (deliverySendRequest, bool) {
	candidate, err := d.prepareFrozenRequest(ctx, request)
	if err != nil {
		d.logger.Error("Failed to prepare final request", slog.Any("error", err))

		return deliverySendRequest{}, false
	}

	frozen, err := d.transition.FreezeRequest(ctx, rows, candidate)
	if err != nil {
		d.logger.Error("Failed to freeze delivery request; preserving preparation", slog.Any("error", err))

		return deliverySendRequest{}, false
	}

	request, err = deliveryRequestFromFrozen(frozen)
	if err != nil {
		d.logger.Error("Invalid frozen delivery request", slog.Any("error", err))

		return deliverySendRequest{}, false
	}

	return request, true
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
// 정규 retry에서 보내므로 기존 backoff·최대 시도·신선도 예산을 우회하지 않는다.
func (d *SendEngine) sendFrozenDelivery(ctx context.Context, operation store.StartedOperation, req deliverySendRequest) error {
	err := d.sendDeliveryMessage(ctx, req)
	if errors.Is(err, errDeliverySendOutcomeUnknown) || !iris.IsPreHandoffClientRequestIDConflict(err) {
		return err
	}

	if req.frozen == nil || req.frozen.Generation >= iris.ReplyReissueMaxGenerations || operation.AttemptCount()+1 >= d.config.MaxRetries {
		return fmt.Errorf("send frozen delivery: %w", errors.Join(errRequestGenerationsExhausted, err))
	}

	if _, advanceErr := d.transition.AdvanceRequestGeneration(ctx, operation, *req.frozen); advanceErr != nil {
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

func (d *SendEngine) dispatchFrozenGroup(ctx context.Context, group *deliveryGroup, formattedMessages map[int64]string, formatFailures map[int64]bool, reuseCache *claimDecisionCache, result *dispatchstate.DispatchResult, mu *sync.Mutex) {
	if len(group.rows) != len(group.frozen.MemberIDs) {
		d.deferIncompleteFrozenGroup(ctx, group.rows)

		return
	}

	selection := d.claims.selectClaimedDeliveries(ctx, group.rows, group.outboxes, reuseCache)
	d.applyLifecycleClaimSelection(ctx, &selection, store.DeliveryModeGrouped, result, mu)

	selected := collectDeliveryIDs(selection.sendRows)
	slices.Sort(selected)

	expected := slices.Clone(group.frozen.MemberIDs)
	slices.Sort(expected)

	if !slices.Equal(selected, expected) {
		d.deferIncompleteFrozenGroup(ctx, selection.sendRows)

		return
	}

	req, err := deliveryRequestFromFrozen(*group.frozen)
	if err != nil {
		d.logger.Error("Invalid persisted provider request", slog.Any("error", err))

		return
	}

	operation, begun := d.beginLifecycleOperation(ctx, selection.sendRows, selection.sendOutboxes, result, mu)
	if !begun {
		return
	}

	mode := store.DeliveryModeGrouped

	if len(selection.sendRows) == 1 {
		mode = store.DeliveryModePerRoom
	}

	if sendErr := d.sendFrozenDelivery(ctx, operation, req); sendErr != nil {
		if mode == store.DeliveryModePerRoom {
			d.handlePerRoomSendFailure(ctx, operation, &selection.sendRows[0], selection.sendRows, selection.sendOutboxes, req, selection.claimTokens, sendErr, result, mu)
		} else {
			d.handleGroupedSendFailure(ctx, operation, group, selection.sendRows, selection.sendOutboxes, req, formattedMessages, formatFailures, selection.claimTokens, selection.rowClaimTokens, sendErr, result, mu)
		}

		return
	}

	if !d.completeLifecycleSent(ctx, operation, selection.claimTokens, mode, result, mu) {
		return
	}

	if len(selection.sendRows) == 1 {
		d.recordPerRoomSuccess(&selection.sendRows[0], selection.sendRows, selection.sendOutboxes, req, selection.claimTokens, result, mu)
	} else {
		d.recordGroupedSuccess(group, selection.sendRows, selection.sendOutboxes, req, selection.claimTokens, result, mu)
	}
}

func (d *SendEngine) deferIncompleteFrozenGroup(ctx context.Context, rows []domain.YouTubeNotificationDelivery) {
	for i := range rows {
		row := &rows[i]
		if _, err := d.transition.DeferFollower(ctx, store.DeferCommand{Delivery: *row, NextAttemptAt: time.Now().Add(d.config.RetryBackoff)}); err != nil {
			d.logger.Warn("Failed to defer incomplete frozen request", slog.Any("error", err))
		}
	}
}

func (d *SendEngine) prepareFallbackRequests(ctx context.Context, rows []domain.YouTubeNotificationDelivery, outboxes []domain.YouTubeNotificationOutbox, messages map[int64]string, failures map[int64]bool) ([]store.FrozenRequest, error) {
	requests := make([]store.FrozenRequest, 0, len(rows))
	for i := range rows {
		if i >= len(outboxes) || failures[rows[i].OutboxID] {
			return nil, errors.New("prepare fallback requests: message unavailable")
		}

		req, err := buildDeliverySendRequest(rows[i].RoomID, messages[rows[i].OutboxID], []domain.YouTubeNotificationOutbox{outboxes[i]})
		if err != nil {
			return nil, fmt.Errorf("prepare fallback requests: build request: %w", err)
		}

		frozen, err := d.prepareFrozenRequest(ctx, req)
		if err != nil {
			return nil, fmt.Errorf("prepare fallback requests: final request: %w", err)
		}

		frozen.MemberIDs = []int64{rows[i].ID}
		requests = append(requests, frozen)
	}

	return requests, nil
}
