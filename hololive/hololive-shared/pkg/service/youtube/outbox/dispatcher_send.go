// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package outbox

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	json "github.com/park285/llm-kakao-bots/shared-go/pkg/json"
	"golang.org/x/sync/errgroup"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/logschema"
)

type deliveryDispatchResult struct {
	successDeliveryIDs []int64
	touchedOutboxIDs   []int64
	successClaimTokens []deliveryClaimToken
	failedDeliveries   int
	failureBuckets     map[string][]int64
}

type deliverySendRequest struct {
	roomID     string
	message    string
	dedupeKeys []string
}

const (
	communityShortsDeliveryPath        = "youtube_outbox_dispatcher"
	deliveryDedupeKeyLogField          = logschema.FieldDedupeKey
	deliveryAttemptStartedLogMessage   = logschema.CommunityShortsDeliveryAttemptMessage
	deliveryAttemptStartedAtLogField   = logschema.FieldAttemptStartedAt
	deliveryResultLogMessage           = logschema.CommunityShortsDeliveryResultMessage
	deliveryAuditLogMessage            = logschema.CommunityShortsDeliveryAuditMessage
	deliveryAuditPostIDLogField        = logschema.FieldPostID
	deliveryAuditContentIDLogField     = logschema.FieldContentID
	deliveryAuditAlarmTypeLogField     = logschema.FieldAlarmType
	deliveryAuditSentAtLogField        = logschema.FieldSentAt
	deliveryAuditSendResultLogField    = logschema.FieldSendResult
	deliveryAuditFailureReasonLogField = logschema.FieldFailureReason
	deliveryAuditModeLogField          = logschema.FieldDeliveryMode
	deliveryAuditPathLogField          = logschema.FieldDeliveryPath
)

var ErrDeliveryDedupeKeyRequired = errors.New("delivery dedupe key is required")

// deliveryGroup: dispatch 시점 동일 room+channel+kind delivery row 그룹
type deliveryGroup struct {
	roomID    string
	channelID string
	kind      domain.OutboxKind
	rows      []domain.YouTubeNotificationDelivery
	outboxes  []domain.YouTubeNotificationOutbox
}

// groupDeliveryRows: delivery row를 room+channel+kind 기준으로 그룹핑한다.
// milestone kind는 그룹핑 제외 (항상 단건 그룹).
// outbox를 찾을 수 없는 row는 orphanRows로 반환한다.
func groupDeliveryRows(
	rows []domain.YouTubeNotificationDelivery,
	outboxByID map[int64]domain.YouTubeNotificationOutbox,
) (groups []deliveryGroup, orphanRows []domain.YouTubeNotificationDelivery) {
	if len(rows) == 0 {
		return nil, nil
	}

	index := make(map[string]int)
	groups = make([]deliveryGroup, 0, len(rows))

	for i := range rows {
		row := rows[i]
		outbox, ok := outboxByID[row.OutboxID]
		if !ok {
			orphanRows = append(orphanRows, row)
			continue
		}

		if outbox.Kind == domain.OutboxKindMilestone {
			groups = append(groups, deliveryGroup{
				roomID:    row.RoomID,
				channelID: outbox.ChannelID,
				kind:      outbox.Kind,
				rows:      []domain.YouTubeNotificationDelivery{row},
				outboxes:  []domain.YouTubeNotificationOutbox{outbox},
			})
			continue
		}

		key := row.RoomID + "|" + outbox.ChannelID + "|" + string(outbox.Kind)
		if idx, exists := index[key]; exists {
			groups[idx].rows = append(groups[idx].rows, row)
			groups[idx].outboxes = append(groups[idx].outboxes, outbox)
			continue
		}

		index[key] = len(groups)
		groups = append(groups, deliveryGroup{
			roomID:    row.RoomID,
			channelID: outbox.ChannelID,
			kind:      outbox.Kind,
			rows:      []domain.YouTubeNotificationDelivery{row},
			outboxes:  []domain.YouTubeNotificationOutbox{outbox},
		})
	}

	return groups, orphanRows
}

// validateOutboxPayload: outbox payload가 정상 파싱 가능한지 검증한다.
// grouped format 전에 호출하여 빈 Title/URL 방지.
func validateOutboxPayload(item domain.YouTubeNotificationOutbox) bool {
	switch item.Kind {
	case domain.OutboxKindNewVideo, domain.OutboxKindNewShort:
		var p videoPayload
		return json.Unmarshal([]byte(item.Payload), &p) == nil
	case domain.OutboxKindCommunityPost:
		var p communityPayload
		return json.Unmarshal([]byte(item.Payload), &p) == nil
	default:
		return true
	}
}

