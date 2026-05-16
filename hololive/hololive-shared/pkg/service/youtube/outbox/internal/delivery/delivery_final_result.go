package delivery

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

type terminalCommunityShortsOutboxResult struct {
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
	LatencyClassification PostLatencyClassificationResult
}

func (r *DeliveryRepository) LoadTerminalCommunityShortsOutboxResults(ctx context.Context, outboxIDs []int64) ([]terminalCommunityShortsOutboxResult, error) {
	uniqueIDs := uniqueInt64s(outboxIDs)
	if r == nil || r.db == nil || len(uniqueIDs) == 0 {
		return nil, nil
	}

	var outboxes []domain.YouTubeNotificationOutbox
	if err := r.db.WithContext(ctx).
		Where("id IN ?", uniqueIDs).
		Where("kind IN ?", []domain.OutboxKind{domain.OutboxKindNewShort, domain.OutboxKindCommunityPost}).
		Where("status IN ?", []domain.OutboxStatus{domain.OutboxStatusSent, domain.OutboxStatusFailed}).
		Order("id ASC").
		Find(&outboxes).Error; err != nil {
		return nil, fmt.Errorf("load terminal community/shorts outboxes: %w", err)
	}
	if len(outboxes) == 0 {
		return nil, nil
	}

	outboxResultIDs := collectOutboxIDs(outboxes)
	var deliveries []domain.YouTubeNotificationDelivery
	if err := r.db.WithContext(ctx).
		Where("outbox_id IN ?", outboxResultIDs).
		Order("outbox_id ASC, id ASC").
		Find(&deliveries).Error; err != nil {
		return nil, fmt.Errorf("load terminal community/shorts deliveries: %w", err)
	}

	deliveriesByOutbox := make(map[int64][]domain.YouTubeNotificationDelivery, len(outboxResultIDs))
	for i := range deliveries {
		row := deliveries[i]
		deliveriesByOutbox[row.OutboxID] = append(deliveriesByOutbox[row.OutboxID], row)
	}

	results := make([]terminalCommunityShortsOutboxResult, 0, len(outboxes))
	for i := range outboxes {
		results = append(results, summarizeTerminalCommunityShortsOutbox(outboxes[i], deliveriesByOutbox[outboxes[i].ID]))
	}

	return results, nil
}

func summarizeTerminalCommunityShortsOutbox(
	outbox domain.YouTubeNotificationOutbox,
	deliveries []domain.YouTubeNotificationDelivery,
) terminalCommunityShortsOutboxResult {
	result := terminalCommunityShortsOutboxResult{
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

func summarizeTerminalCommunityShortsDeliveries(deliveries []domain.YouTubeNotificationDelivery) (int, int, string) {
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

func terminalCommunityShortsDeliveryStatusCounts(status domain.OutboxStatus) (int, int) {
	switch status {
	case domain.OutboxStatusSent:
		return 1, 0
	case domain.OutboxStatusFailed:
		return 0, 1
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
