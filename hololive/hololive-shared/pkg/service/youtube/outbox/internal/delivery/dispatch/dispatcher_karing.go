package dispatch

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/internal/delivery/dispatchstate"
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
	if err := d.sendYouTubeOutboxKaring(ctx, sender, roomID, &payload); err != nil {
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
		return d.wrapKaringTimeoutError(sendCtx, "wait for youtube outbox karing send slot", err)
	}
	defer d.karingMu.Unlock()

	if err := sender.SendYouTubeOutboxKaring(sendCtx, roomID, payload); err != nil {
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
		return fmt.Errorf("%s timed out after %s: %w", action, d.config.DeliverySendTimeout, errors.Join(errDeliverySendTimeout, err))
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
		return deliverySendRequest{}, err
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