func buildDeliverySendRequest(roomID, message string, outboxes []domain.YouTubeNotificationOutbox) (deliverySendRequest, error) {
	if strings.TrimSpace(roomID) == "" {
		return deliverySendRequest{}, fmt.Errorf("build delivery send request: room id is empty")
	}
	if strings.TrimSpace(message) == "" {
		return deliverySendRequest{}, fmt.Errorf("build delivery send request: message is empty")
	}
	if len(outboxes) == 0 {
		return deliverySendRequest{}, fmt.Errorf("build delivery send request: outboxes are empty")
	}

	dedupeKeys := make([]string, 0, len(outboxes))
	for i := range outboxes {
		dedupeKey, err := outboxes[i].DedupeKey()
		if err != nil {
			return deliverySendRequest{}, fmt.Errorf("%w: build delivery send request: outbox[%d] dedupe key: %w", ErrDeliveryDedupeKeyRequired, i, err)
		}
		dedupeKeys = append(dedupeKeys, dedupeKey)
	}

	req := deliverySendRequest{
		roomID:     roomID,
		message:    message,
		dedupeKeys: dedupeKeys,
	}
	if err := validateDeliverySendRequest(req); err != nil {
		return deliverySendRequest{}, fmt.Errorf("build delivery send request: %w", err)
	}

	return req, nil
}

func dedupeKeyLogAttr(dedupeKeys []string) slog.Attr {
	cloned := make([]string, 0, len(dedupeKeys))
	for i := range dedupeKeys {
		cloned = append(cloned, strings.TrimSpace(dedupeKeys[i]))
	}
	return slog.Any(deliveryDedupeKeyLogField, cloned)
}

func dedupeKeyLogValue(outbox domain.YouTubeNotificationOutbox) string {
	dedupeKey, err := outbox.DedupeKey()
	if err == nil {
		return dedupeKey
	}

	return fmt.Sprintf("invalid:%s:%s",
		strings.TrimSpace(string(outbox.Kind)),
		strings.TrimSpace(outbox.ContentID),
	)
}

func dedupeKeyLogAttrForOutboxes(outboxes []domain.YouTubeNotificationOutbox) slog.Attr {
	dedupeKeys := make([]string, 0, len(outboxes))
	for i := range outboxes {
		dedupeKeys = append(dedupeKeys, dedupeKeyLogValue(outboxes[i]))
	}
	return dedupeKeyLogAttr(dedupeKeys)
}

func normalizeCommunityShortsDeliveryPath(path string) string {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return communityShortsDeliveryPath
	}
	return trimmed
}

func deliveryAttemptOrdinal(row domain.YouTubeNotificationDelivery) int {
	attemptOrdinal := row.AttemptCount + 1
	if attemptOrdinal <= 0 {
		return 1
	}
	return attemptOrdinal
}

func deliveryAttemptStartedAt(row domain.YouTubeNotificationDelivery) *time.Time {
	if row.LockedAt == nil || row.LockedAt.IsZero() {
		return nil
	}

	startedAt := row.LockedAt.UTC()
	return &startedAt
}

func isCommunityShortsDeliveryAuditKind(kind domain.OutboxKind) bool {
	switch kind {
	case domain.OutboxKindNewShort, domain.OutboxKindCommunityPost:
		return true
	default:
		return false
	}
}

func (d *Dispatcher) logCommunityShortsDeliveryAttemptStarted(
	rows []domain.YouTubeNotificationDelivery,
	outboxes []domain.YouTubeNotificationOutbox,
	attemptStartedAt time.Time,
	deliveryMode string,
) {
	limit := min(len(outboxes), len(rows))
	if limit == 0 {
		return
	}

	attemptStartedAt = attemptStartedAt.UTC()
	deliveryPath := normalizeCommunityShortsDeliveryPath(communityShortsDeliveryPath)
	limitedRows := rows[:limit]
	limitedOutboxes := outboxes[:limit]

	for i := range limitedOutboxes {
		outbox := limitedOutboxes[i]
		if !isCommunityShortsDeliveryAuditKind(outbox.Kind) {
			continue
		}

		d.logger.Info(deliveryAttemptStartedLogMessage,
			slog.Int64(logschema.FieldDeliveryID, limitedRows[i].ID),
			slog.Int64(logschema.FieldOutboxID, outbox.ID),
			slog.String(logschema.FieldRoomID, limitedRows[i].RoomID),
			slog.String(logschema.FieldChannelID, outbox.ChannelID),
			slog.String(deliveryAuditPostIDLogField, resolveTelemetryPostID(outbox.Kind, outbox.ContentID, outbox.Payload)),
			slog.String(deliveryAuditContentIDLogField, strings.TrimSpace(outbox.ContentID)),
			slog.String(deliveryAuditAlarmTypeLogField, string(outbox.Kind.ToAlarmType())),
			slog.Time(deliveryAttemptStartedAtLogField, attemptStartedAt),
			slog.Int(logschema.FieldAttemptOrdinal, deliveryAttemptOrdinal(limitedRows[i])),
			slog.String(deliveryAuditPathLogField, deliveryPath),
			slog.String(deliveryAuditModeLogField, deliveryMode),
			slog.String(deliveryDedupeKeyLogField, dedupeKeyLogValue(outbox)),
		)
	}
}

