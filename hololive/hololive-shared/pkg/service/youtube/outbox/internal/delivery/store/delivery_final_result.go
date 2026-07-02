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

package store

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/internal/delivery/deliverysql"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/internal/delivery/timeline"
)

type TerminalCommunityShortsOutboxResult struct {
	OutboxID              int64
	Kind                  domain.OutboxKind
	ChannelID             string
	ContentID             string
	Payload               string
	Status                domain.OutboxStatus
	SentAt                *time.Time
	Error                 string
	TargetRoomCount       int
	SuccessfulRoomCount   int
	FailedRoomCount       int
	AggregatedFailReason  string
	LatencyClassification timeline.PostLatencyClassificationResult
}

func (r *DeliveryRepository) LoadTerminalCommunityShortsOutboxResults(ctx context.Context, outboxIDs []int64) ([]TerminalCommunityShortsOutboxResult, error) {
	uniqueIDs := deliverysql.UniqueInt64s(outboxIDs)
	if r == nil || r.db == nil || len(uniqueIDs) == 0 {
		return nil, nil
	}

	postKinds := []domain.OutboxKind{domain.OutboxKindNewShort, domain.OutboxKindCommunityPost}
	terminalStatuses := []domain.OutboxStatus{domain.OutboxStatusSent, domain.OutboxStatusFailed}
	var outboxes []domain.YouTubeNotificationOutbox
	if err := deliverysql.SelectDeliverySQL(ctx, r.db, &outboxes, "load terminal community/shorts outboxes", `
			SELECT id, kind, channel_id, content_id, payload::text AS payload, status, attempt_count, next_attempt_at, created_at, locked_at, sent_at, COALESCE(error, '') AS error
		FROM youtube_notification_outbox
		WHERE `+deliverysql.DeliveryInClause("id", len(uniqueIDs))+`
		  AND `+deliverysql.DeliveryInClause("kind", len(postKinds))+`
		  AND `+deliverysql.DeliveryInClause("status", len(terminalStatuses))+`
		ORDER BY id ASC
	`, deliverysql.AppendDeliveryOutboxStatusArgs(
		deliverysql.AppendDeliveryOutboxKindArgs(
			deliverysql.AppendDeliveryInt64Args(nil, uniqueIDs),
			postKinds...,
		),
		terminalStatuses...,
	)...); err != nil {
		return nil, fmt.Errorf("load terminal community/shorts outboxes: %w", err)
	}
	if len(outboxes) == 0 {
		return nil, nil
	}

	outboxResultIDs := collectOutboxIDs(outboxes)
	var deliveries []domain.YouTubeNotificationDelivery
	if err := deliverysql.SelectDeliverySQL(ctx, r.db, &deliveries, "load terminal community/shorts deliveries", `
		SELECT id, outbox_id, room_id, status, attempt_count, next_attempt_at, created_at, locked_at, sent_at, error
		FROM youtube_notification_delivery
		WHERE `+deliverysql.DeliveryInClause("outbox_id", len(outboxResultIDs))+`
		ORDER BY outbox_id ASC, id ASC
	`, deliverysql.AppendDeliveryInt64Args(nil, outboxResultIDs)...); err != nil {
		return nil, fmt.Errorf("load terminal community/shorts deliveries: %w", err)
	}

	deliveriesByOutbox := make(map[int64][]domain.YouTubeNotificationDelivery, len(outboxResultIDs))
	for i := range deliveries {
		row := deliveries[i]
		deliveriesByOutbox[row.OutboxID] = append(deliveriesByOutbox[row.OutboxID], row)
	}

	results := make([]TerminalCommunityShortsOutboxResult, 0, len(outboxes))
	for i := range outboxes {
		results = append(results, summarizeTerminalCommunityShortsOutbox(&outboxes[i], deliveriesByOutbox[outboxes[i].ID]))
	}

	return results, nil
}

func summarizeTerminalCommunityShortsOutbox(
	outbox *domain.YouTubeNotificationOutbox,
	deliveries []domain.YouTubeNotificationDelivery,
) TerminalCommunityShortsOutboxResult {
	result := TerminalCommunityShortsOutboxResult{
		OutboxID:        outbox.ID,
		Kind:            outbox.Kind,
		ChannelID:       outbox.ChannelID,
		ContentID:       strings.TrimSpace(outbox.ContentID),
		Payload:         outbox.Payload,
		Status:          outbox.Status,
		SentAt:          outbox.SentAt,
		Error:           strings.TrimSpace(outbox.Error),
		TargetRoomCount: len(deliveries),
	}

	result.SuccessfulRoomCount, result.FailedRoomCount, result.AggregatedFailReason = summarizeTerminalCommunityShortsDeliveries(deliveries)
	if result.AggregatedFailReason == "" {
		result.AggregatedFailReason = result.Error
	}

	return result
}

func summarizeTerminalCommunityShortsDeliveries(deliveries []domain.YouTubeNotificationDelivery) (result1, result2 int, result3 string) {
	reasons := make([]string, 0)
	seenReasons := make(map[string]struct{}, len(deliveries))
	successfulRoomCount := 0
	failedRoomCount := 0

	for i := range deliveries {
		row := deliveries[i]
		successIncrement, failureIncrement := terminalCommunityShortsDeliveryStatusCounts(row.Status)
		successfulRoomCount += successIncrement
		failedRoomCount += failureIncrement

		reasons = appendUniqueTerminalCommunityShortsDeliveryReason(reasons, seenReasons, row.Error)
	}

	if len(reasons) > 0 {
		sort.Strings(reasons)
		return successfulRoomCount, failedRoomCount, strings.Join(reasons, " | ")
	}

	return successfulRoomCount, failedRoomCount, ""
}

func terminalCommunityShortsDeliveryStatusCounts(status domain.OutboxStatus) (result1, result2 int) {
	switch status {
	case domain.OutboxStatusSent:
		return 1, 0
	case domain.OutboxStatusFailed, DeliveryStatusQuarantined:
		return 0, 1
	case domain.OutboxStatusPending, DeliveryStatusSending:
		return 0, 0
	default:
		return 0, 0
	}
}

func appendUniqueTerminalCommunityShortsDeliveryReason(reasons []string, seenReasons map[string]struct{}, rawReason string) []string {
	reason := strings.TrimSpace(rawReason)
	if reason == "" {
		return reasons
	}
	if _, exists := seenReasons[reason]; exists {
		return reasons
	}
	seenReasons[reason] = struct{}{}
	return append(reasons, reason)
}

func collectOutboxIDs(items []domain.YouTubeNotificationOutbox) []int64 {
	if len(items) == 0 {
		return nil
	}
	ids := make([]int64, 0, len(items))
	for i := range items {
		ids = append(ids, items[i].ID)
	}
	return ids
}
