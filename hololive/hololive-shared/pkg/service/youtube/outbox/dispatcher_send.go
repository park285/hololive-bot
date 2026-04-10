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
	"log/slog"
	"sync"

	json "github.com/park285/llm-kakao-bots/shared-go/pkg/json"
	"golang.org/x/sync/errgroup"

	"github.com/kapu/hololive-shared/pkg/domain"
)

type deliveryDispatchResult struct {
	successDeliveryIDs []int64
	touchedOutboxIDs   []int64
	failedDeliveries   int
	failureBuckets     map[string][]int64
}

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

func (d *Dispatcher) dispatchDeliveryRows(
	ctx context.Context,
	rows []domain.YouTubeNotificationDelivery,
	outboxByID map[int64]domain.YouTubeNotificationOutbox,
) deliveryDispatchResult {
	result := deliveryDispatchResult{
		successDeliveryIDs: make([]int64, 0, len(rows)),
		touchedOutboxIDs:   make([]int64, 0, len(rows)),
		failureBuckets:     make(map[string][]int64),
	}
	var mu sync.Mutex

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
			d.dispatchGroup(egCtx, group, formattedMessages, formatFailures, &result, &mu)
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
	result *deliveryDispatchResult,
	mu *sync.Mutex,
) {
	// 단건 그룹: 기존 개별 dispatch 경로
	if len(group.rows) == 1 {
		d.dispatchDeliveryRow(ctx, group.rows[0], formattedMessages, formatFailures, result, mu)
		return
	}

	// 복수건: payload 검증 -> 유효 항목만 그룹 포맷
	var validOutboxes []domain.YouTubeNotificationOutbox
	var validRows []domain.YouTubeNotificationDelivery
	var invalidRows []domain.YouTubeNotificationDelivery

	for i := range group.outboxes {
		if validateOutboxPayload(group.outboxes[i]) {
			validOutboxes = append(validOutboxes, group.outboxes[i])
			validRows = append(validRows, group.rows[i])
		} else {
			invalidRows = append(invalidRows, group.rows[i])
		}
	}

	// payload 검증 실패 항목 -> 개별 dispatch
	for i := range invalidRows {
		d.dispatchDeliveryRow(ctx, invalidRows[i], formattedMessages, formatFailures, result, mu)
	}

	// 검증 후 1건 이하 -> 개별 dispatch
	if len(validRows) <= 1 {
		for i := range validRows {
			d.dispatchDeliveryRow(ctx, validRows[i], formattedMessages, formatFailures, result, mu)
		}
		return
	}

	// 그룹 포맷 시도
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
		for i := range validRows {
			d.dispatchDeliveryRow(ctx, validRows[i], formattedMessages, formatFailures, result, mu)
		}
		return
	}

	// 그룹 메시지 전송
	if sendErr := d.sender.SendMessage(ctx, group.roomID, message); sendErr != nil {
		d.logger.Warn("Failed to send grouped delivery",
			slog.String("room_id", group.roomID),
			slog.String("kind", string(group.kind)),
			slog.Int("count", len(validRows)),
			slog.Any("error", sendErr))
		for i := range validRows {
			d.recordDeliveryFailure(result, mu, "send message", validRows[i].ID, validRows[i].OutboxID)
		}
		return
	}

	// 성공: 그룹 내 모든 delivery ID 성공 처리
	mu.Lock()
	for i := range validRows {
		result.successDeliveryIDs = append(result.successDeliveryIDs, validRows[i].ID)
		result.touchedOutboxIDs = append(result.touchedOutboxIDs, validRows[i].OutboxID)
	}
	mu.Unlock()
}

func (d *Dispatcher) dispatchDeliveryRow(
	ctx context.Context,
	row domain.YouTubeNotificationDelivery,
	formattedMessages map[int64]string,
	formatFailures map[int64]bool,
	result *deliveryDispatchResult,
	mu *sync.Mutex,
) {
	if formatFailures[row.OutboxID] {
		d.recordDeliveryFailure(result, mu, "format message", row.ID, row.OutboxID)
		return
	}
	message, ok := formattedMessages[row.OutboxID]
	if !ok {
		d.recordDeliveryFailure(result, mu, "outbox row not found", row.ID, row.OutboxID)
		return
	}

	if sendErr := d.sender.SendMessage(ctx, row.RoomID, message); sendErr != nil {
		d.logger.Warn("Failed to send per-room delivery",
			slog.Int64("delivery_id", row.ID),
			slog.Int64("outbox_id", row.OutboxID),
			slog.String("room_id", row.RoomID),
			slog.Any("error", sendErr))
		d.recordDeliveryFailure(result, mu, "send message", row.ID, row.OutboxID)
		return
	}

	mu.Lock()
	result.successDeliveryIDs = append(result.successDeliveryIDs, row.ID)
	result.touchedOutboxIDs = append(result.touchedOutboxIDs, row.OutboxID)
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

func (d *Dispatcher) deliveryParallelism() int {
	if d.cfg.DeliveryParallelism > 0 {
		return d.cfg.DeliveryParallelism
	}
	return DefaultConfig().DeliveryParallelism
}