func (d *Dispatcher) logCommunityShortsDeliveryResult(
	rows []domain.YouTubeNotificationDelivery,
	outboxes []domain.YouTubeNotificationOutbox,
	sentAt time.Time,
	deliveryMode string,
	sendResult string,
	failureReason string,
) {
	if d == nil || d.logger == nil {
		return
	}

	limit := min(len(outboxes), len(rows))
	if limit == 0 {
		return
	}

	sentAt = sentAt.UTC()
	deliveryPath := normalizeCommunityShortsDeliveryPath(communityShortsDeliveryPath)
	summary := summarizeCommunityShortsDeliveryResult(rows[:limit], outboxes[:limit])
	if summary.alarmCount == 0 {
		return
	}

	roomCount := len(summary.uniqueRooms)
	successfulAlarmCount, failedAlarmCount, successfulRoomCount, failedRoomCount := deliveryResultCounts(sendResult, summary.alarmCount, roomCount)

	attrs := []any{
		slog.String(logschema.FieldChannelID, summary.channelID),
		slog.String(deliveryAuditAlarmTypeLogField, string(summary.alarmType)),
		slog.Time(deliveryAuditSentAtLogField, sentAt),
		slog.String(deliveryAuditSendResultLogField, sendResult),
		slog.String(deliveryAuditPathLogField, deliveryPath),
		slog.String(deliveryAuditModeLogField, deliveryMode),
		slog.Int(logschema.FieldTargetAlarmCount, summary.alarmCount),
		slog.Int(logschema.FieldSuccessfulAlarmCount, successfulAlarmCount),
		slog.Int(logschema.FieldFailedAlarmCount, failedAlarmCount),
		slog.Int(logschema.FieldTargetRoomCount, roomCount),
		slog.Int(logschema.FieldSuccessfulRoomCount, successfulRoomCount),
		slog.Int(logschema.FieldFailedRoomCount, failedRoomCount),
	}
	if roomCount == 1 {
		for roomID := range summary.uniqueRooms {
			attrs = append(attrs, slog.String(logschema.FieldRoomID, roomID))
			break
		}
	}
	if trimmedReason := strings.TrimSpace(failureReason); trimmedReason != "" {
		attrs = append(attrs, slog.String(deliveryAuditFailureReasonLogField, truncateString(trimmedReason, 100)))
	}

	d.logger.Info(deliveryResultLogMessage, attrs...)
}

type communityShortsDeliveryResultSummary struct {
	alarmCount  int
	channelID   string
	alarmType   domain.AlarmType
	uniqueRooms map[string]struct{}
}

func summarizeCommunityShortsDeliveryResult(
	rows []domain.YouTubeNotificationDelivery,
	outboxes []domain.YouTubeNotificationOutbox,
) communityShortsDeliveryResultSummary {
	summary := communityShortsDeliveryResultSummary{
		uniqueRooms: make(map[string]struct{}, len(rows)),
	}

	for i := range outboxes {
		outbox := outboxes[i]
		if !isCommunityShortsDeliveryAuditKind(outbox.Kind) {
			continue
		}

		summary.alarmCount++
		if summary.channelID == "" {
			summary.channelID = strings.TrimSpace(outbox.ChannelID)
		}
		if summary.alarmType == "" {
			summary.alarmType = outbox.Kind.ToAlarmType()
		}

		roomID := strings.TrimSpace(rows[i].RoomID)
		if roomID != "" {
			summary.uniqueRooms[roomID] = struct{}{}
		}
	}

	return summary
}

func deliveryResultCounts(sendResult string, alarmCount, roomCount int) (int, int, int, int) {
	switch strings.TrimSpace(sendResult) {
	case "success":
		return alarmCount, 0, roomCount, 0
	case "failure":
		return 0, alarmCount, 0, roomCount
	default:
		return 0, 0, 0, 0
	}
}

