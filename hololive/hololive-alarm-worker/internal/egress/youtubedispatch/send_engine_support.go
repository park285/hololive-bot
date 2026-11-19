package youtubedispatch

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/privacylog"
	messagedelivery "github.com/kapu/hololive-shared/pkg/service/delivery"
)

func partitionGroupedDeliveries(
	group *deliveryGroup,
) ([]domain.YouTubeNotificationDelivery, []domain.YouTubeNotificationOutbox, []domain.YouTubeNotificationDelivery) {
	validRows := make([]domain.YouTubeNotificationDelivery, 0, len(group.rows))
	validOutboxes := make([]domain.YouTubeNotificationOutbox, 0, len(group.outboxes))
	invalidRows := make([]domain.YouTubeNotificationDelivery, 0)

	for i := range group.outboxes {
		if validateOutboxPayload(&group.outboxes[i]) {
			validOutboxes = append(validOutboxes, group.outboxes[i])
			validRows = append(validRows, group.rows[i])

			continue
		}

		invalidRows = append(invalidRows, group.rows[i])
	}

	return validRows, validOutboxes, invalidRows
}

func (d *SendEngine) dispatchRowsIndividually(
	ctx context.Context,
	rows []domain.YouTubeNotificationDelivery,
	outboxByID map[int64]domain.YouTubeNotificationOutbox,
	formattedMessages map[int64]messagedelivery.MessageVariants,
	formatFailures map[int64]bool,
	result *deliveryDispatchResult,
) {
	for i := range rows {
		d.dispatchDeliveryRow(ctx, &rows[i], outboxByID, formattedMessages, formatFailures, result)
	}
}

func (d *SendEngine) formatGroupedMessage(
	ctx context.Context,
	group *deliveryGroup,
	validOutboxes []domain.YouTubeNotificationOutbox,
) (messagedelivery.MessageVariants, error) {
	if group == nil {
		return messagedelivery.MessageVariants{}, errors.New("format grouped message: delivery group is missing")
	}

	memberName, err := d.formatter.GetMemberName(ctx, group.channelID)
	if err != nil {
		return messagedelivery.MessageVariants{}, fmt.Errorf("format grouped message: %w", err)
	}

	message, err := d.formatter.FormatGroupedMessage(ctx, d.formatter.DisplayMemberName(memberName), group.channelID, group.kind, validOutboxes)
	if err != nil {
		return messagedelivery.MessageVariants{}, fmt.Errorf("format grouped message: %w", err)
	}

	return message, nil
}

func (d *SendEngine) preFormatMessages(ctx context.Context, outboxByID map[int64]domain.YouTubeNotificationOutbox) (messages map[int64]messagedelivery.MessageVariants, failures map[int64]bool) {
	messages = make(map[int64]messagedelivery.MessageVariants, len(outboxByID))
	failures = make(map[int64]bool)

	if len(outboxByID) == 0 {
		return messages, failures
	}

	ids := make([]int64, 0, len(outboxByID))
	items := make([]domain.YouTubeNotificationOutbox, 0, len(outboxByID))

	for id := range outboxByID {
		ids = append(ids, id)
		items = append(items, outboxByID[id])
	}

	results, err := d.formatter.FormatMessages(ctx, items)
	if err != nil {
		d.logger.Warn("Failed to pre-format outbox messages",
			slog.Int("outbox_count", len(ids)),
			slog.Any("error", fmt.Errorf("format messages: %w", err)))

		for _, id := range ids {
			failures[id] = true
		}

		return messages, failures
	}

	for i, id := range ids {
		if results[i].Err != nil {
			d.logger.Warn("Failed to pre-format outbox message",
				slog.Int64("outbox_id", id),
				slog.Any("error", fmt.Errorf("format message: %w", results[i].Err)))

			failures[id] = true

			continue
		}

		messages[id] = results[i].Message
	}

	return messages, failures
}

var errDeliverySendRequestInvalid = errors.New("delivery send request is invalid")

func (d *SendEngine) sendDeliveryMessage(ctx context.Context, req deliverySendRequest) (irisRequestID string, sendErr error) {
	if err := validateDeliverySendRequest(req); err != nil {
		return "", fmt.Errorf("validate delivery send request: %w: %w", errDeliverySendRequestInvalid, err)
	}

	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("send delivery message before request: %w", err)
	}

	sendCtx := ctx
	cancel := func() {}

	if d.config.DeliverySendTimeout > 0 {
		sendCtx, cancel = context.WithTimeoutCause(ctx, d.config.DeliverySendTimeout, errDeliverySendTimeout)
	}

	defer cancel()

	if req.frozen == nil || (req.frozen.Route != "text" && req.frozen.Route != "richtext") {
		return "", fmt.Errorf("send frozen delivery: stored text or richtext request required: %w", errDeliverySendRequestInvalid)
	}

	if req.clientRequestID == "" {
		return "", fmt.Errorf("send frozen delivery: client request id required: %w", errDeliverySendRequestInvalid)
	}

	finish := d.beginProviderAttempt()
	completed := false

	defer func() { finish(sendErr, completed) }()

	irisRequestID, err := d.sender.SendPreparedMessage(sendCtx, req.roomID, req.message, req.frozen.Route, req.clientRequestID)

	completed = true

	if err != nil {
		return "", d.wrapDeliverySendError(sendCtx, err)
	}

	return irisRequestID, nil
}

func (d *SendEngine) wrapDeliverySendError(sendCtx context.Context, err error) error {
	if errors.Is(context.Cause(sendCtx), errDeliverySendTimeout) {
		return fmt.Errorf("send delivery message timed out after %s: %w", d.config.DeliverySendTimeout, errors.Join(errDeliverySendOutcomeUnknown, errDeliverySendTimeout, err))
	}

	if deliverySendOutcomeUnknown(err) {
		return fmt.Errorf("send delivery message: %w", errors.Join(errDeliverySendOutcomeUnknown, err))
	}

	return fmt.Errorf("send delivery message: %w", err)
}

func (d *SendEngine) recordPerRoomSendOutcomeUnknown(
	row *domain.YouTubeNotificationDelivery,
	sendReq deliverySendRequest,
	sendErr error,
) {
	d.logger.Warn("Per-room delivery send outcome unknown, holding row for stale sending quarantine",
		slog.Int64("delivery_id", row.ID),
		slog.Int64("outbox_id", row.OutboxID),
		privacylog.RoomIDAttr(row.RoomID),
		dedupeKeyLogAttr(sendReq.dedupeKeys),
		slog.Any("error", sendErr))
}

func (d *SendEngine) recordGroupedSendOutcomeUnknown(
	group *deliveryGroup,
	validRows []domain.YouTubeNotificationDelivery,
	sendReq deliverySendRequest,
	sendErr error,
) {
	roomID, channelID, kind := groupedDeliveryFields(group)
	d.logger.Warn("Grouped delivery send outcome unknown, holding rows for stale sending quarantine",
		privacylog.RoomIDAttr(roomID),
		slog.String("channel_id", channelID),
		slog.String("kind", string(kind)),
		slog.Int("count", len(validRows)),
		slog.Group("delivery_ids", slog.Int("count", len(validRows))),
		slog.Group("outbox_ids", slog.Int("delivery_count", len(validRows))),
		dedupeKeyLogAttr(sendReq.dedupeKeys),
		slog.Any("error", sendErr))
}