func (d *Dispatcher) logCommunityShortsDeliveryAudit(
	ctx context.Context,
	rows []domain.YouTubeNotificationDelivery,
	outboxes []domain.YouTubeNotificationOutbox,
	sentAt time.Time,
	deliveryMode string,
	sendResult string,
	failureReason string,
	sendErr error,
) {
	limit := min(len(outboxes), len(rows))
	if limit == 0 {
		return
	}

	sentAt = sentAt.UTC()
	deliveryPath := normalizeCommunityShortsDeliveryPath(communityShortsDeliveryPath)
	events := make([]domain.YouTubeNotificationDeliveryTelemetry, 0, limit)
	limitedRows := rows[:limit]
	limitedOutboxes := outboxes[:limit]
	for i := range limitedOutboxes {
		outbox := limitedOutboxes[i]
		if !isCommunityShortsDeliveryAuditKind(outbox.Kind) {
			continue
		}

		attemptFinishedAt := sentAt.UTC()
		events = append(events, domain.YouTubeNotificationDeliveryTelemetry{
			DeliveryID:        limitedRows[i].ID,
			AttemptOrdinal:    deliveryAttemptOrdinal(limitedRows[i]),
			OutboxID:          outbox.ID,
			ChannelID:         outbox.ChannelID,
			ContentID:         strings.TrimSpace(outbox.ContentID),
			PostID:            resolveTelemetryPostID(outbox.Kind, outbox.ContentID, outbox.Payload),
			RoomID:            limitedRows[i].RoomID,
			AlarmType:         outbox.Kind.ToAlarmType(),
			DedupeKey:         dedupeKeyLogValue(outbox),
			DeliveryPath:      deliveryPath,
			DeliveryMode:      deliveryMode,
			SendResult:        sendResult,
			FailureReason:     truncateString(strings.TrimSpace(failureReason), 100),
			AttemptStartedAt:  deliveryAttemptStartedAt(limitedRows[i]),
			AttemptFinishedAt: &attemptFinishedAt,
			EventAt:           attemptFinishedAt,
			NextAttemptAt:     time.Now().UTC(),
		})
	}
	if len(events) == 0 {
		return
	}

	preparedEvents := events
	if d.telemetry != nil {
		prepared, err := d.telemetry.prepareRows(ctx, events)
		if err != nil {
			d.logger.Warn("Failed to enrich persistent delivery audit",
				slog.Int("events", len(events)),
				slog.Any("error", err))
		} else {
			preparedEvents = prepared
			enqueueErr := d.telemetry.enqueuePrepared(ctx, preparedEvents)
			if enqueueErr == nil {
				if err := d.telemetry.PersistPostLatencyClassificationsByOutboxIDs(ctx, collectTelemetryOutboxIDs(preparedEvents)); err != nil {
					d.logger.Warn("Failed to persist post latency classifications",
						slog.Int("events", len(preparedEvents)),
						slog.Any("error", err))
				}
				return
			}
			d.logger.Warn("Failed to enqueue persistent delivery audit",
				slog.Int("events", len(preparedEvents)),
				slog.Any("error", enqueueErr))
		}
	}

	fallbackClassificationsByOutboxID, err := d.loadDeliveryTelemetryLatencyClassifications(ctx, preparedEvents)
	if err != nil {
		d.logger.Warn("Failed to load fallback delivery telemetry latency classifications",
			slog.Int("events", len(preparedEvents)),
			slog.Any("error", err))
	}

	for i := range preparedEvents {
		attrs := buildDeliveryAuditLogAttrsWithClassification(preparedEvents[i], fallbackClassificationsByOutboxID[preparedEvents[i].OutboxID])
		attrs = append(attrs, slog.String(logschema.FieldTelemetrySource, "direct_fallback"))
		if sendErr != nil {
			attrs = append(attrs, slog.String("error", sendErr.Error()))
		}

		d.logger.Info(deliveryAuditLogMessage, attrs...)
	}
}

func validateDeliverySendRequest(req deliverySendRequest) error {
	if strings.TrimSpace(req.roomID) == "" {
		return fmt.Errorf("send delivery message: room id is empty")
	}
	if strings.TrimSpace(req.message) == "" {
		return fmt.Errorf("send delivery message: message is empty")
	}
	if len(req.dedupeKeys) == 0 {
		return fmt.Errorf("%w: send delivery message: dedupe keys are empty", ErrDeliveryDedupeKeyRequired)
	}
	for i := range req.dedupeKeys {
		if strings.TrimSpace(req.dedupeKeys[i]) == "" {
			return fmt.Errorf("%w: send delivery message: dedupe key at index %d is empty", ErrDeliveryDedupeKeyRequired, i)
		}
	}
	return nil
}

func deliveryFailureReason(err error) string {
	if errors.Is(err, ErrDeliveryDedupeKeyRequired) {
		return "dedupe key"
	}
	return "send message"
}

func (d *Dispatcher) dispatchDeliveryRows(
	ctx context.Context,
	rows []domain.YouTubeNotificationDelivery,
	outboxByID map[int64]domain.YouTubeNotificationOutbox,
) deliveryDispatchResult {
	result := deliveryDispatchResult{
		successDeliveryIDs: make([]int64, 0, len(rows)),
		touchedOutboxIDs:   make([]int64, 0, len(rows)),
		successClaimTokens: make([]deliveryClaimToken, 0, len(rows)),
		failureBuckets:     make(map[string][]int64),
	}
	var mu sync.Mutex
	reuseCache := newDeliveryClaimReuseCache(len(rows))

	formattedMessages, formatFailures := d.preFormatMessages(ctx, outboxByID)

	groups, orphanRows := groupDeliveryRows(rows, outboxByID)

	// orphan row 처리
	for i := range orphanRows {
		d.recordDeliveryFailure(&result, &mu, "outbox row not found", orphanRows[i].ID, orphanRows[i].OutboxID)
	}

	eg, egCtx := errgroup.WithContext(ctx)
	eg.SetLimit(d.deliveryParallelism())

	for i := range groups {
		group := groups[i]
		eg.Go(func() error {
			d.dispatchGroup(egCtx, group, formattedMessages, formatFailures, reuseCache, &result, &mu)
			return nil
		})
	}
	_ = eg.Wait()

	return result
}

func (d *Dispatcher) dispatchGroup(
	ctx context.Context,
	group deliveryGroup,
	formattedMessages map[int64]string,
	formatFailures map[int64]bool,
	reuseCache *deliveryClaimReuseCache,
	result *deliveryDispatchResult,
	mu *sync.Mutex,
) {
	groupOutboxByID := make(map[int64]domain.YouTubeNotificationOutbox, len(group.outboxes))
	for i := range group.outboxes {
		groupOutboxByID[group.outboxes[i].ID] = group.outboxes[i]
	}

	// 단건 그룹: 기존 개별 dispatch 경로
	if len(group.rows) == 1 {
		d.dispatchDeliveryRow(ctx, group.rows[0], groupOutboxByID, formattedMessages, formatFailures, reuseCache, result, mu)
		return
	}

	validRows, validOutboxes, invalidRows := partitionGroupedDeliveries(group)
	d.dispatchRowsIndividually(ctx, invalidRows, groupOutboxByID, formattedMessages, formatFailures, reuseCache, result, mu)

	// 검증 후 1건 이하 -> 개별 dispatch
	if len(validRows) <= 1 {
		d.dispatchRowsIndividually(ctx, validRows, groupOutboxByID, formattedMessages, formatFailures, reuseCache, result, mu)
		return
	}

	claimSelection := d.selectClaimedDeliveries(ctx, validRows, validOutboxes, reuseCache)
	d.applyClaimSelection(result, mu, claimSelection)
	validRows = claimSelection.sendRows
	validOutboxes = claimSelection.sendOutboxes
	if len(validRows) == 0 {
		return
	}
	if len(validRows) == 1 {
		d.dispatchClaimedDeliveryRow(ctx, validRows[0], validOutboxes[0], formattedMessages, formatFailures, claimSelection.claimTokens, result, mu)
		return
	}

	message, formatted := d.formatGroupedMessage(ctx, group, validRows, validOutboxes)
	if !formatted {
		d.dispatchClaimedRowsIndividually(ctx, validRows, validOutboxes, formattedMessages, formatFailures, claimSelection.rowClaimTokens, result, mu)
		return
	}

	d.dispatchGroupedClaimedRows(ctx, group, validRows, validOutboxes, message, claimSelection.claimTokens, result, mu)
}

func (d *Dispatcher) dispatchDeliveryRow(
	ctx context.Context,
	row domain.YouTubeNotificationDelivery,
	outboxByID map[int64]domain.YouTubeNotificationOutbox,
	formattedMessages map[int64]string,
	formatFailures map[int64]bool,
	reuseCache *deliveryClaimReuseCache,
	result *deliveryDispatchResult,
	mu *sync.Mutex,
) {
	outbox, ok := outboxByID[row.OutboxID]
	if !ok {
		d.recordDeliveryFailure(result, mu, "outbox row not found", row.ID, row.OutboxID)
		return
	}

	claimSelection := d.selectClaimedDeliveries(ctx, []domain.YouTubeNotificationDelivery{row}, []domain.YouTubeNotificationOutbox{outbox}, reuseCache)
	d.applyClaimSelection(result, mu, claimSelection)
	if len(claimSelection.sendRows) == 0 {
		return
	}

	d.dispatchClaimedDeliveryRow(ctx, claimSelection.sendRows[0], claimSelection.sendOutboxes[0], formattedMessages, formatFailures, claimSelection.claimTokens, result, mu)
}

func (d *Dispatcher) dispatchClaimedDeliveryRow(
	ctx context.Context,
	row domain.YouTubeNotificationDelivery,
	outbox domain.YouTubeNotificationOutbox,
	formattedMessages map[int64]string,
	formatFailures map[int64]bool,
	claimTokens []deliveryClaimToken,
	result *deliveryDispatchResult,
	mu *sync.Mutex,
) {
	rows, outboxes := singleDeliveryBatch(row, outbox)
	if formatFailures[row.OutboxID] {
		d.releaseDeliveryClaimsWithWarning(ctx, claimTokens, "Failed to release per-room delivery claims after format error",
			slog.Int64("delivery_id", row.ID),
			slog.Int64("outbox_id", row.OutboxID),
		)
		failedAt := time.Now()
		d.logCommunityShortsDeliveryAudit(ctx, rows, outboxes, failedAt, "per_room", "failure", "format message", nil)
		d.logCommunityShortsDeliveryResult(rows, outboxes, failedAt, "per_room", "failure", "format message")
		d.recordDeliveryFailure(result, mu, "format message", row.ID, row.OutboxID)
		return
	}

	message, ok := formattedMessages[row.OutboxID]
	if !ok {
		d.releaseDeliveryClaimsWithWarning(ctx, claimTokens, "Failed to release per-room delivery claims after missing preformatted message",
			slog.Int64("delivery_id", row.ID),
			slog.Int64("outbox_id", row.OutboxID),
		)
		d.recordDeliveryFailure(result, mu, "outbox row not found", row.ID, row.OutboxID)
		return
	}

	sendReq, err := buildDeliverySendRequest(row.RoomID, message, []domain.YouTubeNotificationOutbox{outbox})
	if err != nil {
		d.releaseDeliveryClaimsWithWarning(ctx, claimTokens, "Failed to release per-room delivery claims after request build error",
			slog.Int64("delivery_id", row.ID),
			slog.Int64("outbox_id", row.OutboxID),
		)
		failedAt := time.Now()
		d.logger.Warn("Failed to build per-room delivery request",
			slog.Int64("delivery_id", row.ID),
			slog.Int64("outbox_id", row.OutboxID),
			slog.String("room_id", row.RoomID),
			dedupeKeyLogAttrForOutboxes([]domain.YouTubeNotificationOutbox{outbox}),
			slog.Any("error", err))
		d.logCommunityShortsDeliveryAudit(ctx, rows, outboxes, failedAt, "per_room", "failure", "dedupe key", err)
		d.logCommunityShortsDeliveryResult(rows, outboxes, failedAt, "per_room", "failure", "dedupe key")
		d.recordDeliveryFailure(result, mu, "dedupe key", row.ID, row.OutboxID)
		return
	}

	attemptStartedAt := time.Now().UTC()
	d.logCommunityShortsDeliveryAttemptStarted(rows, outboxes, attemptStartedAt, "per_room")
	if sendErr := d.sendDeliveryMessage(ctx, sendReq); sendErr != nil {
		d.releaseDeliveryClaimsWithWarning(ctx, claimTokens, "Failed to release per-room delivery claims after send failure",
			slog.Int64("delivery_id", row.ID),
			slog.Int64("outbox_id", row.OutboxID),
		)
		failedAt := time.Now()
		d.logger.Warn("Failed to send per-room delivery",
			slog.Int64("delivery_id", row.ID),
			slog.Int64("outbox_id", row.OutboxID),
			slog.String("room_id", row.RoomID),
			dedupeKeyLogAttr(sendReq.dedupeKeys),
			slog.Any("error", sendErr))
		d.logCommunityShortsDeliveryAudit(ctx, rows, outboxes, failedAt, "per_room", "failure", deliveryFailureReason(sendErr), sendErr)
		d.logCommunityShortsDeliveryResult(rows, outboxes, failedAt, "per_room", "failure", deliveryFailureReason(sendErr))
		d.recordDeliveryFailure(result, mu, deliveryFailureReason(sendErr), row.ID, row.OutboxID)
		return
	}

	sentAt := time.Now()
	d.logger.Info("Sent per-room delivery",
		slog.Int64("delivery_id", row.ID),
		slog.Int64("outbox_id", row.OutboxID),
		slog.String("room_id", row.RoomID),
		dedupeKeyLogAttr(sendReq.dedupeKeys))
	d.logCommunityShortsDeliveryAudit(ctx, rows, outboxes, sentAt, "per_room", "success", "", nil)
	d.logCommunityShortsDeliveryResult(rows, outboxes, sentAt, "per_room", "success", "")

	mu.Lock()
	result.successDeliveryIDs = append(result.successDeliveryIDs, row.ID)
	result.touchedOutboxIDs = append(result.touchedOutboxIDs, row.OutboxID)
	result.successClaimTokens = append(result.successClaimTokens, claimTokens...)
	mu.Unlock()
}

func (d *Dispatcher) recordDeliveryFailure(
	result *deliveryDispatchResult,
	mu *sync.Mutex,
	reason string,
	deliveryID, outboxID int64,
) {
	mu.Lock()
	result.failedDeliveries++
	result.failureBuckets[reason] = append(result.failureBuckets[reason], deliveryID)
	result.touchedOutboxIDs = append(result.touchedOutboxIDs, outboxID)
	mu.Unlock()
}

func partitionGroupedDeliveries(
	group deliveryGroup,
) ([]domain.YouTubeNotificationDelivery, []domain.YouTubeNotificationOutbox, []domain.YouTubeNotificationDelivery) {
	var validRows []domain.YouTubeNotificationDelivery
	var validOutboxes []domain.YouTubeNotificationOutbox
	var invalidRows []domain.YouTubeNotificationDelivery

	for i := range group.outboxes {
		if validateOutboxPayload(group.outboxes[i]) {
			validOutboxes = append(validOutboxes, group.outboxes[i])
			validRows = append(validRows, group.rows[i])
			continue
		}

		invalidRows = append(invalidRows, group.rows[i])
	}

	return validRows, validOutboxes, invalidRows
}

func (d *Dispatcher) dispatchRowsIndividually(
	ctx context.Context,
	rows []domain.YouTubeNotificationDelivery,
	outboxByID map[int64]domain.YouTubeNotificationOutbox,
	formattedMessages map[int64]string,
	formatFailures map[int64]bool,
	reuseCache *deliveryClaimReuseCache,
	result *deliveryDispatchResult,
	mu *sync.Mutex,
) {
	for i := range rows {
		d.dispatchDeliveryRow(ctx, rows[i], outboxByID, formattedMessages, formatFailures, reuseCache, result, mu)
	}
}

func (d *Dispatcher) formatGroupedMessage(
	ctx context.Context,
	group deliveryGroup,
	validRows []domain.YouTubeNotificationDelivery,
	validOutboxes []domain.YouTubeNotificationOutbox,
) (string, bool) {
	memberName, err := d.formatter.getMemberName(ctx, group.channelID)
	if err != nil || memberName == "" {
		memberName = "VTuber"
	}

	message, err := d.formatter.formatGroupedMessage(ctx, memberName, group.channelID, group.kind, validOutboxes)
	if err != nil {
		d.logger.Warn("Grouped format failed, falling back to individual dispatch",
			slog.String("room_id", group.roomID),
			slog.String("channel_id", group.channelID),
			slog.String("kind", string(group.kind)),
			slog.Int("count", len(validRows)),
			slog.Any("error", err))
		return "", false
	}

	return message, true
}

func (d *Dispatcher) dispatchClaimedRowsIndividually(
	ctx context.Context,
	rows []domain.YouTubeNotificationDelivery,
	outboxes []domain.YouTubeNotificationOutbox,
	formattedMessages map[int64]string,
	formatFailures map[int64]bool,
	rowClaimTokens [][]deliveryClaimToken,
	result *deliveryDispatchResult,
	mu *sync.Mutex,
) {
	for i := range rows {
		var claims []deliveryClaimToken
		if i < len(rowClaimTokens) {
			claims = rowClaimTokens[i]
		}
		d.dispatchClaimedDeliveryRow(ctx, rows[i], outboxes[i], formattedMessages, formatFailures, claims, result, mu)
	}
}

func (d *Dispatcher) dispatchGroupedClaimedRows(
	ctx context.Context,
	group deliveryGroup,
	validRows []domain.YouTubeNotificationDelivery,
	validOutboxes []domain.YouTubeNotificationOutbox,
	message string,
	claimTokens []deliveryClaimToken,
	result *deliveryDispatchResult,
	mu *sync.Mutex,
) {
	sendReq, err := buildDeliverySendRequest(group.roomID, message, validOutboxes)
	if err != nil {
		d.releaseDeliveryClaimsWithWarning(ctx, claimTokens, "Failed to release grouped delivery claims after request build error",
			slog.String("room_id", group.roomID),
			slog.String("channel_id", group.channelID),
		)
		failedAt := time.Now()
		d.logger.Warn("Failed to build grouped delivery request",
			slog.String("room_id", group.roomID),
			slog.String("channel_id", group.channelID),
			slog.String("kind", string(group.kind)),
			slog.Int("count", len(validOutboxes)),
			dedupeKeyLogAttrForOutboxes(validOutboxes),
			slog.Any("error", err))
		d.logCommunityShortsDeliveryAudit(ctx, validRows, validOutboxes, failedAt, "grouped", "failure", "dedupe key", err)
		d.logCommunityShortsDeliveryResult(validRows, validOutboxes, failedAt, "grouped", "failure", "dedupe key")
		for i := range validRows {
			d.recordDeliveryFailure(result, mu, "dedupe key", validRows[i].ID, validRows[i].OutboxID)
		}
		return
	}

	attemptStartedAt := time.Now().UTC()
	d.logCommunityShortsDeliveryAttemptStarted(validRows, validOutboxes, attemptStartedAt, "grouped")
	if sendErr := d.sendDeliveryMessage(ctx, sendReq); sendErr != nil {
		d.releaseDeliveryClaimsWithWarning(ctx, claimTokens, "Failed to release grouped delivery claims after send failure",
			slog.String("room_id", group.roomID),
			slog.String("channel_id", group.channelID),
		)
		failedAt := time.Now()
		d.logger.Warn("Failed to send grouped delivery",
			slog.String("room_id", group.roomID),
			slog.String("kind", string(group.kind)),
			slog.Int("count", len(validRows)),
			dedupeKeyLogAttr(sendReq.dedupeKeys),
			slog.Any("error", sendErr))
		d.logCommunityShortsDeliveryAudit(ctx, validRows, validOutboxes, failedAt, "grouped", "failure", deliveryFailureReason(sendErr), sendErr)
		d.logCommunityShortsDeliveryResult(validRows, validOutboxes, failedAt, "grouped", "failure", deliveryFailureReason(sendErr))
		for i := range validRows {
			d.recordDeliveryFailure(result, mu, deliveryFailureReason(sendErr), validRows[i].ID, validRows[i].OutboxID)
		}
		return
	}

	sentAt := time.Now()
	d.logger.Info("Sent grouped delivery",
		slog.String("room_id", group.roomID),
		slog.String("channel_id", group.channelID),
		slog.String("kind", string(group.kind)),
		slog.Int("count", len(validRows)),
		dedupeKeyLogAttr(sendReq.dedupeKeys))
	d.logCommunityShortsDeliveryAudit(ctx, validRows, validOutboxes, sentAt, "grouped", "success", "", nil)
	d.logCommunityShortsDeliveryResult(validRows, validOutboxes, sentAt, "grouped", "success", "")

	mu.Lock()
	for i := range validRows {
		result.successDeliveryIDs = append(result.successDeliveryIDs, validRows[i].ID)
		result.touchedOutboxIDs = append(result.touchedOutboxIDs, validRows[i].OutboxID)
	}
	result.successClaimTokens = append(result.successClaimTokens, claimTokens...)
	mu.Unlock()
}

func singleDeliveryBatch(
	row domain.YouTubeNotificationDelivery,
	outbox domain.YouTubeNotificationOutbox,
) ([]domain.YouTubeNotificationDelivery, []domain.YouTubeNotificationOutbox) {
	return []domain.YouTubeNotificationDelivery{row}, []domain.YouTubeNotificationOutbox{outbox}
}

func (d *Dispatcher) releaseDeliveryClaimsWithWarning(
	ctx context.Context,
	claims []deliveryClaimToken,
	message string,
	attrs ...any,
) {
	if releaseErr := d.releaseDeliveryClaims(ctx, claims); releaseErr != nil && d.logger != nil {
		d.logger.Warn(message, append(attrs, slog.Any("error", releaseErr))...)
	}
}

// preFormatMessages: outbox_id별로 메시지를 1회 포맷하여 캐싱
func (d *Dispatcher) preFormatMessages(ctx context.Context, outboxByID map[int64]domain.YouTubeNotificationOutbox) (messages map[int64]string, failures map[int64]bool) {
	messages = make(map[int64]string, len(outboxByID))
	failures = make(map[int64]bool)
	for id := range outboxByID {
		item := outboxByID[id]
		msg, err := d.formatter.formatMessage(ctx, item)
		if err != nil {
			d.logger.Warn("Failed to pre-format outbox message",
				slog.Int64("outbox_id", id),
				slog.Any("error", err))
			failures[id] = true
			continue
		}
		messages[id] = msg
	}
	return
}

func (d *Dispatcher) sendDeliveryMessage(ctx context.Context, req deliverySendRequest) error {
	if err := validateDeliverySendRequest(req); err != nil {
		return err
	}

	if err := d.sender.SendMessage(ctx, req.roomID, req.message); err != nil {
		return fmt.Errorf("send delivery message: %w", err)
	}

	return nil
}

func (d *Dispatcher) deliveryParallelism() int {
	if d.cfg.DeliveryParallelism > 0 {
		return d.cfg.DeliveryParallelism
	}
	return DefaultConfig().DeliveryParallelism
}
